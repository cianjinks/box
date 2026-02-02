package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [container-id] [runtime-bundle-path]",
	Short: "run a container from a runtime bundle on disk",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		containerId := args[0]
		runtimePath := args[1]

		ctx := cmd.Context()
		log := Logger(ctx)

		// read config
		config := &specs.Spec{}
		configPath := filepath.Join(runtimePath, configFile)

		file, err := os.Open(configPath)
		if err != nil {
			return fmt.Errorf("failed to open runtime config file: %w", err)
		}
		defer file.Close()
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(config); err != nil {
			return fmt.Errorf("failed to decode runtime config file: %w", err)
		}

		// path to rootfs
		rootfsPath, err := filepath.Abs(filepath.Join(runtimePath, rootfsFolder))
		if err != nil {
			return fmt.Errorf("failed to make absolute path from rootfs path: %w", err)
		}

		log.Info("run", "container", containerId)

		exec := exec.Command("/proc/self/exe", "child", rootfsPath)
		exec.Stdin = os.Stdin
		exec.Stdout = os.Stdout
		exec.Stderr = os.Stderr
		exec.SysProcAttr = &syscall.SysProcAttr{
			// CLONE_NEWIPC: new IPC namespace
			// CLONE_NEWNET: new network namespace
			// CLONE_NEWNS: new mount namespace
			// CLONE_NEWPID: new PID namespace
			// CLONE_NEWUSER: new user namespace
			// CLONE_NEWUTS: new UTS namespace (hostname + NIS domain isolation)
			Cloneflags: syscall.CLONE_NEWIPC | syscall.CLONE_NEWNET | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWUSER | syscall.CLONE_NEWUTS,
			// remap current user to root in the container
			UidMappings: []syscall.SysProcIDMap{
				{
					ContainerID: 0,
					HostID:      os.Getuid(),
					Size:        1,
				},
			},
			GidMappings: []syscall.SysProcIDMap{
				{
					ContainerID: 0,
					HostID:      os.Getgid(),
					Size:        1,
				},
			},
			// TODO: `UseCgroupFD` and `CgroupFD`
		}

		if err := exec.Run(); err != nil {
			return fmt.Errorf("failed to clone process: %w", err)
		}

		return nil
	},
}
