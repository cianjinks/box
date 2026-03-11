package cmd

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/cobra"
)

var childCmd = &cobra.Command{
	Use:    "child runtime-bundle-path",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runtimePath := args[0]

		ctx := cmd.Context()
		log := Logger(ctx)

		config, rootfsPath, err := GetConfigAndRootFromRuntimePath(runtimePath)
		if err != nil {
			return err
		}

		// 1. recursively mark all mounts as private to avoid mount leakage
		log.Info("marking all mounts as private")
		if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
			return fmt.Errorf("failed to make mount tree private: %w", err)
		}

		// 2. bind mount the rootfs to itself as we need a mount for pivot_root
		log.Info("creating bind mount for rootfs", "rootfsPath", rootfsPath)
		if err := syscall.Mount(rootfsPath, rootfsPath, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
			return fmt.Errorf("failed to create bind mount for rootfs: %w", err)
		}

		// 3. pivot_root, we use this trick from the man page to pivot without a needing temporary
		//    directory to hold the old root
		log.Info("applying pivot root to rootfs")
		if err := syscall.Chdir(rootfsPath); err != nil {
			return err
		}
		if err := syscall.PivotRoot(".", "."); err != nil {
			return fmt.Errorf("failed to pivot root: %w", err)
		}
		if err := syscall.Chdir("/"); err != nil {
			return err
		}
		if err := syscall.Unmount(".", syscall.MNT_DETACH); err != nil {
			return fmt.Errorf("failed to unmount old rootfs: %w", err)
		}

		// 4. create mounts from the OCI config
		for _, m := range config.Mounts {
			log.Info("creating mount from config", "m", m.Destination)

			// ensure mount directory exists
			if err := os.MkdirAll(m.Destination, 0755); err != nil {
				return fmt.Errorf("failed to create mount directory at %s: %w", m.Destination, err)
			}

			// it's difficult to use cgroup v1 on a host with v2
			if m.Type == "cgroup" {
				m.Type = "cgroup2"
			}

			// mount
			flags, data, err := ParseMountFlagsAndDataFromOptions(m.Options)
			if err != nil {
				return fmt.Errorf("failed to parse mount options for %s: %w", m.Destination, err)
			}
			if err := syscall.Mount(m.Source, m.Destination, m.Type, flags, data); err != nil {
				return fmt.Errorf("failed to mount %s: %w", m.Destination, err)
			}
		}

		// 5. create default devices
		if err := CreateSpecialDevice("/dev/null", Null); err != nil {
			return err
		}
		if err := CreateSpecialDevice("/dev/zero", Zero); err != nil {
			return err
		}
		if err := CreateSpecialDevice("/dev/full", Full); err != nil {
			return err
		}
		if err := CreateSpecialDevice("/dev/random", Random); err != nil {
			return err
		}
		if err := CreateSpecialDevice("/dev/urandom", URandom); err != nil {
			return err
		}
		if err := CreateSpecialDevice("/dev/tty", TTY); err != nil {
			return err
		}
		// TODO: /dev/console if `terminal: true` in OCI config
		// TODO: /dev/ptmx

		// 6. hostname
		if config.Hostname != "" {
			log.Info("setting hostname", "hostname", config.Hostname)
			syscall.Sethostname([]byte(config.Hostname))
		}

		// 7. masked paths
		// This probably isn't very secure as the empty directory exists inside the rootfs of the container.
		// Normally we would do this _before_ pivot root and use some working directory on the host for each
		// container but I didn't want to do that.
		emptyDir := "/.empty-dir"
		emptyFile := "/.empty-file"
		if err := os.MkdirAll(emptyDir, 07550); err != nil {
			return err
		}
		if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
			return err
		}
		if err := MaskPaths(config.Linux.MaskedPaths, emptyDir, emptyFile); err != nil {
			return fmt.Errorf("failed to mask paths from config: %w", err)
		}

		// 8. read only paths
		for _, roPath := range config.Linux.ReadonlyPaths {
			if err := syscall.Mount(roPath, roPath, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
				return err
			}
			if err := syscall.Mount(roPath, roPath, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
				return err
			}
		}

		// 9. drop privileges
		// TODO

		// 10. execve the container process
		log.Info("executing container process")
		if config.Process.Cwd != "" {
			if err := syscall.Chdir(config.Process.Cwd); err != nil {
				return err
			}
		}
		if len(config.Process.Args) <= 0 {
			return errors.New("no process provided by OCI config")
		}
		if err := syscall.Exec(config.Process.Args[0], config.Process.Args, config.Process.Env); err != nil {
			return fmt.Errorf("failed to execute container process: %w", err)
		}

		return nil
	},
}
