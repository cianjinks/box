package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create [container-id] [runtime-bundle-path]",
	Short: "create a container from a runtime bundle on disk",
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

		log.Info("testing", "container", containerId)

		return nil
	},
}
