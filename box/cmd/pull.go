package cmd

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull [uri] [path]",
	Short: "pull a remote image",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		imageURI := args[0]
		savePath := args[1]

		// pull image
		image, err := crane.Pull(imageURI, crane.WithPlatform(&v1.Platform{
			Architecture: "amd64",
			OS:           "linux",
		}))
		if err != nil {
			return fmt.Errorf("failed to pull image: %w", err)
		}

		// print info
		manifest, err := image.Manifest()
		if err != nil {
			return err
		}
		for k, v := range manifest.Annotations {
			fmt.Printf("%s: %s\n", k, v)
		}

		// save image
		if err := crane.SaveOCI(image, savePath); err != nil {
			return fmt.Errorf("failed to save image: %w", err)
		}

		return nil
	},
}
