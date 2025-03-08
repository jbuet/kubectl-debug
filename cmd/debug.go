package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/yaml"
)

// Add at the top of the file, after imports
var ExecCommand = exec.Command

// Add near the top with other vars
var (
	sleepDuration = time.Second
	maxAttempts   = 30 // 30 seconds max wait time
)

// Tipo de error para errores de ejecución
type ExecError struct {
	msg string
}

func (e *ExecError) Error() string {
	return e.msg
}

func newExecError(format string, args ...interface{}) *ExecError {
	return &ExecError{
		msg: fmt.Sprintf(format, args...),
	}
}

func findExistingDebugPod() (string, error) {
	labelSelector := "debug-tool/type=debug-pod"
	if podName != "" {
		labelSelector += fmt.Sprintf(",debug-tool/target=%s", podName)
	}

	cmd := ExecCommand("kubectl", "get", "pod", "-n", namespace, "-l", labelSelector,
		"--no-headers",
		"-o", "custom-columns=:metadata.name")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()

	// If there's an error, check if it's because no pods were found
	if err != nil {
		if strings.Contains(stderr.String(), "No resources found") {
			return "", nil
		}
		return "", fmt.Errorf("error checking for existing pods: %v - %s", err, stderr.String())
	}

	// Get the first non-empty pod name
	for _, line := range strings.Split(string(output), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			return name, nil
		}
	}

	return "", nil
}

func askForNewPod(existingPod string) bool {
	if force {
		return true
	}

	fmt.Printf("Debug pod '%s' already exists in namespace '%s'. Do you want to:\n", existingPod, namespace)
	fmt.Printf("[1] Use existing pod\n")
	fmt.Printf("[2] Create new pod\n")
	fmt.Printf("Choose (1/2) [1]: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(response)
	return response == "2"
}

func generateUniqueName() string {
	timestamp := time.Now().Format("150405") // HHMMSS
	randomStr := fmt.Sprintf("%04d", rand.Intn(10000))

	// If no target pod, use simpler name format
	if podName == "" {
		return fmt.Sprintf("debug-%s-%s", timestamp, randomStr)
	}

	// If target pod specified, include it in the name
	return fmt.Sprintf("debug-%s-%s-%s", podName, timestamp, randomStr)
}

func attachToPod(debugPodName string) error {
	args := []string{"exec", "-it", debugPodName, "-n", namespace, "--", "sh"}
	cmd := ExecCommand("kubectl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func deletePod(debugPodName string) error {
	cmd := ExecCommand("kubectl", "delete", "pod", debugPodName, "-n", namespace)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getTargetPodLabels() (map[string]string, error) {
	cmd := ExecCommand("kubectl", "get", "pod", podName, "-n", namespace, "-o", "jsonpath={.metadata.labels}")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error getting target pod labels: %v - %s", err, stderr.String())
	}

	// Si no hay output, retornar un mapa con los labels básicos
	if len(output) == 0 {
		return map[string]string{
			"debug-tool/type":   "debug-pod",
			"debug-tool/target": podName,
		}, nil
	}

	// Parsear el output JSON a un mapa
	labels := make(map[string]string)
	if err := json.Unmarshal(output, &labels); err != nil {
		log.Printf("Warning: Error parsing labels JSON: %v, using basic labels", err)
		return map[string]string{
			"debug-tool/type":   "debug-pod",
			"debug-tool/target": podName,
		}, nil
	}

	return labels, nil
}

func waitForPod(debugPodName string) error {
	for i := 0; i < maxAttempts; i++ {
		cmd := ExecCommand("kubectl", "get", "pod", debugPodName, "-n", namespace,
			"-o", "jsonpath={.status.phase}")
		output, err := cmd.Output()
		if err == nil && string(output) == "Running" {
			return nil
		}
		time.Sleep(sleepDuration)
	}
	return fmt.Errorf("pod did not become ready within %d seconds", maxAttempts)
}

func getDeploymentSelectors() (map[string]string, error) {
	// Primero obtener el nombre del deployment buscando el owner reference del pod
	cmd := ExecCommand("kubectl", "get", "pod", podName, "-n", namespace,
		"-o", "jsonpath={.metadata.ownerReferences[?(@.kind=='ReplicaSet')].name}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error getting pod owner reference: %v", err)
	}
	replicaSetName := strings.TrimSpace(string(output))
	if replicaSetName == "" {
		return nil, nil // Pod no pertenece a un ReplicaSet
	}

	// Obtener el nombre del deployment desde el ReplicaSet
	cmd = ExecCommand("kubectl", "get", "rs", replicaSetName, "-n", namespace,
		"-o", "jsonpath={.metadata.ownerReferences[?(@.kind=='Deployment')].name}")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error getting replicaset owner reference: %v", err)
	}
	deploymentName := strings.TrimSpace(string(output))
	if deploymentName == "" {
		return nil, nil // ReplicaSet no pertenece a un Deployment
	}

	// Obtener los matchLabels del deployment
	cmd = ExecCommand("kubectl", "get", "deployment", deploymentName, "-n", namespace,
		"-o", "jsonpath={.spec.selector.matchLabels}")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error getting deployment selector: %v", err)
	}

	// Parsear los matchLabels
	selectors := make(map[string]string)
	if err := json.Unmarshal(output, &selectors); err != nil {
		return nil, fmt.Errorf("error parsing deployment selector: %v", err)
	}

	return selectors, nil
}

func getTargetPodSecurityContext() (*corev1.PodSecurityContext, error) {
	cmd := ExecCommand("kubectl", "get", "pod", podName, "-n", namespace, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error getting pod info: %v", err)
	}

	var pod corev1.Pod
	if err := json.Unmarshal(output, &pod); err != nil {
		return nil, fmt.Errorf("error parsing pod JSON: %v", err)
	}

	return pod.Spec.SecurityContext, nil
}

func getSecurityContextForProfile(profileName string) (*corev1.SecurityContext, *corev1.PodSecurityContext) {
	containerContext := &corev1.SecurityContext{
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}

	podContext := &corev1.PodSecurityContext{
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}

	switch profileName {
	case "restricted":
		containerContext.AllowPrivilegeEscalation = pointer.Bool(false)
		containerContext.Capabilities = &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		}
		containerContext.RunAsNonRoot = pointer.Bool(true)
		containerContext.RunAsUser = pointer.Int64(1000)
		containerContext.SeccompProfile.Type = corev1.SeccompProfileTypeRuntimeDefault

		podContext.RunAsNonRoot = pointer.Bool(true)
		podContext.RunAsUser = pointer.Int64(1000)
		podContext.SeccompProfile.Type = corev1.SeccompProfileTypeRuntimeDefault

	case "baseline":
		containerContext.AllowPrivilegeEscalation = pointer.Bool(false)
		containerContext.Capabilities = &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		}
		containerContext.SeccompProfile.Type = corev1.SeccompProfileTypeRuntimeDefault

		podContext.SeccompProfile.Type = corev1.SeccompProfileTypeRuntimeDefault

	case "privileged":
		containerContext.AllowPrivilegeEscalation = pointer.Bool(true)
		containerContext.Privileged = pointer.Bool(true)
		containerContext.Capabilities = &corev1.Capabilities{
			Add: []corev1.Capability{"ALL"},
		}
		containerContext.SeccompProfile.Type = corev1.SeccompProfileTypeUnconfined

		podContext.SeccompProfile.Type = corev1.SeccompProfileTypeUnconfined
	}

	return containerContext, podContext
}

func createCustomDebugYAML() (string, error) {
	secContext, err := getTargetPodSecurityContext()
	if err != nil {
		return "", fmt.Errorf("error getting security context: %v", err)
	}

	debugContainer := map[string]interface{}{
		"securityContext": secContext,
	}

	yamlData, err := yaml.Marshal(debugContainer)
	if err != nil {
		return "", fmt.Errorf("error marshaling YAML: %v", err)
	}

	tmpfile, err := os.CreateTemp("", "debug-custom-*.yaml")
	if err != nil {
		return "", fmt.Errorf("error creating temporary file: %v", err)
	}

	if _, err := tmpfile.Write(yamlData); err != nil {
		os.Remove(tmpfile.Name())
		return "", fmt.Errorf("error writing YAML: %v", err)
	}

	if err := tmpfile.Close(); err != nil {
		os.Remove(tmpfile.Name())
		return "", fmt.Errorf("error closing temporary file: %v", err)
	}

	return tmpfile.Name(), nil
}

func createDebugPod() (string, error) {
	debugPodName := generateUniqueName()
	log.Printf("Generating debug pod name: %s", debugPodName)

	// Initialize basic labels
	labels := map[string]string{
		"debug-tool/type": "debug-pod",
	}

	// Configure pod spec
	automountServiceAccountToken := false
	podSpec := corev1.PodSpec{
		AutomountServiceAccountToken:  &automountServiceAccountToken,
		TerminationGracePeriodSeconds: pointer.Int64(0),
	}

	// If targeting an existing pod
	if podName != "" {
		// Try to get target pod's security context
		secContext, err := getTargetPodSecurityContext()
		if err != nil {
			log.Printf("Warning: Could not get target pod security context: %v", err)
		} else if secContext != nil && secContext.RunAsUser != nil {
			// Only set security context if target pod has RunAsUser defined
			podSpec.SecurityContext = secContext
			log.Printf("Using security context from target pod (UID: %d)", *secContext.RunAsUser)
		} else {
			log.Printf("No security context defined in target pod, using profile settings")
		}

		// Get target pod labels
		targetLabels, err := getTargetPodLabels()
		if err == nil {
			labels = targetLabels
		}
		labels["debug-tool/target"] = podName

		// Remove deployment selectors if present
		deploymentSelectors, err := getDeploymentSelectors()
		if err == nil && deploymentSelectors != nil {
			for key := range deploymentSelectors {
				delete(labels, key)
			}
		}

		// Enable process namespace sharing
		shareProcessNamespace := true
		podSpec.ShareProcessNamespace = &shareProcessNamespace
	}

	// If no security context is set from target pod, use profile settings
	if podSpec.SecurityContext == nil {
		_, podContext := getSecurityContextForProfile(profile)
		podSpec.SecurityContext = podContext
		log.Printf("Using security context from profile: %s", profile)
	}

	// Ensure debug tool labels are present
	labels["debug-tool/type"] = "debug-pod"

	debugPod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      debugPodName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: podSpec,
	}

	// Add the debug container with appropriate security context
	containerContext, _ := getSecurityContextForProfile(profile)
	if podSpec.SecurityContext != nil && podSpec.SecurityContext.RunAsUser != nil {
		// If pod has a specific RunAsUser, override the container's RunAsUser
		containerContext.RunAsUser = podSpec.SecurityContext.RunAsUser
		containerContext.RunAsNonRoot = podSpec.SecurityContext.RunAsNonRoot
	}

	// Add the debug container
	var command []string
	if interactive && tty {
		command = []string{"bash"}
	} else {
		command = []string{"sleep", "infinity"}
	}

	debugPod.Spec.Containers = []corev1.Container{
		{
			Name:            "debugger",
			Image:           image,
			Command:         command,
			Stdin:           true,
			TTY:             true,
			SecurityContext: containerContext,
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse(memoryLimit),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(cpuRequest),
					corev1.ResourceMemory: resource.MustParse(memoryRequest),
				},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"/bin/true"},
					},
				},
				InitialDelaySeconds: 5,
				PeriodSeconds:       10,
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"/bin/true"},
					},
				},
				InitialDelaySeconds: 5,
				PeriodSeconds:       10,
			},
		},
	}

	podYAML, err := yaml.Marshal(debugPod)
	if err != nil {
		return "", fmt.Errorf("error generating YAML: %v", err)
	}

	tmpfile, err := os.CreateTemp("", "debug-pod-*.yaml")
	if err != nil {
		return "", fmt.Errorf("error creating temporary file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write(podYAML); err != nil {
		return "", fmt.Errorf("error writing YAML: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		return "", fmt.Errorf("error closing temporary file: %v", err)
	}

	log.Printf("Applying debug pod YAML...")
	applyCmd := ExecCommand("kubectl", "apply", "-f", tmpfile.Name())
	var stderr bytes.Buffer
	applyCmd.Stderr = &stderr
	if err := applyCmd.Run(); err != nil {
		return "", fmt.Errorf("error creating debug pod: %v - %s", err, stderr.String())
	}

	log.Printf("Debug pod created successfully")
	return debugPodName, nil
}

func debugExistingPod() error {
	customYAML, err := createCustomDebugYAML()
	if err != nil {
		log.Printf("Warning: Could not create custom YAML: %v", err)
	}
	defer os.Remove(customYAML) // Clean up the temporary file

	debugPodName := generateUniqueName()
	args := []string{
		"debug", podName,
		"-n", namespace,
		"--image", image,
		"--share-processes",
		"--container", "debugger",
		"--profile=restricted",
		"--copy-to=" + debugPodName,
	}

	if customYAML != "" {
		args = append(args, "--custom="+customYAML)
	}

	if interactive && tty {
		args = append(args, "-it", "--")
	}

	cmd := ExecCommand("kubectl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getTargetPodImage() (string, error) {
	cmd := ExecCommand("kubectl", "get", "pod", podName, "-n", namespace,
		"-o", "jsonpath={.spec.containers[0].image}")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("error getting target pod image: %v", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func askForPodCreation() bool {
	fmt.Printf("Pod '%s' does not exist in namespace '%s'. Do you want to create a new debug pod? (y/N): ", podName, namespace)
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

func getTargetContainerName() (string, error) {
	cmd := ExecCommand("kubectl", "get", "pod", podName, "-n", namespace,
		"-o", "jsonpath={.spec.containers[0].name}")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("error getting container name: %v", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func setupSignalHandler(debugPodName string) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Printf("\nReceived interrupt signal, cleaning up...")
		if err := deletePod(debugPodName); err != nil {
			log.Printf("Warning: Failed to delete pod %s: %v", debugPodName, err)
		}
		os.Exit(1)
	}()
}

func runDebug() error {
	// Case 1: New standalone debug pod (no target pod specified)
	if podName == "" {
		debugPodName, err := createDebugPod()
		if err != nil {
			return newExecError("failed to create debug pod: %v", err)
		}

		// Set up signal handler for cleanup
		if removeAfter {
			setupSignalHandler(debugPodName)
		}

		// Wait for pod to be ready only if we're going to attach to it
		if interactive && tty {
			log.Printf("Waiting for pod to be ready...")
			if err := waitForPod(debugPodName); err != nil {
				return newExecError("pod did not become ready: %v", err)
			}
		}

		// If --rm flag is set, clean up the pod after the session ends
		if removeAfter {
			defer func() {
				log.Printf("Cleaning up debug pod %s...", debugPodName)
				deleteArgs := []string{
					"delete",
					"pod",
					debugPodName,
					"-n",
					namespace,
				}
				deleteCmd := ExecCommand("kubectl", deleteArgs...)
				if err := deleteCmd.Run(); err != nil {
					log.Printf("Warning: Failed to delete debug pod: %v", err)
				} else {
					log.Printf("Debug pod deleted successfully")
				}
			}()
		}

		// Attach to the pod if interactive mode is enabled
		if interactive && tty {
			attachArgs := []string{
				"attach",
				"-it",
				debugPodName,
				"-n",
				namespace,
			}
			attachCmd := ExecCommand("kubectl", attachArgs...)
			attachCmd.Stdin = os.Stdin
			attachCmd.Stdout = os.Stdout
			attachCmd.Stderr = os.Stderr
			if err := attachCmd.Run(); err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					os.Exit(exitErr.ExitCode())
				}
				return fmt.Errorf("error attaching to pod: %v", err)
			}
		} else {
			log.Printf("You can access the pod with: kubectl exec -it %s -n %s -- sh\n", debugPodName, namespace)
		}
		return nil
	}

	// For cases 2 and 3, verify if target pod exists
	cmd := ExecCommand("kubectl", "get", "pod", podName, "-n", namespace)
	if cmd.Run() != nil {
		return newExecError("target pod %s does not exist in namespace %s", podName, namespace)
	}

	// Get the target container name
	containerName, err := getTargetContainerName()
	if err != nil {
		return newExecError("error getting container name: %v", err)
	}

	// Check for existing debug pod if we're going to create a new one
	if copyPod {
		existingPod, err := findExistingDebugPod()
		if err != nil {
			return newExecError("error checking for existing debug pods: %v", err)
		}

		if existingPod != "" && !force {
			if !askForNewPod(existingPod) {
				// Use existing pod
				log.Printf("Using existing debug pod: %s\n", existingPod)
				if interactive && tty {
					log.Printf("Attaching to pod...\n")
					if err := attachToPod(existingPod); err != nil {
						return newExecError("%v", err)
					}
					if removeAfter {
						log.Printf("Removing debug pod...\n")
						if err := deletePod(existingPod); err != nil {
							return newExecError("%v", err)
						}
					}
				} else {
					log.Printf("You can access the pod with: kubectl exec -it %s -n %s -- sh\n", existingPod, namespace)
				}
				return nil
			}
		}
	}

	// Case 2: Create a copy of target pod with debug container
	if copyPod {
		debugPodName := generateUniqueName()

		// Set up signal handler for cleanup
		if removeAfter {
			setupSignalHandler(debugPodName)
		}

		// Create custom debug container configuration for resources
		customDebug := map[string]interface{}{
			"resources": map[string]interface{}{
				"limits": map[string]string{
					"memory": memoryLimit,
				},
				"requests": map[string]string{
					"cpu":    cpuRequest,
					"memory": memoryRequest,
				},
			},
		}

		// Create temporary file for custom debug configuration
		customYAML, err := yaml.Marshal(customDebug)
		if err != nil {
			return newExecError("failed to create custom debug configuration: %v", err)
		}

		tmpfile, err := os.CreateTemp("", "debug-custom-*.yaml")
		if err != nil {
			return newExecError("failed to create temporary file: %v", err)
		}
		defer os.Remove(tmpfile.Name())

		if _, err := tmpfile.Write(customYAML); err != nil {
			return newExecError("failed to write custom debug configuration: %v", err)
		}
		if err := tmpfile.Close(); err != nil {
			return newExecError("failed to close temporary file: %v", err)
		}

		// Check if target pod has a security context
		secContext, err := getTargetPodSecurityContext()
		if err != nil {
			log.Printf("Warning: Could not get target pod security context: %v", err)
		}

		args := []string{
			"debug", podName,
			"-n", namespace,
			"--image", image,
			"--share-processes",
			"--copy-to=" + debugPodName,
			"--custom=" + tmpfile.Name(),
		}

		// Only set profile if target pod has security context or profile was explicitly set
		if (secContext != nil && secContext.RunAsUser != nil) || profile != "" {
			profileToUse := profile
			if profileToUse == "" {
				profileToUse = "general"
			}
			args = append(args, "--profile="+profileToUse)
		}

		if interactive {
			args = append(args, "-i")
		}
		if tty {
			args = append(args, "-t")
		}
		if interactive && tty {
			args = append(args, "--")
		}

		log.Printf("Creating debug pod %s as a copy of %s...\n", debugPodName, podName)
		cmd = ExecCommand("kubectl", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return newExecError("failed to create debug pod: %v", err)
		}

		if !interactive || !tty {
			log.Printf("You can access the pod with: kubectl exec -it %s -n %s -- sh\n", debugPodName, namespace)
		}

		if removeAfter && interactive && tty {
			log.Printf("Removing debug pod...\n")
			if err := deletePod(debugPodName); err != nil {
				return newExecError("%v", err)
			}
		}
		return nil
	}

	// Case 3: Add debug container to existing pod
	args := []string{
		"debug", podName,
		"-n", namespace,
		"--image", image,
		"--target=" + containerName,
	}

	// Always set profile if specified, otherwise use "general" as default
	if profile != "" {
		args = append(args, "--profile="+profile)
	} else {
		args = append(args, "--profile=general")
	}

	if interactive {
		args = append(args, "-i")
	}
	if tty {
		args = append(args, "-t")
	}
	if interactive && tty {
		args = append(args, "--")
	}

	log.Printf("Adding debug container to pod %s (targeting container %s)...\n", podName, containerName)
	cmd = ExecCommand("kubectl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// Export functions for testing
func SetPodName(name string) {
	podName = name
}

func SetNamespace(ns string) {
	namespace = ns
}

func SetImage(img string) {
	image = img
}

func SetCopyPod(copy bool) {
	copyPod = copy
}

func RunDebug() error {
	return runDebug()
}

func GetTargetContainerName() (string, error) {
	return getTargetContainerName()
}

func GetTargetPodSecurityContext() (*corev1.PodSecurityContext, error) {
	return getTargetPodSecurityContext()
}

func WaitForPod(podName string) error {
	return waitForPod(podName)
}

func DeletePod(podName string) error {
	return deletePod(podName)
}

func AttachToPod(podName string) error {
	return attachToPod(podName)
}

func FindExistingDebugPod() (string, error) {
	return findExistingDebugPod()
}

func GenerateUniqueName() string {
	return generateUniqueName()
}

// Add to the exported functions section
func SetSleepDuration(d time.Duration) {
	sleepDuration = d
}

func GetSleepDuration() time.Duration {
	return sleepDuration
}

func SetMaxAttempts(attempts int) {
	maxAttempts = attempts
}

func GetMaxAttempts() int {
	return maxAttempts
}
