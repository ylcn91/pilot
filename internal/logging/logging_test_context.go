package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestContextPropagation(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithTaskID(ctx, "TASK-123")
	ctx = ContextWithComponent(ctx, "executor")
	ctx = ContextWithProject(ctx, "pilot")

	if taskID := ctx.Value(taskIDKey); taskID != "TASK-123" {
		t.Errorf("expected task_id=TASK-123, got %v", taskID)
	}
	if component := ctx.Value(componentKey); component != "executor" {
		t.Errorf("expected component=executor, got %v", component)
	}
	if project := ctx.Value(projectKey); project != "pilot" {
		t.Errorf("expected project=pilot, got %v", project)
	}
}

func TestContextWithCorrelationID(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, nil)
	loggerMu.Lock()
	defaultLogger = slog.New(handler)
	loggerMu.Unlock()

	ctx := context.Background()
	ctx = ContextWithCorrelationID(ctx, "req-789")
	ctx = ContextWithTaskID(ctx, "TASK-001")

	WithContext(ctx).Info("processing request")

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["correlation_id"] != "req-789" {
		t.Errorf("expected correlation_id='req-789', got %v", result["correlation_id"])
	}
	if result["task_id"] != "TASK-001" {
		t.Errorf("expected task_id='TASK-001', got %v", result["task_id"])
	}
}

func TestWithContext(t *testing.T) {
	tests := []struct {
		name           string
		setupContext   func(context.Context) context.Context
		expectedFields map[string]string
	}{
		{
			name: "with task_id only",
			setupContext: func(ctx context.Context) context.Context {
				return ContextWithTaskID(ctx, "TASK-001")
			},
			expectedFields: map[string]string{
				"task_id": "TASK-001",
			},
		},
		{
			name: "with component only",
			setupContext: func(ctx context.Context) context.Context {
				return ContextWithComponent(ctx, "gateway")
			},
			expectedFields: map[string]string{
				"component": "gateway",
			},
		},
		{
			name: "with project only",
			setupContext: func(ctx context.Context) context.Context {
				return ContextWithProject(ctx, "pilot")
			},
			expectedFields: map[string]string{
				"project": "pilot",
			},
		},
		{
			name: "with all fields",
			setupContext: func(ctx context.Context) context.Context {
				ctx = ContextWithTaskID(ctx, "TASK-002")
				ctx = ContextWithComponent(ctx, "executor")
				ctx = ContextWithProject(ctx, "my-project")
				return ctx
			},
			expectedFields: map[string]string{
				"task_id":   "TASK-002",
				"component": "executor",
				"project":   "my-project",
			},
		},
		{
			name: "with empty context",
			setupContext: func(ctx context.Context) context.Context {
				return ctx
			},
			expectedFields: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			handler := slog.NewJSONHandler(&buf, nil)
			loggerMu.Lock()
			defaultLogger = slog.New(handler)
			loggerMu.Unlock()

			ctx := tt.setupContext(context.Background())
			WithContext(ctx).Info("test message")

			var result map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
				t.Fatalf("failed to parse JSON output: %v", err)
			}

			for key, expectedValue := range tt.expectedFields {
				if result[key] != expectedValue {
					t.Errorf("expected %s='%s', got %v", key, expectedValue, result[key])
				}
			}
		})
	}
}

func TestContextLoggingFunctions(t *testing.T) {
	tests := []struct {
		name    string
		logFunc func(context.Context, string, ...any)
		level   string
	}{
		{"DebugContext", DebugContext, "DEBUG"},
		{"InfoContext", InfoContext, "INFO"},
		{"WarnContext", WarnContext, "WARN"},
		{"ErrorContext", ErrorContext, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})
			loggerMu.Lock()
			defaultLogger = slog.New(handler)
			loggerMu.Unlock()

			ctx := ContextWithTaskID(context.Background(), "TASK-CTX")
			tt.logFunc(ctx, "context test message")

			var result map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
				t.Fatalf("failed to parse JSON output for %s: %v", tt.name, err)
			}

			if result["level"] != tt.level {
				t.Errorf("expected level=%s, got %v", tt.level, result["level"])
			}
			if result["task_id"] != "TASK-CTX" {
				t.Errorf("expected task_id='TASK-CTX', got %v", result["task_id"])
			}
		})
	}
}
