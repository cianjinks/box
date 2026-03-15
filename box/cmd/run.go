package cmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	systemd "github.com/coreos/go-systemd/v22/dbus"
	dbus "github.com/godbus/dbus/v5"
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

var cpuCount int
var memoryMiB int

func init() {
	runCmd.Flags().IntVar(&cpuCount, "cpus", -1, "Limit the number of CPUs available to the container")
	runCmd.Flags().IntVar(&memoryMiB, "mem", -1, "Limit the amount of memory available to the container (in MiB)")
}

var runCmd = &cobra.Command{
	Use:   "run [flags] <container-id> <runtime-bundle-path>",
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

		// 3. place child in cgroup using systemd
		conn, err := systemd.NewWithContext(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to systemd dbus (sorry box doesn't support non-systemd): %w", err)
		}
		defer conn.Close()
		unitName := fmt.Sprintf("box-container-%d.scope", child.Process.Pid)
		properties := []systemd.Property{
			{Name: "PIDs", Value: dbus.MakeVariant([]uint32{uint32(child.Process.Pid)})},
			{Name: "Description", Value: dbus.MakeVariant("Box container scope")},
		}
		if cpuCount != -1 && cpuCount > 1 && cpuCount <= runtime.NumCPU() {
			properties = append(properties, systemd.Property{
				Name:  "CPUQuotaPerSecUSec",
				Value: dbus.MakeVariant(uint64(cpuCount) * 100000),
			})
		}
		if memoryMiB != -1 {
			properties = append(properties, systemd.Property{
				Name: "MemoryMax", Value: dbus.MakeVariant(uint64(memoryMiB) * 1048576),
			})
		}
		doneChan := make(chan string, 1)
		if _, err := conn.StartTransientUnitContext(ctx, unitName, "replace", properties, doneChan); err != nil {
			return fmt.Errorf("failed to start transient unit for container: %w", err)
		}
		select {
		case <-doneChan:
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for start transient unit: %w", ctx.Err())
		}

		// 4. signal child to continue
		w.Close()

		// 5. wait for exit
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
