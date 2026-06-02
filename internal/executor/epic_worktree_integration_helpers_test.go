package executor

import (
	"log/slog"
	"os"
)

// testLogger creates a logger for testing that suppresses most output.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
