package architect

import "testing"

const testMod = "example.com/proj"

func node(path string, imports ...string) PackageNode {
	return PackageNode{ImportPath: path, Imports: imports}
}

// cleanGraph satisfies every default rule: pilotapi imports nothing internal,
// executor does not import config (config imports executor instead).
func cleanGraph() *PackageGraph {
	return NewPackageGraph([]PackageNode{
		node(testMod + "/internal/pilotapi"),
		node(testMod+"/internal/config", testMod+"/internal/executor"),
		node(testMod+"/internal/executor", testMod+"/internal/pilotapi"),
		node(testMod + "/internal/logging"),
	})
}

func TestCheckLayerRules_SilentOnCleanGraph(t *testing.T) {
	got := CheckLayerRules(cleanGraph(), testMod, defaultLayerRules)
	if len(got) != 0 {
		t.Fatalf("clean graph must yield no violations, got %+v", got)
	}
}

func TestCheckLayerRules_FlagsPlantedExecutorToConfig(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		// Planted regression: executor now imports config (forbidden).
		node(testMod+"/internal/executor", testMod+"/internal/config"),
		node(testMod + "/internal/config"),
	})
	got := CheckLayerRules(g, testMod, defaultLayerRules)
	if len(got) != 1 {
		t.Fatalf("planted executor->config must be flagged once, got %+v", got)
	}
	v := got[0]
	if v.Rule.Name != "executor-must-not-import-config" {
		t.Errorf("wrong rule fired: %s", v.Rule.Name)
	}
	if v.From != testMod+"/internal/executor" || v.To != testMod+"/internal/config" {
		t.Errorf("violation edge = %s -> %s", v.From, v.To)
	}
}

func TestCheckLayerRules_FlagsPlantedPilotapiLeak(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		// Planted regression: leaf pilotapi now imports another internal pkg.
		node(testMod+"/internal/pilotapi", testMod+"/internal/logging"),
		node(testMod + "/internal/logging"),
	})
	got := CheckLayerRules(g, testMod, defaultLayerRules)
	if len(got) != 1 {
		t.Fatalf("pilotapi leaf leak must be flagged once, got %+v", got)
	}
	if got[0].Rule.Name != "pilotapi-is-a-leaf" {
		t.Errorf("wrong rule fired: %s", got[0].Rule.Name)
	}
}

// TestCheckLayerRules_PilotapiSubpackageImportAllowed proves the leaf rule does
// not fire on pilotapi importing its own sub-package (still "under From", so
// excluded by the !underPrefix(imp, from) guard).
func TestCheckLayerRules_PilotapiInternalSubpackageNotFlagged(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		node(testMod+"/internal/pilotapi", testMod+"/internal/pilotapi/sub"),
		node(testMod + "/internal/pilotapi/sub"),
	})
	got := CheckLayerRules(g, testMod, defaultLayerRules)
	if len(got) != 0 {
		t.Fatalf("pilotapi importing its own sub-package is allowed, got %+v", got)
	}
}

// TestUnderPrefix_NoFalsePrefixMatch guards the classic prefix bug where
// "internal/configx" looks like it is under "internal/config".
func TestUnderPrefix_NoFalsePrefixMatch(t *testing.T) {
	if underPrefix("internal/configx", "internal/config") {
		t.Fatal("configx must not match the config prefix")
	}
	if !underPrefix("internal/config", "internal/config") {
		t.Fatal("exact match must hold")
	}
	if !underPrefix("internal/config/sub", "internal/config") {
		t.Fatal("sub-package must match")
	}
}

func TestCheckLayerRules_CustomRule(t *testing.T) {
	rules := []LayerRule{
		{Name: "ui-no-db", From: "internal/ui", To: "internal/db", Reason: "presentation must not touch storage"},
	}
	g := NewPackageGraph([]PackageNode{
		node(testMod+"/internal/ui", testMod+"/internal/db"),
		node(testMod + "/internal/db"),
	})
	got := CheckLayerRules(g, testMod, rules)
	if len(got) != 1 || got[0].Rule.Name != "ui-no-db" {
		t.Fatalf("custom rule did not fire as expected: %+v", got)
	}
}

func TestJoinPrefix(t *testing.T) {
	if got := joinPrefix("m", "internal/x"); got != "m/internal/x" {
		t.Errorf("joinPrefix = %q", got)
	}
	if got := joinPrefix("", "internal/x"); got != "internal/x" {
		t.Errorf("empty module prefix should pass through: %q", got)
	}
}

// TestCheckLayerRules_NoModulePrefix lets rules work in tests that build graphs
// with bare internal/* paths (no module prefix).
func TestCheckLayerRules_NoModulePrefix(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		node("internal/executor", "internal/config"),
		node("internal/config"),
	})
	got := CheckLayerRules(g, "", defaultLayerRules)
	if len(got) != 1 {
		t.Fatalf("bare-path violation must be flagged, got %+v", got)
	}
}
