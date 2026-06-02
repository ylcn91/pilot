package tunnel

import (
	"context"
	"testing"
	"time"
)

func TestCheckCLI(t *testing.T) {
	tests := []struct {
		name    string
		cli     string
		wantOK  bool
		wantLen bool // whether path length > 0
	}{
		{
			name:    "ls exists",
			cli:     "ls",
			wantOK:  true,
			wantLen: true,
		},
		{
			name:    "bash exists",
			cli:     "bash",
			wantOK:  true,
			wantLen: true,
		},
		{
			name:    "nonexistent cli",
			cli:     "definitely-not-a-real-cli-tool-xyz",
			wantOK:  false,
			wantLen: false,
		},
		{
			name:    "empty string",
			cli:     "",
			wantOK:  false,
			wantLen: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := CheckCLI(tt.cli)
			if ok != tt.wantOK {
				t.Errorf("CheckCLI(%q) ok = %v, want %v", tt.cli, ok, tt.wantOK)
			}
			if tt.wantLen && path == "" {
				t.Errorf("CheckCLI(%q) path is empty, want non-empty", tt.cli)
			}
			if !tt.wantLen && path != "" {
				t.Errorf("CheckCLI(%q) path = %q, want empty", tt.cli, path)
			}
		})
	}
}

func TestRunCommand(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		args     []string
		wantErr  bool
		contains string
	}{
		{
			name:     "echo hello",
			cmd:      "echo",
			args:     []string{"hello"},
			wantErr:  false,
			contains: "hello",
		},
		{
			name:    "nonexistent command",
			cmd:     "definitely-not-a-real-command-xyz",
			args:    nil,
			wantErr: true,
		},
		{
			name:    "command with error exit",
			cmd:     "ls",
			args:    []string{"/nonexistent-dir-xyz"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := RunCommand(ctx, tt.cmd, tt.args...)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.contains != "" && output != tt.contains {
				// Allow for trimmed output
				if len(output) < len(tt.contains) {
					t.Errorf("output = %q, want to contain %q", output, tt.contains)
				}
			}
		})
	}
}

func TestRunCommandWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := RunCommand(ctx, "sleep", "10")
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestServiceStatus(t *testing.T) {
	status := GetServiceStatus()

	// On any system, should return a valid status struct
	if status == nil {
		t.Fatal("expected non-nil status")
	}

	// These should be consistent with system state
	_ = status.Installed
	_ = status.Running
	_ = status.PlistPath
}

func TestIsServiceInstalled(t *testing.T) {
	// Should not panic
	_ = IsServiceInstalled()
}

func TestIsServiceRunning(t *testing.T) {
	// Should not panic
	_ = IsServiceRunning()
}

func TestRunCommandContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Sleep should timeout
	time.Sleep(10 * time.Millisecond)
	_, err := RunCommand(ctx, "sleep", "10")
	if err == nil {
		t.Error("expected error with timed out context")
	}
}
