package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
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
