package cmd

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var (
	logJSON bool
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:           "box",
	Short:         "Box is a toy container runtime",
	SilenceErrors: true,
	SilenceUsage:  true,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		log := newLogger(logJSON, verbose)

		slog.SetDefault(log)

		ctx := context.WithValue(cmd.Context(), loggerKey{}, log)
		cmd.SetContext(ctx)

		return nil
	},
}

func newLogger(json bool, verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if json {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		slog.Error("command failure", "err", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&logJSON, "json", false, "enable JSON format logging")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose logging")

	rootCmd.AddCommand(pullCmd)
	rootCmd.AddCommand(createCmd)
}
