package cmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/vishvananda/netlink"
)

const (
	bridgeName        = "bridge-box"
	BridgeIP          = "10.0.0.171"
	BridgePrefix      = 24
	hostVethName      = "veth-box-host"
	ContainerVethName = "veth-box-cont"
	ContainerIP       = "10.0.0.172"
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

		// 1. configure exec
		child := exec.Command("/proc/self/exe", "child", runtimePath)
		// TODO: use a PTY
		child.Stdin = os.Stdin
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		r, w, _ := os.Pipe() // create a pipe to communicate with the child
		child.ExtraFiles = []*os.File{r}
		child.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: CloneFlagsFromNamespaces(config.Linux.Namespaces),
			// TODO: `UseCgroupFD` and `CgroupFD`
		}
		if err := child.Start(); err != nil {
			return fmt.Errorf("failed to start child process: %w", err)
		}

		// 2. setup container networking
		// create bridge
		bridgeAttrs := netlink.NewLinkAttrs()
		bridgeAttrs.Name = bridgeName
		if err := netlink.LinkAdd(&netlink.Bridge{LinkAttrs: bridgeAttrs}); err != nil {
			return fmt.Errorf("failed to create bridge interface: %w", err)
		}
		bridgeLink, err := netlink.LinkByName(bridgeName)
		if err != nil {
			return fmt.Errorf("failed to find bridge interface: %w", err)
		}
		addr := &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   net.ParseIP(BridgeIP),
				Mask: net.CIDRMask(BridgePrefix, 32),
			},
		}
		if err := netlink.AddrAdd(bridgeLink, addr); err != nil {
			return fmt.Errorf("failed to add IP address to bridge %s: %w", BridgeIP, err)
		}
		defer netlink.LinkDel(bridgeLink)
		// create veth pair
		hostVethAttrs := netlink.NewLinkAttrs()
		hostVethAttrs.Name = hostVethName
		if err := netlink.LinkAdd(&netlink.Veth{LinkAttrs: hostVethAttrs, PeerName: ContainerVethName}); err != nil {
			return fmt.Errorf("failed to create container veth pair: %w", err)
		}
		hostVethLink, err := netlink.LinkByName(hostVethName)
		if err != nil {
			return fmt.Errorf("failed to find host veth interface: %w", err)
		}
		containerVethLink, err := netlink.LinkByName(ContainerVethName)
		if err != nil {
			return fmt.Errorf("failed to find container veth interface: %w", err)
		}
		defer netlink.LinkDel(hostVethLink) // destroys container veth too
		// move container veth into container network namespace
		if err := netlink.LinkSetNsPid(containerVethLink, child.Process.Pid); err != nil {
			return fmt.Errorf("failed to move container veth into namespace for pid %d: %w", child.Process.Pid, err)
		}
		// attach host to bridge
		if err := netlink.LinkSetMaster(hostVethLink, bridgeLink); err != nil {
			return fmt.Errorf("failed to attach host veth to bridge: %w", err)
		}
		// bring UP interfaces
		if err := netlink.LinkSetUp(bridgeLink); err != nil {
			return fmt.Errorf("failed to set bridge UP: %w", err)
		}
		if err := netlink.LinkSetUp(hostVethLink); err != nil {
			return fmt.Errorf("failed to set host veth UP: %w", err)
		}
		// setup NAT
		ipForwardEnabled, err := SetupNAT(ContainerIP)
		if err != nil {
			return fmt.Errorf("failed to setup container NAT: %w", err)
		}
		defer CleanupNAT(ContainerIP, ipForwardEnabled)

		// 3. signal child to continue
		w.Close()

		// 4. wait for exit
		if err := child.Wait(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 130 {
				// Ignore Ctrl+D
				return nil
			}
			return fmt.Errorf("error waiting for child process: %w", err)
		}

		return nil
	},
}
