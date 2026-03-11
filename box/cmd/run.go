package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run container-id runtime-bundle-path",
	Short: "run a container from a runtime bundle on disk",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		containerId := args[0]
		runtimePath := args[1]

		ctx := cmd.Context()
		log := Logger(ctx)

		config, _, err := GetConfigAndRootFromRuntimePath(runtimePath)
		if err != nil {
			return err
		}

		log.Info("run", "container", containerId)

		exec := exec.Command("/proc/self/exe", "child", runtimePath)
		// TODO: use a PTY
		exec.Stdin = os.Stdin
		exec.Stdout = os.Stdout
		exec.Stderr = os.Stderr
		exec.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: CloneFlagsFromNamespaces(config.Linux.Namespaces),
			// TODO: `UseCgroupFD` and `CgroupFD`
		}

		if err := exec.Run(); err != nil {
			return fmt.Errorf("failed to execute child process: %w", err)
		}

		return nil
	},
}
