package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	namespace     string
	podName       string
	image         string
	interactive   bool
	tty           bool
	removeAfter   bool
	force         bool
	cpuRequest    string
	memoryLimit   string
	memoryRequest string
	profile       string
	copyPod       bool
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
		// Validate removeAfter flag
		if removeAfter && !(interactive && tty) {
			return fmt.Errorf("--rm requires -it")
		}

		// Validate profile
		switch profile {
		case "general", "restricted", "baseline", "privileged", "":
			// Valid profiles
		default:
			return fmt.Errorf("invalid profile %q: must be one of: general, restricted, baseline, privileged", profile)
		}

		return runDebug()
	},
}

func init() {
	// Set namespace flag with default value
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "namespace for the debug pod")

	// Other flags
	rootCmd.PersistentFlags().StringVarP(&podName, "pod", "p", "", "name of the target pod (optional)")
	rootCmd.PersistentFlags().StringVar(&image, "image", "jbuet/debug:latest", "debug container image")
	rootCmd.PersistentFlags().BoolVarP(&interactive, "stdin", "i", false, "keep stdin open even if not attached")
	rootCmd.PersistentFlags().BoolVarP(&tty, "tty", "t", false, "allocate a TTY for the container")
	rootCmd.PersistentFlags().BoolVar(&removeAfter, "rm", false, "automatically remove the pod after the session ends")
	rootCmd.PersistentFlags().BoolVarP(&force, "force", "f", false, "force creation of a new debug pod if one already exists")
	rootCmd.PersistentFlags().BoolVar(&copyPod, "copy", false, "create a copy of the target pod instead of adding a container")

	// Security profile flag
	rootCmd.PersistentFlags().StringVar(&profile, "profile", "", "security profile to use (general, restricted, baseline, privileged)")

	// Resource flags
	rootCmd.PersistentFlags().StringVar(&memoryLimit, "memory-limit", "128Mi", "memory limit for the debug container")
	rootCmd.PersistentFlags().StringVar(&cpuRequest, "cpu-request", "100m", "CPU request for the debug container")
	rootCmd.PersistentFlags().StringVar(&memoryRequest, "memory-request", "128Mi", "memory request for the debug container")
}

func Execute() error {
	return rootCmd.Execute()
}
