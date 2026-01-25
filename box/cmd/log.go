package cmd

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

func Logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
