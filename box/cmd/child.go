package cmd

import (
	"github.com/spf13/cobra"
)

var childCmd = &cobra.Command{
	Use:    "child",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rootfsPath := args[0]

		ctx := cmd.Context()
		log := Logger(ctx)

		log.Info("hi from the child process!", "rootfs", rootfsPath)

		// TODO:
		//  1. Bind mount the rootfs to itself
		//  2. Apply mounts from OCI config
		//  3. chdir(rootfs)
		//  4. pivot_root(".", ".")
		//  5. chdir("/")?
		//  6. umount(".", MNT_DETACH)
		//  7. capset
		//  8. execve (syscall.Exec)

		return nil
	},
}
