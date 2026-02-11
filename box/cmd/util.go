package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	var dataBuilder strings.Builder
	for _, o := range options {
		dataBuilder.WriteString(o + ",")
	}
	return 0, dataBuilder.String(), nil
}
