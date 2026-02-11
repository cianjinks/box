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
	rootfsFolder = "rootfs"
	configFile   = "config.json"
)

var pullCmd = &cobra.Command{
	Use:   "pull uri runtime-bundle-path",
	Short: "pull a remote image and write a container runtime bundle to disk",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		imageURI := args[0]
		savePath := args[1]

		ctx := cmd.Context()
		log := Logger(ctx)

		// pull image
		log.Info("Pulling image", "image", imageURI)
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
		log.Info("Extracting rootfs", "savePath", savePath)
		if err := extractRootFS(image, savePath); err != nil {
			return fmt.Errorf("failed to extract image rootfs: %w", err)
		}

		// write runtime config
		log.Info("Writing runtime config", "savePath", savePath)
		if err := generateConfig(image, savePath); err != nil {
			return fmt.Errorf("failed to generate runtime config: %w", err)
		}

		return nil
	},
}

func extractRootFS(image v1.Image, path string) error {
	base := filepath.Join(path, rootfsFolder)

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
				// create file
				file, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
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

	imageConfig, err := image.ConfigFile()
	if err != nil {
		return fmt.Errorf("failed to get image config: %w", err)
	}

	// Mostly taken from: https://github.com/opencontainers/runc/blob/506a849db794a0ee84ba9fb0d9465d960b62876c/libcontainer/specconv/example.go#L14
	config := &specs.Spec{
		Version: specs.Version,
		Process: &specs.Process{
			Terminal:        true,
			User:            specs.User{}, // TODO
			Args:            imageConfig.Config.Cmd,
			Env:             imageConfig.Config.Env,
			Cwd:             imageConfig.Config.WorkingDir,
			NoNewPrivileges: true,
			Capabilities: &specs.LinuxCapabilities{
				Bounding: []string{
					"CAP_AUDIT_WRITE",
					"CAP_KILL",
					"CAP_NET_BIND_SERVICE",
				},
				Permitted: []string{
					"CAP_AUDIT_WRITE",
					"CAP_KILL",
					"CAP_NET_BIND_SERVICE",
				},
				Effective: []string{
					"CAP_AUDIT_WRITE",
					"CAP_KILL",
					"CAP_NET_BIND_SERVICE",
				},
			},
			Rlimits: []specs.POSIXRlimit{
				{
					Type: "RLIMIT_NOFILE",
					Hard: uint64(1024),
					Soft: uint64(1024),
				},
			},
		},
		Hostname: "box",
		Mounts: []specs.Mount{
			{
				Destination: "/proc",
				Type:        "proc",
				Source:      "proc",
				Options:     nil,
			},
			{
				Destination: "/dev",
				Type:        "tmpfs",
				Source:      "tmpfs",
				Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
			},
			{
				Destination: "/dev/pts",
				Type:        "devpts",
				Source:      "devpts",
				Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"},
			},
			{
				Destination: "/dev/shm",
				Type:        "tmpfs",
				Source:      "shm",
				Options:     []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"},
			},
			{
				Destination: "/dev/mqueue",
				Type:        "mqueue",
				Source:      "mqueue",
				Options:     []string{"nosuid", "noexec", "nodev"},
			},
			{
				Destination: "/sys",
				Type:        "sysfs",
				Source:      "sysfs",
				Options:     []string{"nosuid", "noexec", "nodev", "ro"},
			},
			{
				Destination: "/sys/fs/cgroup",
				Type:        "cgroup",
				Source:      "cgroup",
				Options:     []string{"nosuid", "noexec", "nodev", "relatime", "ro"},
			},
		},
		Linux: &specs.Linux{
			MaskedPaths: []string{
				"/proc/acpi",
				"/proc/asound",
				"/proc/kcore",
				"/proc/keys",
				"/proc/latency_stats",
				"/proc/timer_list",
				"/proc/timer_stats",
				"/proc/sched_debug",
				"/sys/firmware",
				"/proc/scsi",
			},
			ReadonlyPaths: []string{
				"/proc/bus",
				"/proc/fs",
				"/proc/irq",
				"/proc/sys",
				"/proc/sysrq-trigger",
			},
			Resources: &specs.LinuxResources{
				Devices: []specs.LinuxDeviceCgroup{
					{
						Allow:  false,
						Access: "rwm",
					},
				},
			},
			Namespaces: []specs.LinuxNamespace{
				{
					Type: specs.PIDNamespace,
				},
				{
					Type: specs.NetworkNamespace,
				},
				{
					Type: specs.IPCNamespace,
				},
				{
					Type: specs.UTSNamespace,
				},
				{
					Type: specs.MountNamespace,
				},
			},
		},
	}

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
