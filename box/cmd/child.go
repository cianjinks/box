package cmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/vishvananda/netlink"
	"kernel.org/pub/linux/libs/security/libcap/cap"
)

const (
	parentPipeFD = uintptr(3)
	resolvConf   = "/etc/resolv.conf"
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

		// avoid incorrect permissions
		syscall.Umask(0)

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

		// 3. bind mount host /etc/resolv.conf as readonly for DNS before pivot_root
		log.Info("setting up DNS")
		containerResolvPath := filepath.Join(rootfsPath, resolvConf)
		f, err := os.Create(containerResolvPath)
		if err != nil {
			return err
		}
		f.Close()
		if err := syscall.Mount(resolvConf, containerResolvPath, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
			return err
		}
		if err := syscall.Mount(resolvConf, containerResolvPath, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
			return fmt.Errorf("failed to bind mount path as readonly %s: %w", resolvConf, err)
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
		log.Info("creating mounts from OCI config")
		for _, m := range config.Mounts {
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
		log.Info("creating default devices (null, zero, random, etc)")
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
		if err := CreateSpecialDevice("/dev/ptmx", Ptmx); err != nil {
			return err
		}
		// TODO: /dev/console if `terminal: true` in OCI config

		// 6. enforce ownership and mode of some important paths
		syscall.Chown("/", 0, 0)
		syscall.Chmod("/", 0755)
		syscall.Chown("/tmp", 0, 0)
		syscall.Chmod("/tmp", 01777)
		syscall.Chown("/etc", 0, 0)
		syscall.Chmod("/etc", 0755)
		syscall.Chown("/root", 0, 0)
		syscall.Chmod("/root", 0700)
		syscall.Chown("/dev", 0, 0)
		syscall.Chmod("/dev", 0755)

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
				return fmt.Errorf("failed to bind mount path as readonly %s: %w", roPath, err)
			}
		}

		// 9. hostname
		if config.Hostname != "" {
			log.Info("setting hostname", "hostname", config.Hostname)
			syscall.Sethostname([]byte(config.Hostname))
		}

		// 10. wait for parent to setup networking + cgroups (block on pipe)
		pipe := os.NewFile(parentPipeFD, "pipe")
		buf := make([]byte, 1)
		pipe.Read(buf)

		// 11. configure container veth interface now that it has been placed
		//     inside the container namespace by the parent
		// find it
		containerVethLink, err := netlink.LinkByName(ContainerVethName)
		if err != nil {
			return fmt.Errorf("failed to find container veth interface: %w", err)
		}
		// give IP
		addr := &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   net.ParseIP(ContainerIP),
				Mask: net.CIDRMask(BridgePrefix, 32),
			},
		}
		if err := netlink.AddrAdd(containerVethLink, addr); err != nil {
			return fmt.Errorf("failed to add IP address to container veth %s: %w", ContainerIP, err)
		}
		// bring UP
		if err := netlink.LinkSetUp(containerVethLink); err != nil {
			return fmt.Errorf("failed to set container veth UP: %w", err)
		}
		// add default route to bridge
		route := &netlink.Route{
			LinkIndex: containerVethLink.Attrs().Index,
			Gw:        net.ParseIP(BridgeIP),
			Dst:       nil, // default route (0.0.0.0/0)
		}
		if err := netlink.RouteAdd(route); err != nil {
			return fmt.Errorf("failed to add default route to bridge: %w", err)
		}

		// 12. drop privileges
		// https://sites.google.com/site/fullycapable/Home?authuser=0
		// the capabilities APIs are strange:
		//  - ambient and bounding controlled through prctl
		//  - bounding can only be dropped
		//  - effective, permitted, inheritable controlled through capset
		log.Info("dropping privileges / capabilities")

		// 12.1 ambient
		ambientValues, err := ParseCapabilities(config.Process.Capabilities.Ambient)
		if err != nil {
			return fmt.Errorf("failed to parse ambient capabilities from config: %w", err)
		}
		cap.ResetAmbient()
		for _, capability := range ambientValues {
			cap.SetAmbient(true, capability)
		}

		// 12.2 bounding
		boundingValues, err := ParseCapabilities(config.Process.Capabilities.Bounding)
		for c := cap.Value(0); c < cap.NamedCount; c++ {
			v, err := cap.GetBound(c)
			if err != nil {
				return fmt.Errorf("failed to get bounding capabiliy %s: %w", c.String(), err)
			}
			if v && !slices.Contains(boundingValues, c) {
				cap.DropBound(c)
			}
		}

		// 12.3 effective, permitted, inheritable
		set := cap.NewSet()
		effectiveValues, err := ParseCapabilities(config.Process.Capabilities.Effective)
		if err != nil {
			return fmt.Errorf("failed to parse effective capabilities from config: %w", err)
		}
		for _, capability := range effectiveValues {
			set.SetFlag(cap.Effective, true, capability)
		}
		permittedValues, err := ParseCapabilities(config.Process.Capabilities.Permitted)
		if err != nil {
			return fmt.Errorf("failed to parse permitted capabilities from config: %w", err)
		}
		for _, capability := range permittedValues {
			set.SetFlag(cap.Permitted, true, capability)
		}
		inheritableValues, err := ParseCapabilities(config.Process.Capabilities.Inheritable)
		if err != nil {
			return fmt.Errorf("failed to parse inheritable capabilities from config: %w", err)
		}
		for _, capability := range inheritableValues {
			set.SetFlag(cap.Inheritable, true, capability)
		}
		if err := set.SetProc(); err != nil {
			return fmt.Errorf("failed to set effective/permitted/inheritable capabilities of the process: %w", err)
		}

		// 13. execve the container process
		log.Info("executing container process")
		if config.Process.Cwd != "" {
			if err := syscall.Chdir(config.Process.Cwd); err != nil {
				return err
			}
		}
		if len(config.Process.Args) <= 0 {
			return errors.New("no process provided by OCI config")
		}
		// if env provides PATH try to resolve the executable, otherwise pass it directly
		executable := config.Process.Args[0]
		for _, envVar := range config.Process.Env {
			split := strings.Split(envVar, "=")
			if split[0] == "PATH" {
				executable, err = FindExecutable(executable, split[1])
				if err != nil {
					return fmt.Errorf("failed to find executable from config in rootfs: %w", err)
				}
			}
		}
		if err := syscall.Exec(executable, config.Process.Args, config.Process.Env); err != nil {
			return fmt.Errorf("failed to execute container process: %w", err)
		}

		return nil
	},
}
