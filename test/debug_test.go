package test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jbuet/kubectl-debug/cmd"
)

// MockCommand stores the last command execution for validation
type MockCommand struct {
	Command string
	Args    []string
}

var lastCommand MockCommand
var mockShouldFail bool

// Mock exec.Command
func mockExecCommand(command string, args ...string) *exec.Cmd {
	// Store the command for validation
	lastCommand = MockCommand{
		Command: command,
		Args:    args,
	}

	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	if mockShouldFail {
		cmd.Env = append(cmd.Env, "GO_WANT_HELPER_PROCESS_FAIL=1")
	}
	return cmd
}

// TestHelperProcess helps mock exec.Command
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	if os.Getenv("GO_WANT_HELPER_PROCESS_FAIL") == "1" {
		fmt.Fprintf(os.Stderr, "mock failure")
		os.Exit(1)
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No command\n")
		os.Exit(2)
	}

	cmd, args := args[0], args[1:]
	switch cmd {
	case "kubectl":
		if len(args) > 0 {
			switch args[0] {
			case "get":
				if args[1] == "pod" {
					switch {
					case strings.Contains(strings.Join(args, " "), "custom-columns=:metadata.name"):
						// Mock findExistingDebugPod
						fmt.Println("debug-test-123")
					case strings.Contains(strings.Join(args, " "), "jsonpath={.spec.containers[0].name}"):
						// Mock getTargetContainerName
						fmt.Println("nginx")
					case strings.Contains(strings.Join(args, " "), "jsonpath={.metadata.labels}"):
						// Mock getTargetPodLabels
						fmt.Println("{\"app\":\"nginx\",\"debug-tool/type\":\"debug-pod\"}")
					case strings.Contains(strings.Join(args, " "), "jsonpath={.spec.containers[0].image}"):
						// Mock getTargetPodImage
						fmt.Println("nginx:latest")
					default:
						// Mock pod existence check
						if strings.Contains(strings.Join(args, " "), "nonexistent") {
							os.Exit(1)
						}
					}
					return
				}
			case "apply", "delete", "exec", "debug":
				return
			}
		}
	}
	os.Exit(0)
}

func TestRunDebug(t *testing.T) {
	origExecCommand := cmd.ExecCommand
	defer func() { cmd.ExecCommand = origExecCommand }()
	cmd.ExecCommand = mockExecCommand

	tests := []struct {
		name       string
		namespace  string
		podName    string
		image      string
		copyPod    bool
		shouldFail bool
		wantErr    bool
	}{
		{
			name:       "Create standalone pod",
			namespace:  "default",
			podName:    "",
			image:     "debug:latest",
			copyPod:   false,
			shouldFail: false,
			wantErr:    false,
		},
		{
			name:       "Create copy of target pod",
			namespace:  "default",
			podName:    "test-pod",
			image:     "debug:latest",
			copyPod:   true,
			shouldFail: false,
			wantErr:    false,
		},
		{
			name:       "Add container to existing pod",
			namespace:  "default",
			podName:    "test-pod",
			image:     "debug:latest",
			copyPod:   false,
			shouldFail: false,
			wantErr:    false,
		},
		{
			name:       "Target pod does not exist",
			namespace:  "default",
			podName:    "nonexistent",
			image:     "debug:latest",
			copyPod:   false,
			shouldFail: true,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd.SetNamespace(tt.namespace)
			cmd.SetPodName(tt.podName)
			cmd.SetImage(tt.image)
			cmd.SetCopyPod(tt.copyPod)
			mockShouldFail = tt.shouldFail

			err := cmd.RunDebug()
			if (err != nil) != tt.wantErr {
				t.Errorf("RunDebug() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetTargetContainerName(t *testing.T) {
	origExecCommand := cmd.ExecCommand
	defer func() { cmd.ExecCommand = origExecCommand }()
	cmd.ExecCommand = mockExecCommand

	tests := []struct {
		name        string
		namespace   string
		podName     string
		shouldFail  bool
		wantErr     bool
		wantName    string
		wantCommand string
		wantArgs    []string
	}{
		{
			name:        "Valid pod",
			namespace:   "default",
			podName:     "test-pod",
			shouldFail:  false,
			wantErr:     false,
			wantName:    "nginx",
			wantCommand: "kubectl",
			wantArgs:    []string{"get", "pod", "test-pod", "-n", "default", "-o", "jsonpath={.spec.containers[0].name}"},
		},
		{
			name:        "Invalid pod",
			namespace:   "default",
			podName:     "nonexistent",
			shouldFail:  true,
			wantErr:     true,
			wantName:    "",
			wantCommand: "kubectl",
			wantArgs:    []string{"get", "pod", "nonexistent", "-n", "default"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd.SetNamespace(tt.namespace)
			cmd.SetPodName(tt.podName)
			mockShouldFail = tt.shouldFail
			
			got, err := cmd.GetTargetContainerName()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTargetContainerName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.wantName {
				t.Errorf("GetTargetContainerName() = %v, want %v", got, tt.wantName)
			}
		})
	}
}

func TestDeletePod(t *testing.T) {
	origExecCommand := cmd.ExecCommand
	defer func() { cmd.ExecCommand = origExecCommand }()
	cmd.ExecCommand = mockExecCommand

	tests := []struct {
		name       string
		podName    string
		shouldFail bool
		wantErr    bool
	}{
		{
			name:       "Successfully delete pod",
			podName:    "test-pod",
			shouldFail: false,
			wantErr:    false,
		},
		{
			name:       "Fail to delete pod",
			podName:    "nonexistent",
			shouldFail: true,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockShouldFail = tt.shouldFail
			err := cmd.DeletePod(tt.podName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeletePod() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAttachToPod(t *testing.T) {
	origExecCommand := cmd.ExecCommand
	defer func() { cmd.ExecCommand = origExecCommand }()
	cmd.ExecCommand = mockExecCommand

	tests := []struct {
		name       string
		podName    string
		shouldFail bool
		wantErr    bool
	}{
		{
			name:       "Successfully attach to pod",
			podName:    "test-pod",
			shouldFail: false,
			wantErr:    false,
		},
		{
			name:       "Fail to attach to pod",
			podName:    "nonexistent",
			shouldFail: true,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockShouldFail = tt.shouldFail
			err := cmd.AttachToPod(tt.podName)
			if (err != nil) != tt.wantErr {
				t.Errorf("AttachToPod() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFindExistingDebugPod(t *testing.T) {
	origExecCommand := cmd.ExecCommand
	defer func() { cmd.ExecCommand = origExecCommand }()
	cmd.ExecCommand = mockExecCommand

	tests := []struct {
		name       string
		namespace  string
		podName    string
		shouldFail bool
		wantPod    string
		wantErr    bool
	}{
		{
			name:       "Find existing pod",
			namespace:  "default",
			podName:    "test-pod",
			shouldFail: false,
			wantPod:    "debug-test-123",
			wantErr:    false,
		},
		{
			name:       "No existing pod",
			namespace:  "default",
			podName:    "nonexistent",
			shouldFail: true,
			wantPod:    "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd.SetNamespace(tt.namespace)
			cmd.SetPodName(tt.podName)
			mockShouldFail = tt.shouldFail

			got, err := cmd.FindExistingDebugPod()
			if (err != nil) != tt.wantErr {
				t.Errorf("FindExistingDebugPod() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantPod {
				t.Errorf("FindExistingDebugPod() = %v, want %v", got, tt.wantPod)
			}
		})
	}
}

func TestGenerateUniqueName(t *testing.T) {
	tests := []struct {
		name     string
		podName  string
		wantPrefix string
	}{
		{
			name:     "No target pod",
			podName:  "",
			wantPrefix: "debug-",
		},
		{
			name:     "With target pod",
			podName:  "test-pod",
			wantPrefix: "debug-test-pod-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd.SetPodName(tt.podName)
			got := cmd.GenerateUniqueName()
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("GenerateUniqueName() = %v, want prefix %v", got, tt.wantPrefix)
			}
		})
	}
}

// Helper function to compare string slices
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
} 