package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	namespace     string
	podName       string
	containerName string
	image         string
	targetPod     string
	autoAttach    bool
	removeAfter   bool
	force         bool
	cpuRequest    string
	memoryLimit   string
	memoryRequest string
)

var rootCmd = &cobra.Command{
	Use:   "kubectl-debug",
	Short: "A tool for creating secure debug pods in Kubernetes",
	Long: `kubectl-debug creates debug pods with secure defaults,
including non-root execution, resource limits, and security context configuration.
It provides an easy-to-use CLI interface for debugging Kubernetes pods.`,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if podName == "" {
			return fmt.Errorf("pod name is required")
		}
		return runDebug()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "namespace of the pod")
	rootCmd.PersistentFlags().StringVarP(&podName, "pod", "p", "", "name of the pod to debug")
	rootCmd.PersistentFlags().StringVarP(&containerName, "container", "c", "", "name of the container to debug")
	rootCmd.PersistentFlags().StringVarP(&image, "image", "i", "jbuet/debug:v1.0.0", "debug container image")
	rootCmd.PersistentFlags().StringVarP(&targetPod, "target", "t", "", "target pod name if different from original")
	rootCmd.PersistentFlags().BoolVarP(&autoAttach, "attach", "a", false, "automatically attach to the debug pod")
	rootCmd.PersistentFlags().BoolVarP(&removeAfter, "rm", "r", false, "automatically remove the pod after the session ends")
	rootCmd.PersistentFlags().BoolVarP(&force, "force", "f", false, "force creation of a new debug pod if one already exists")

	// Resource flags
	rootCmd.PersistentFlags().StringVar(&memoryLimit, "memory-limit", "128Mi", "memory limit for the debug container")
	rootCmd.PersistentFlags().StringVar(&cpuRequest, "cpu-request", "100m", "CPU request for the debug container")
	rootCmd.PersistentFlags().StringVar(&memoryRequest, "memory-request", "128Mi", "memory request for the debug container")

	rootCmd.MarkPersistentFlagRequired("pod")
}

func Execute() error {
	return rootCmd.Execute()
}
