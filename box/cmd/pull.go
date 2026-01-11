package cmd

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/spf13/cobra"
)

const (
	rootfsPath = "rootfs"
	configFile = "config.json"
)

var pullCmd = &cobra.Command{
	Use:   "pull [uri] [path]",
	Short: "pull a remote image and write a container runtime to disk",
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
		mediaType, err := image.MediaType()
		if err != nil {
			return fmt.Errorf("image has no media type: %w", err)
		}
		if !mediaType.IsImage() {
			return errors.New("the provided URI does not reference an image")
		}

		// extract rootfs to disk
		if err := extractRootFS(image, savePath); err != nil {
			return fmt.Errorf("failed to extract image rootfs: %w", err)
		}

		// write runtime config
		if err := generateConfig(image, savePath); err != nil {
			return fmt.Errorf("failed to generate runtime config: %w, err")
		}

		return nil
	},
}

func extractRootFS(image v1.Image, path string) error {
	base := filepath.Join(path, rootfsPath)

	layers, err := image.Layers()
	if err != nil {
		return fmt.Errorf("failed to get image layers: %w", err)
	}

	for _, layer := range layers {
		ur, err := layer.Uncompressed()
		if err != nil {
			return err
		}
		defer ur.Close()

		tr := tar.NewReader(ur)
		for {
			header, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}

			target := filepath.Join(base, header.Name)

			// check for path traversal
			if !strings.HasPrefix(target, filepath.Clean(base)) {
				return fmt.Errorf("invalid file path: %s", header.Name)
			}

			switch header.Typeflag {

			case tar.TypeDir:
				// create directory
				if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
					return err
				}
			case tar.TypeReg:
				// ensure parent directory exists
				if err := os.MkdirAll(filepath.Dir(target), os.FileMode(0755)); err != nil {
					return err
				}

				// create file
				file, err := os.Create(target)
				if err != nil {
					return err
				}
				if _, err := io.CopyN(file, tr, header.Size); err != nil {
					file.Close()
					return err
				}
				file.Close()
			case tar.TypeSymlink:
				if err := os.Symlink(header.Linkname, target); err != nil {
					if !errors.Is(err, os.ErrExist) {
						return err
					}
				}
			default:
				fmt.Printf("Ignoring unknown tar entry: %d\n", header.Typeflag)
			}
		}
	}

	return nil
}

func generateConfig(image v1.Image, path string) error {
	configPath := filepath.Join(path, configFile)

	// TODO
	config := &specs.Spec{}

	// write to file
	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(config); err != nil {
		return err
	}

	return nil
}
