package quality

import (
	"testing"
	"time"
)

func TestConfig_GetGate(t *testing.T) {
	config := &Config{
		Gates: []*Gate{
			{Name: "build", Type: GateBuild},
			{Name: "test", Type: GateTest},
		},
	}

	// Found
	gate := config.GetGate("build")
	if gate == nil {
		t.Fatal("expected to find build gate")
	}
	if gate.Type != GateBuild {
		t.Errorf("expected type %s, got %s", GateBuild, gate.Type)
	}

	// Not found
	gate = config.GetGate("nonexistent")
	if gate != nil {
		t.Error("expected nil for nonexistent gate")
	}
}

func TestConfig_GetRequiredGates(t *testing.T) {
	config := &Config{
		Gates: []*Gate{
			{Name: "build", Required: true},
			{Name: "test", Required: true},
			{Name: "lint", Required: false},
		},
	}

	required := config.GetRequiredGates()
	if len(required) != 2 {
		t.Errorf("expected 2 required gates, got %d", len(required))
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Gates: []*Gate{
					{Name: "build", Command: "make build"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			config: &Config{
				Gates: []*Gate{
					{Name: "", Command: "make build"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing command",
			config: &Config{
				Gates: []*Gate{
					{Name: "build", Command: ""},
				},
			},
			wantErr: true,
		},
		{
			name: "empty gates",
			config: &Config{
				Gates: []*Gate{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled {
		t.Error("expected disabled by default")
	}

	if len(config.Gates) != 3 {
		t.Errorf("expected 3 default gates, got %d", len(config.Gates))
	}

	// Check default gates exist
	gates := make(map[string]*Gate)
	for _, g := range config.Gates {
		gates[g.Name] = g
	}

	if gates["build"] == nil {
		t.Error("expected build gate")
	}
	if gates["test"] == nil {
		t.Error("expected test gate")
	}
	if gates["lint"] == nil {
		t.Error("expected lint gate")
	}

	// lint should not be required by default
	if gates["lint"].Required {
		t.Error("expected lint to not be required by default")
	}

	// build and test should be required
	if !gates["build"].Required {
		t.Error("expected build to be required")
	}
	if !gates["test"].Required {
		t.Error("expected test to be required")
	}
}

func TestMinimalBuildGate(t *testing.T) {
	config := MinimalBuildGate()

	if !config.Enabled {
		t.Error("expected minimal build gate to be enabled")
	}

	if len(config.Gates) != 1 {
		t.Errorf("expected 1 gate, got %d", len(config.Gates))
	}

	gate := config.Gates[0]
	if gate.Name != "build" {
		t.Errorf("expected gate name 'build', got '%s'", gate.Name)
	}
	if gate.Type != GateBuild {
		t.Errorf("expected gate type %s, got %s", GateBuild, gate.Type)
	}
	if !gate.Required {
		t.Error("expected build gate to be required")
	}
	if gate.Timeout != 3*time.Minute {
		t.Errorf("expected 3 minute timeout, got %v", gate.Timeout)
	}
	if gate.MaxRetries != 1 {
		t.Errorf("expected 1 max retry, got %d", gate.MaxRetries)
	}

	// Check failure config
	if config.OnFailure.Action != ActionRetry {
		t.Errorf("expected ActionRetry, got %s", config.OnFailure.Action)
	}
	if config.OnFailure.MaxRetries != 1 {
		t.Errorf("expected 1 max retry in failure config, got %d", config.OnFailure.MaxRetries)
	}
}
