package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestJSONOutput(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)

	logger.Info("test message",
		slog.String("component", "test"),
		slog.String("task_id", "TASK-001"),
		slog.Int("tokens", 5000),
	)

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["msg"] != "test message" {
		t.Errorf("expected msg='test message', got %v", result["msg"])
	}
	if result["component"] != "test" {
		t.Errorf("expected component='test', got %v", result["component"])
	}
	if result["task_id"] != "TASK-001" {
		t.Errorf("expected task_id='TASK-001', got %v", result["task_id"])
	}
	if result["level"] != "INFO" {
		t.Errorf("expected level='INFO', got %v", result["level"])
	}
}

func TestWithComponent(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, nil)
	loggerMu.Lock()
	defaultLogger = slog.New(handler)
	loggerMu.Unlock()

	WithComponent("gateway").Info("test message")

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["component"] != "gateway" {
		t.Errorf("expected component='gateway', got %v", result["component"])
	}
}

func TestWithTask(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, nil)
	loggerMu.Lock()
	defaultLogger = slog.New(handler)
	loggerMu.Unlock()

	WithTask("TASK-456").Info("task started")

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["task_id"] != "TASK-456" {
		t.Errorf("expected task_id='TASK-456', got %v", result["task_id"])
	}
}

func TestWithCorrelationID(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, nil)
	loggerMu.Lock()
	defaultLogger = slog.New(handler)
	loggerMu.Unlock()

	WithCorrelationID("abc-123-def").Info("request started")

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["correlation_id"] != "abc-123-def" {
		t.Errorf("expected correlation_id='abc-123-def', got %v", result["correlation_id"])
	}
}

func TestLogLevels(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	loggerMu.Lock()
	defaultLogger = slog.New(handler)
	loggerMu.Unlock()

	tests := []struct {
		logFunc func(string, ...any)
		level   string
	}{
		{Debug, "DEBUG"},
		{Info, "INFO"},
		{Warn, "WARN"},
		{Error, "ERROR"},
	}

	for _, tt := range tests {
		buf.Reset()
		tt.logFunc("test message")

		var result map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse JSON output for %s: %v", tt.level, err)
		}

		if result["level"] != tt.level {
			t.Errorf("expected level=%s, got %v", tt.level, result["level"])
		}
	}
}

func TestWith(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, nil)
	loggerMu.Lock()
	defaultLogger = slog.New(handler)
	loggerMu.Unlock()

	With("custom_key", "custom_value").Info("test message")

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["custom_key"] != "custom_value" {
		t.Errorf("expected custom_key='custom_value', got %v", result["custom_key"])
	}
}

func TestLogLevelsFiltering(t *testing.T) {
	tests := []struct {
		name         string
		configLevel  string
		logLevel     string
		logFunc      func(string, ...any)
		shouldAppear bool
	}{
		{"info config allows info", "info", "INFO", Info, true},
		{"info config allows warn", "info", "WARN", Warn, true},
		{"info config allows error", "info", "ERROR", Error, true},
		{"warn config blocks info", "warn", "INFO", Info, false},
		{"warn config allows warn", "warn", "WARN", Warn, true},
		{"error config blocks warn", "error", "WARN", Warn, false},
		{"error config allows error", "error", "ERROR", Error, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				Level: parseLevel(tt.configLevel),
			})
			loggerMu.Lock()
			defaultLogger = slog.New(handler)
			loggerMu.Unlock()

			tt.logFunc("test message")

			hasContent := buf.Len() > 0
			if hasContent != tt.shouldAppear {
				t.Errorf("expected message to appear: %v, but got: %v", tt.shouldAppear, hasContent)
			}
		})
	}
}

func TestWithMultipleAttributes(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, nil)
	loggerMu.Lock()
	defaultLogger = slog.New(handler)
	loggerMu.Unlock()

	With(
		"key1", "value1",
		"key2", 42,
		"key3", true,
	).Info("multiple attributes")

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["key1"] != "value1" {
		t.Errorf("expected key1='value1', got %v", result["key1"])
	}
	if result["key2"] != float64(42) {
		t.Errorf("expected key2=42, got %v", result["key2"])
	}
	if result["key3"] != true {
		t.Errorf("expected key3=true, got %v", result["key3"])
	}
}

func TestLogLevelsWithArguments(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	loggerMu.Lock()
	defaultLogger = slog.New(handler)
	loggerMu.Unlock()

	tests := []struct {
		logFunc func(string, ...any)
		level   string
	}{
		{Debug, "DEBUG"},
		{Info, "INFO"},
		{Warn, "WARN"},
		{Error, "ERROR"},
	}

	for _, tt := range tests {
		buf.Reset()
		tt.logFunc("test message", "extra_key", "extra_value")

		var result map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse JSON output for %s: %v", tt.level, err)
		}

		if result["extra_key"] != "extra_value" {
			t.Errorf("expected extra_key='extra_value', got %v", result["extra_key"])
		}
	}
}
