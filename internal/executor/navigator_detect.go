package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DetectProjectInfo extracts project name and tech stack from config files.
func (n *NavigatorInitializer) DetectProjectInfo(projectPath string) (*ProjectInfo, error) {
	// Try detection methods in order
	detectors := []func(string) *ProjectInfo{
		n.detectFromGoMod,
		n.detectFromPackageJSON,
		n.detectFromPyprojectToml,
		n.detectFromCargoToml,
	}

	for _, detector := range detectors {
		if info := detector(projectPath); info != nil {
			return info, nil
		}
	}

	// Fallback: use directory name
	return &ProjectInfo{
		Name:         filepath.Base(projectPath),
		TechStack:    "Unknown",
		DetectedFrom: "directory_name",
	}, nil
}

// detectFromGoMod detects Go projects.
func (n *NavigatorInitializer) detectFromGoMod(projectPath string) *ProjectInfo {
	goModPath := filepath.Join(projectPath, "go.mod")
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return nil
	}

	// Extract module name
	moduleRe := regexp.MustCompile(`module\s+([^\s]+)`)
	match := moduleRe.FindStringSubmatch(string(content))
	name := filepath.Base(projectPath)
	if len(match) > 1 {
		parts := strings.Split(match[1], "/")
		name = parts[len(parts)-1]
	}

	// Detect stack
	stackParts := []string{"Go"}
	contentStr := string(content)

	if strings.Contains(contentStr, "gin-gonic/gin") {
		stackParts = append(stackParts, "Gin")
	} else if strings.Contains(contentStr, "gorilla/mux") {
		stackParts = append(stackParts, "Gorilla Mux")
	} else if strings.Contains(contentStr, "fiber") {
		stackParts = append(stackParts, "Fiber")
	}

	if strings.Contains(contentStr, "gorm") {
		stackParts = append(stackParts, "GORM")
	}

	if strings.Contains(contentStr, "mattn/go-sqlite3") || strings.Contains(contentStr, "modernc.org/sqlite") {
		stackParts = append(stackParts, "SQLite")
	}

	return &ProjectInfo{
		Name:         name,
		TechStack:    strings.Join(stackParts, ", "),
		DetectedFrom: "go.mod",
	}
}

// detectFromPackageJSON detects Node.js projects.
func (n *NavigatorInitializer) detectFromPackageJSON(projectPath string) *ProjectInfo {
	pkgPath := filepath.Join(projectPath, "package.json")
	content, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil
	}

	var pkg struct {
		Name            string            `json:"name"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	if err := json.Unmarshal(content, &pkg); err != nil {
		return nil
	}

	name := pkg.Name
	if name == "" {
		name = filepath.Base(projectPath)
	}

	// Merge deps
	deps := make(map[string]bool)
	for k := range pkg.Dependencies {
		deps[k] = true
	}
	for k := range pkg.DevDependencies {
		deps[k] = true
	}

	// Detect stack
	var stackParts []string

	if deps["next"] {
		stackParts = append(stackParts, "Next.js")
	} else if deps["react"] {
		stackParts = append(stackParts, "React")
	} else if deps["vue"] {
		stackParts = append(stackParts, "Vue")
	} else if deps["express"] {
		stackParts = append(stackParts, "Express")
	} else {
		stackParts = append(stackParts, "Node.js")
	}

	if deps["typescript"] {
		stackParts = append(stackParts, "TypeScript")
	}

	if deps["prisma"] {
		stackParts = append(stackParts, "Prisma")
	}

	return &ProjectInfo{
		Name:         name,
		TechStack:    strings.Join(stackParts, ", "),
		DetectedFrom: "package.json",
	}
}

// detectFromPyprojectToml detects Python projects.
func (n *NavigatorInitializer) detectFromPyprojectToml(projectPath string) *ProjectInfo {
	pyprojectPath := filepath.Join(projectPath, "pyproject.toml")
	content, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return nil
	}

	contentStr := string(content)

	// Extract name
	nameRe := regexp.MustCompile(`name\s*=\s*["']([^"']+)["']`)
	match := nameRe.FindStringSubmatch(contentStr)
	name := filepath.Base(projectPath)
	if len(match) > 1 {
		name = match[1]
	}

	// Detect stack
	stackParts := []string{"Python"}
	contentLower := strings.ToLower(contentStr)

	if strings.Contains(contentLower, "fastapi") {
		stackParts = append(stackParts, "FastAPI")
	} else if strings.Contains(contentLower, "django") {
		stackParts = append(stackParts, "Django")
	} else if strings.Contains(contentLower, "flask") {
		stackParts = append(stackParts, "Flask")
	}

	return &ProjectInfo{
		Name:         name,
		TechStack:    strings.Join(stackParts, ", "),
		DetectedFrom: "pyproject.toml",
	}
}

// detectFromCargoToml detects Rust projects.
func (n *NavigatorInitializer) detectFromCargoToml(projectPath string) *ProjectInfo {
	cargoPath := filepath.Join(projectPath, "Cargo.toml")
	content, err := os.ReadFile(cargoPath)
	if err != nil {
		return nil
	}

	contentStr := string(content)

	// Extract name
	nameRe := regexp.MustCompile(`name\s*=\s*["']([^"']+)["']`)
	match := nameRe.FindStringSubmatch(contentStr)
	name := filepath.Base(projectPath)
	if len(match) > 1 {
		name = match[1]
	}

	// Detect stack
	stackParts := []string{"Rust"}

	if strings.Contains(contentStr, "actix-web") {
		stackParts = append(stackParts, "Actix Web")
	} else if strings.Contains(contentStr, "axum") {
		stackParts = append(stackParts, "Axum")
	}

	return &ProjectInfo{
		Name:         name,
		TechStack:    strings.Join(stackParts, ", "),
		DetectedFrom: "Cargo.toml",
	}
}
