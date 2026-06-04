package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfigValueNode(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantTag string
		wantVal string
		wantSeq bool
	}{
		{"bool true", "true", "!!bool", "true", false},
		{"bool false", "false", "!!bool", "false", false},
		{"int", "42", "!!int", "42", false},
		{"float", "3.14", "!!float", "3.14", false},
		{"plain string", "hello", "!!str", "hello", false},
		{"empty string", "", "!!str", "", false},
		{"string with spaces preserved", "  spaced  ", "!!str", "  spaced  ", false},
		{"version-looking string stays string", "1.2.3", "!!str", "1.2.3", false},
		{"flow sequence", `["Bug","Task"]`, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := configValueNode(tt.raw)
			if tt.wantSeq {
				if node.Kind != yaml.SequenceNode {
					t.Fatalf("expected sequence node, got kind %d", node.Kind)
				}
				if len(node.Content) != 2 {
					t.Fatalf("expected 2 sequence items, got %d", len(node.Content))
				}
				return
			}
			if node.Kind != yaml.ScalarNode {
				t.Fatalf("expected scalar node, got kind %d", node.Kind)
			}
			if node.Tag != tt.wantTag {
				t.Errorf("tag = %q, want %q", node.Tag, tt.wantTag)
			}
			if node.Value != tt.wantVal {
				t.Errorf("value = %q, want %q", node.Value, tt.wantVal)
			}
		})
	}
}

// rootOf parses YAML into the mapping root used by setYAMLPath.
func rootOf(t *testing.T, src string) (*yaml.Node, *yaml.Node) {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &doc, configDocumentRoot(&doc)
}

func marshalNode(t *testing.T, doc *yaml.Node) string {
	t.Helper()
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

func TestSetYAMLPathScalar(t *testing.T) {
	doc, root := rootOf(t, "gateway:\n  port: 8080\n")
	if err := setYAMLPath(root, "gateway.port", configValueNode("9090")); err != nil {
		t.Fatal(err)
	}
	out := marshalNode(t, doc)
	if !strings.Contains(out, "port: 9090") {
		t.Errorf("expected updated port, got:\n%s", out)
	}
}

func TestSetYAMLPathNestedCreatesPath(t *testing.T) {
	doc, root := rootOf(t, "gateway:\n  port: 8080\n")
	if err := setYAMLPath(root, "adapters.github.enabled", configValueNode("true")); err != nil {
		t.Fatal(err)
	}
	out := marshalNode(t, doc)
	// Existing key must survive.
	if !strings.Contains(out, "port: 8080") {
		t.Errorf("unrelated key lost:\n%s", out)
	}
	// New nested path created with a bool.
	var parsed struct {
		Adapters struct {
			GitHub struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"github"`
		} `yaml:"adapters"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if !parsed.Adapters.GitHub.Enabled {
		t.Errorf("expected adapters.github.enabled=true:\n%s", out)
	}
}

func TestSetYAMLPathPreservesComments(t *testing.T) {
	src := "# top comment\ngateway:\n  # port comment\n  port: 8080\nother: keep\n"
	doc, root := rootOf(t, src)
	if err := setYAMLPath(root, "gateway.port", configValueNode("9090")); err != nil {
		t.Fatal(err)
	}
	out := marshalNode(t, doc)
	if !strings.Contains(out, "# top comment") {
		t.Errorf("top comment lost:\n%s", out)
	}
	if !strings.Contains(out, "# port comment") {
		t.Errorf("inline comment lost:\n%s", out)
	}
	if !strings.Contains(out, "other: keep") {
		t.Errorf("unrelated key lost:\n%s", out)
	}
}

func TestSetYAMLPathSequenceIndex(t *testing.T) {
	doc, root := rootOf(t, "projects:\n  - name: a\n")
	if err := setYAMLPath(root, "projects.0.name", configValueNode("renamed")); err != nil {
		t.Fatal(err)
	}
	out := marshalNode(t, doc)
	if !strings.Contains(out, "name: renamed") {
		t.Errorf("sequence element not updated:\n%s", out)
	}
}

func TestSetYAMLPathEmptySegmentRejected(t *testing.T) {
	_, root := rootOf(t, "a: 1\n")
	if err := setYAMLPath(root, "a..b", configValueNode("x")); err == nil {
		t.Error("expected error for empty path segment")
	}
}

func TestSetYAMLPathOverwritesScalarWithMapping(t *testing.T) {
	doc, root := rootOf(t, "adapters: scalar\n")
	if err := setYAMLPath(root, "adapters.github.enabled", configValueNode("true")); err != nil {
		t.Fatal(err)
	}
	out := marshalNode(t, doc)
	var parsed struct {
		Adapters struct {
			GitHub struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"github"`
		} `yaml:"adapters"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if !parsed.Adapters.GitHub.Enabled {
		t.Errorf("scalar not replaced by mapping path:\n%s", out)
	}
}

// End-to-end: run `config set` against a temp file and assert the file round-trips,
// unrelated keys survive, and types are applied.
func TestConfigSetCmdRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	initial := strings.Join([]string{
		"# Pilot config",
		"gateway:",
		"  host: localhost",
		"  port: 8080",
		"orchestrator:",
		"  max_concurrent_tasks: 1",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}

	oldCfgFile := cfgFile
	cfgFile = path
	defer func() { cfgFile = oldCfgFile }()

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{
		"gateway.port", "9090",
		"gateway.host", "0.0.0.0",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)

	// Comment preserved.
	if !strings.Contains(out, "# Pilot config") {
		t.Errorf("comment lost:\n%s", out)
	}
	// Unrelated key preserved.
	if !strings.Contains(out, "max_concurrent_tasks: 1") {
		t.Errorf("unrelated key lost:\n%s", out)
	}

	// Reparse to confirm typed values.
	var parsed struct {
		Gateway struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
		} `yaml:"gateway"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if parsed.Gateway.Port != 9090 {
		t.Errorf("port = %d, want 9090", parsed.Gateway.Port)
	}
	if parsed.Gateway.Host != "0.0.0.0" {
		t.Errorf("host = %q, want 0.0.0.0", parsed.Gateway.Host)
	}

	// File should round-trip cleanly through the loader without error.
	if err := cmd.Execute(); err != nil {
		t.Fatalf("second config set failed (round-trip regression): %v", err)
	}
}

func TestConfigSetCmdRejectsOddArgs(t *testing.T) {
	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"only-key"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Error("expected error for odd number of args")
	}
}
