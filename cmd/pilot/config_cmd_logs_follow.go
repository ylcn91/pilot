package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ylcn91/pilot/internal/config"
)

// logFollowPollInterval controls how often follow mode checks for new log data.
var logFollowPollInterval = 500 * time.Millisecond

// followLogFile tails the configured log file and streams new lines to stdout
// until the process is interrupted (Ctrl-C / SIGTERM). It errors clearly when
// logging is not directed to a file.
func followLogFile(ctx context.Context, cfg *config.Config) error {
	path := logOutputPath(cfg)
	if path == "" {
		return fmt.Errorf("logs --follow requires logging.output to be a file path; current output is a console stream (configure logging.output in config.yaml)")
	}

	// Own signal-notified context so Ctrl-C exits the follow loop cleanly even
	// though the root command runs without a cancellable context.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Following %s (Ctrl-C to stop)\n", path)
	return tailFile(ctx, path, os.Stdout, logFollowPollInterval)
}

// logOutputPath returns the configured log file path, or "" when logging goes
// to a console stream (stdout/stderr) or is unset.
func logOutputPath(cfg *config.Config) string {
	if cfg == nil || cfg.Logging == nil {
		return ""
	}
	switch cfg.Logging.Output {
	case "", "stdout", "stderr":
		return ""
	default:
		return cfg.Logging.Output
	}
}

// tailFile streams appended bytes from path to w, starting at the current end
// of the file, until ctx is cancelled. New files that don't exist yet are
// waited for. Returns nil on graceful (context) shutdown.
func tailFile(ctx context.Context, path string, w io.Writer, poll time.Duration) error {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	var (
		f      *os.File
		offset int64
	)
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()

	open := func() error {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		// Start at end so we stream only new output, mirroring `tail -f`.
		end, err := file.Seek(0, io.SeekEnd)
		if err != nil {
			_ = file.Close()
			return err
		}
		f = file
		offset = end
		return nil
	}

	if err := open(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		if f == nil {
			if err := open(); err != nil {
				continue // file still not present; keep waiting
			}
		}

		info, err := f.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat log file: %w", err)
		}
		// Detect truncation/rotation: file shrank, so reopen from the start.
		if info.Size() < offset {
			_ = f.Close()
			f = nil
			offset = 0
			continue
		}

		for {
			n, readErr := f.ReadAt(buf, offset)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return fmt.Errorf("failed to write log output: %w", werr)
				}
				offset += int64(n)
			}
			if readErr == io.EOF || n == 0 {
				break
			}
			if readErr != nil && readErr != io.EOF {
				return fmt.Errorf("failed to read log file: %w", readErr)
			}
		}
	}
}
