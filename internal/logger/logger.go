package logger

import (
	"log/slog"
	"os"
)

// Init initializes the global slog default logger to output JSON.
// It also sets the default log level.
func Init() {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
		// Optional: Add Source: true if you want file/line numbers in the logs
		// Source: true,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler)
	
	// Set this JSON logger as the default for slog.Info, slog.Error, etc.
	slog.SetDefault(logger)
}
