package cmd

import (
	"errors"
	"fmt"
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

		// 3. create other mounts from the OCI config
		for _, m := range config.Mounts {
			log.Info("creating mount from config", "m", m.Destination)
			_, _, err := ParseMountFlagsAndDataFromOptions(m.Options)
			if err != nil {
				return err
			}
			// if err := syscall.Mount(m.Source, m.Destination, m.Type, flags, data); err != nil {
			// 	return err
			// }
		}

		// 4. pivot_root, we use this trick from the man page to pivot without a needing temporary
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

		// 5. other OCI config
		if config.Hostname != "" {
			log.Info("setting hostname", "hostname", config.Hostname)
			syscall.Sethostname([]byte(config.Hostname))
		}

		// 6. drop priveleges
		// TODO

		// 7. execve the container process
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
