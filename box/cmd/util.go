package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
	"kernel.org/pub/linux/libs/security/libcap/cap"
)

// GetConfigAndRootFromRuntimePath expects an OCI runtime bundle at `runtimePath`. It returns the
// config and rootfs path from there if they exist.
func GetConfigAndRootFromRuntimePath(runtimePath string) (*specs.Spec, string, error) {
	// read config
	config := &specs.Spec{}
	configPath := filepath.Join(runtimePath, configFile)

	file, err := os.Open(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open runtime config file: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(config); err != nil {
		return nil, "", fmt.Errorf("failed to decode runtime config file: %w", err)
	}

	// path to rootfs
	rootfsPath, err := filepath.Abs(filepath.Join(runtimePath, rootfsFolder))
	if err != nil {
		return nil, "", fmt.Errorf("failed to make absolute path from rootfs path: %w", err)
	}

	return config, rootfsPath, nil
}

// The mount options in an OCI runtime config include both flags and data and we must manually
// determine which is which. List taken from mount(2).
var mountFlagMap = map[string]uintptr{
	// main flags:
	"remount": syscall.MS_REMOUNT,
	"bind":    syscall.MS_BIND,
	// propagation flags:
	"shared":     syscall.MS_SHARED,
	"private":    syscall.MS_PRIVATE,
	"slave":      syscall.MS_SLAVE,
	"unbindable": syscall.MS_UNBINDABLE,
	// additional flags:
	"dirsync":  syscall.MS_DIRSYNC,
	"lazytime": 0x2000000,
	// TODO: MS_MANDLOCK?
	"noatime":    syscall.MS_NOATIME,
	"nodev":      syscall.MS_NODEV,
	"nodiratime": syscall.MS_NODIRATIME,
	"noexec":     syscall.MS_NOEXEC,
	"nosuid":     syscall.MS_NOSUID,
	"ro":         syscall.MS_RDONLY,
	// TODO: MS_REC?
	"relatime":    syscall.MS_RELATIME,
	"silent":      syscall.MS_SILENT,
	"strictatime": syscall.MS_STRICTATIME,
	// TODO: MS_SYNCHRONOUS?
	"nosymfollow": 0x100,
}

// ParseMountFlagsAndDataFromOptions expects a list of mount options from an OCI runtime config. It
// converts these options into the corresponding `flags` and `data` parameters for the mount syscall.
// See: https://github.com/opencontainers/runtime-spec/blob/main/config.md#linux-mount-options
func ParseMountFlagsAndDataFromOptions(options []string) (uintptr, string, error) {
	var flags uintptr
	var dataBuilder strings.Builder

	for _, o := range options {
		if flag, ok := mountFlagMap[o]; ok {
			flags |= flag
		} else {
			dataBuilder.WriteString(o + ",")

		}
	}

	return flags, dataBuilder.String(), nil
}

var namespaceFlagMap = map[specs.LinuxNamespaceType]uintptr{
	specs.PIDNamespace:     syscall.CLONE_NEWPID,
	specs.NetworkNamespace: syscall.CLONE_NEWNET,
	specs.MountNamespace:   syscall.CLONE_NEWNS,
	specs.IPCNamespace:     syscall.CLONE_NEWIPC,
	specs.UTSNamespace:     syscall.CLONE_NEWUTS,
	specs.UserNamespace:    syscall.CLONE_NEWUSER,
	specs.CgroupNamespace:  syscall.CLONE_NEWCGROUP,
	specs.TimeNamespace:    syscall.CLONE_NEWTIME,
}

// CloneFlagsFromNamespaces takes a list of namespaces from an OCI runtime config and returns
// the corresponding flags bitmask for unshare(2).
func CloneFlagsFromNamespaces(namespaces []specs.LinuxNamespace) uintptr {
	var flags uintptr
	for _, ns := range namespaces {
		if flag, ok := namespaceFlagMap[ns.Type]; ok {
			flags |= flag
		}
	}
	return flags
}

type SpecialDevice int

const (
	Null    SpecialDevice = (1 << 8) | 3
	Zero    SpecialDevice = (1 << 8) | 5
	Full    SpecialDevice = (1 << 8) | 7
	Random  SpecialDevice = (1 << 8) | 8
	URandom SpecialDevice = (1 << 8) | 9
	TTY     SpecialDevice = (5 << 8) | 0
)

func CreateSpecialDevice(path string, dev SpecialDevice) error {
	if err := syscall.Mknod(path, syscall.S_IFCHR|0666, int(dev)); err != nil {
		return fmt.Errorf("failed to create special device at %s: %w", path, err)
	}
	return nil
}

// MaskPaths hides the given set of paths by bind mounting either `dirMask` or `fileMask`
// on top of them, depending on whether the path is a directory or file respectively.
func MaskPaths(paths []string, dirMask string, fileMask string) error {
	for _, maskedPath := range paths {
		stat, err := os.Stat(maskedPath)
		if err != nil {
			// if the path doesn't exist no need to do anything
			return nil
		}
		source := dirMask
		if !stat.IsDir() {
			source = fileMask
		}
		if err := syscall.Mount(source, maskedPath, "", syscall.MS_BIND, ""); err != nil {
			return fmt.Errorf("failed to bind mount path %s: %w", maskedPath, err)
		}
	}
	return nil
}

var capMap = map[string]cap.Value{
	"CAP_AUDIT_WRITE":      cap.AUDIT_WRITE,
	"CAP_KILL":             cap.KILL,
	"CAP_NET_BIND_SERVICE": cap.NET_BIND_SERVICE,
}

// ParseCapabilities takes a list of capability strings from an OCI runtime config and returns
// the corresponding libcap `cap.Value` enum values.
func ParseCapabilities(capabilities []string) ([]cap.Value, error) {
	results := []cap.Value{}
	for _, capability := range capabilities {
		if val, ok := capMap[capability]; ok {
			results = append(results, val)
		} else {
			return []cap.Value{}, fmt.Errorf("found unsupported capability %s", capability)
		}
	}
	return results, nil
}
