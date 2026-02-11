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

		// TODO: use information from config here, such as namespaces and more
		_, _, err := GetConfigAndRootFromRuntimePath(runtimePath)
		if err != nil {
			return err
		}

		log.Info("run", "container", containerId)

		exec := exec.Command("/proc/self/exe", "child", runtimePath)
		// TODO: use a PTY for security
		exec.Stdin = os.Stdin
		exec.Stdout = os.Stdout
		exec.Stderr = os.Stderr
		exec.SysProcAttr = &syscall.SysProcAttr{
			// CLONE_NEWIPC: new IPC namespace
			// CLONE_NEWNET: new network namespace
			// CLONE_NEWNS: new mount namespace
			// CLONE_NEWPID: new PID namespace
			// CLONE_NEWUTS: new UTS namespace (hostname + NIS domain isolation)
			// TODO: build this list from the OCI config
			Cloneflags: syscall.CLONE_NEWIPC | syscall.CLONE_NEWNET | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWUTS,
			// TODO: `UseCgroupFD` and `CgroupFD`
		}

		if err := exec.Run(); err != nil {
			return fmt.Errorf("failed to execute child process: %w", err)
		}

		return nil
	},
}
