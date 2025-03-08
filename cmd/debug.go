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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/yaml"
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
	cmd := exec.Command("kubectl", "get", "pod", "-n", namespace, "-l",
		fmt.Sprintf("debug-tool/type=debug-pod,debug-tool/target=%s", podName),
		"--no-headers",
		"-o", "custom-columns=:metadata.name")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()

	// Si hay error, verificar si es porque no hay pods
	if err != nil {
		if strings.Contains(stderr.String(), "No resources found") {
			return "", nil
		}
		return "", fmt.Errorf("error checking for existing pods: %v - %s", err, stderr.String())
	}

	// Buscar pods que empiecen con "debug-" + podName
	prefix := fmt.Sprintf("debug-%s-", podName)
	for _, line := range strings.Split(string(output), "\n") {
		podName := strings.TrimSpace(line)
		if podName != "" && strings.HasPrefix(podName, prefix) {
			return podName, nil
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
	return fmt.Sprintf("debug-%s-%s-%s", podName, timestamp, randomStr)
}

func attachToPod(debugPodName string) error {
	args := []string{"exec", "-it", debugPodName, "-n", namespace, "--", "sh"}
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func deletePod(debugPodName string) error {
	cmd := exec.Command("kubectl", "delete", "pod", debugPodName, "-n", namespace)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getTargetPodLabels() (map[string]string, error) {
	cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace, "-o", "jsonpath={.metadata.labels}")
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
	maxAttempts := 30 // 30 seconds max wait time
	for i := 0; i < maxAttempts; i++ {
		cmd := exec.Command("kubectl", "get", "pod", debugPodName, "-n", namespace,
			"-o", "jsonpath={.status.phase}")
		output, err := cmd.Output()
		if err == nil && string(output) == "Running" {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("pod did not become ready within %d seconds", maxAttempts)
}

func getDeploymentSelectors() (map[string]string, error) {
	// Primero obtener el nombre del deployment buscando el owner reference del pod
	cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace,
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
	cmd = exec.Command("kubectl", "get", "rs", replicaSetName, "-n", namespace,
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
	cmd = exec.Command("kubectl", "get", "deployment", deploymentName, "-n", namespace,
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

func createDebugPod() (string, error) {
	debugPodName := generateUniqueName()
	log.Printf("Generating debug pod name: %s", debugPodName)

	// Obtener labels del pod target
	targetLabels, err := getTargetPodLabels()
	if err != nil {
		log.Printf("Warning: Could not get target pod labels: %v, using basic labels", err)
		targetLabels = map[string]string{
			"debug-tool/type":   "debug-pod",
			"debug-tool/target": podName,
		}
	}

	// Obtener los selectores del deployment y eliminarlos de los labels
	deploymentSelectors, err := getDeploymentSelectors()
	if err != nil {
		log.Printf("Warning: Could not get deployment selectors: %v", err)
	} else if deploymentSelectors != nil {
		log.Printf("Found deployment selectors: %v", deploymentSelectors)
		// Eliminar los selectores del deployment de los labels
		for key := range deploymentSelectors {
			delete(targetLabels, key)
		}
	}

	// Asegurarnos de que los labels básicos estén presentes
	targetLabels["debug-tool/type"] = "debug-pod"
	targetLabels["debug-tool/target"] = podName

	log.Printf("Creating debug pod with labels: %v", targetLabels)

	automountServiceAccountToken := false
	debugPod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      debugPodName,
			Namespace: namespace,
			Labels:    targetLabels,
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken:  &automountServiceAccountToken,
			TerminationGracePeriodSeconds: pointer.Int64(0),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: pointer.Bool(true),
				RunAsUser:    pointer.Int64(1000),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "debugger",
					Image:   image,
					Command: []string{"sleep", "infinity"},
					Stdin:   true,
					TTY:     true,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: pointer.Bool(false),
						RunAsNonRoot:             pointer.Bool(true),
						RunAsUser:                pointer.Int64(1000),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
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
	applyCmd := exec.Command("kubectl", "apply", "-f", tmpfile.Name())
	var stderr bytes.Buffer
	applyCmd.Stderr = &stderr
	if err := applyCmd.Run(); err != nil {
		return "", fmt.Errorf("error creating debug pod: %v - %s", err, stderr.String())
	}

	log.Printf("Debug pod created successfully")
	return debugPodName, nil
}

func debugExistingPod() error {
	args := []string{
		"debug", podName,
		"-n", namespace,
		"--image", image,
		"--share-processes",
		"--container", "debugger",
		"--profile=restricted",
	}

	if autoAttach {
		args = append(args, "-it", "--")
	}

	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getTargetPodImage() (string, error) {
	cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace,
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

func runDebug() error {
	// Verificar si el pod existe
	cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace)
	podExists := cmd.Run() == nil

	if !podExists {
		if containerName != "" {
			// Si se usa -p y el pod no existe, preguntar si crear uno nuevo
			if !askForPodCreation() {
				return newExecError("operation cancelled")
			}
			// Si el usuario quiere crear un pod nuevo pero no especificó imagen
			if image == "" {
				return newExecError("please specify an image to create a new debug pod")
			}
			// Continuar con la creación del pod (ya no usaremos kubectl debug)
			containerName = ""
		} else {
			return newExecError("pod %s does not exist in namespace %s", podName, namespace)
		}
	} else if containerName != "" {
		// Si se especifica un contenedor y el pod existe, usar kubectl debug directamente
		if image == "" {
			// Si no se especificó imagen, usar la del pod target
			targetImage, err := getTargetPodImage()
			if err != nil {
				return newExecError("could not get target pod image: %v", err)
			}
			if targetImage == "" {
				return newExecError("target pod has no image")
			}
			image = targetImage
		}
		log.Printf("Creating debug container in pod %s...\n", podName)
		if err := debugExistingPod(); err != nil {
			return newExecError("%v", err)
		}
		return nil
	}

	// Para la creación de un pod separado, si no se especificó imagen, usar la del pod target
	if image == "" {
		if podExists {
			targetImage, err := getTargetPodImage()
			if err != nil {
				return newExecError("could not get target pod image: %v", err)
			}
			if targetImage == "" {
				return newExecError("target pod has no image")
			}
			image = targetImage
		} else {
			return newExecError("please specify an image to create a new debug pod")
		}
	}

	// Resto de la lógica existente para crear un pod separado
	existingPod, err := findExistingDebugPod()
	if err != nil {
		return newExecError("%v", err)
	}

	var debugPodName string
	var isNewPod bool
	if existingPod != "" {
		if askForNewPod(existingPod) {
			isNewPod = true
			debugPodName, err = createDebugPod()
			if err != nil {
				return newExecError("%v", err)
			}
			log.Printf("Debug pod created: %s\n", debugPodName)
		} else {
			debugPodName = existingPod
			log.Printf("Using existing debug pod: %s\n", debugPodName)
		}
	} else {
		isNewPod = true
		debugPodName, err = createDebugPod()
		if err != nil {
			return newExecError("%v", err)
		}
		log.Printf("Debug pod created: %s\n", debugPodName)
	}

	if autoAttach {
		if isNewPod {
			log.Printf("Waiting for pod to be ready...\n")
			if err := waitForPod(debugPodName); err != nil {
				return newExecError("%v", err)
			}
		}
		log.Printf("Attaching to pod...\n")
		if err := attachToPod(debugPodName); err != nil {
			return newExecError("%v", err)
		}
		if removeAfter {
			log.Printf("Removing debug pod...\n")
			if err := deletePod(debugPodName); err != nil {
				return newExecError("%v", err)
			}
		}
	} else {
		log.Printf("You can access the pod with: kubectl exec -it %s -n %s -- sh\n", debugPodName, namespace)
	}

	return nil
}
