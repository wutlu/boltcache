// Package logger configures structured logging (log/slog) and prints the
// startup banner.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/wutlu/boltcache/config"
)

// Setup builds a logger from the logging configuration and installs it as
// the slog default. The returned closer releases the log file, if any.
func Setup(cfg config.LoggingConfig) (*slog.Logger, io.Closer, error) {
	var out io.Writer = os.Stderr
	var closer io.Closer = io.NopCloser(nil)
	if cfg.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.File), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create log directory: %w", err)
		}
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file: %w", err)
		}
		out, closer = f, f
	}
	opts := &slog.HandlerOptions{Level: ParseLevel(cfg.Level)}
	var h slog.Handler
	if strings.EqualFold(cfg.Format, "json") {
		h = slog.NewJSONHandler(out, opts)
	} else {
		h = slog.NewTextHandler(out, opts)
	}
	l := slog.New(h)
	slog.SetDefault(l)
	return l, closer, nil
}

// ParseLevel converts a level name; unknown names mean info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}
