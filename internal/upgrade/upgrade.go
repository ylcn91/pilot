// Package upgrade provides self-update functionality for Pilot.
// It supports checking for new versions, downloading updates,
// graceful task completion before upgrade, and automatic rollback.
package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// GitHubRepo is the GitHub repository for releases
	GitHubRepo = "ylcn91/pilot"

	// DefaultTimeout for HTTP requests
	DefaultTimeout = 30 * time.Second

	// BackupSuffix for previous version
	BackupSuffix = ".backup"
)

// Release represents a GitHub release
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
	HTMLURL     string    `json:"html_url"`
}

// Asset represents a release asset
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
}

// VersionInfo contains current version information
type VersionInfo struct {
	Current       string
	Latest        string
	LatestRelease *Release
	UpdateAvail   bool
	ReleaseNotes  string
}

// Upgrader handles version checking and self-update
type Upgrader struct {
	currentVersion      string
	httpClient          *http.Client
	binaryPath          string
	backupPath          string
	prepareForExecution func(string) error // injectable for testing; defaults to PrepareForExecution
}

// NewUpgrader creates a new Upgrader instance
func NewUpgrader(currentVersion string) (*Upgrader, error) {
	binaryPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks
	resolvedPath, err := filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	// Detect Homebrew installation
	if isHomebrewPath(resolvedPath) {
		return nil, fmt.Errorf("homebrew installation detected at %s\n\n"+
			"Self-upgrade is not supported for Homebrew installations.\n"+
			"Please use Homebrew to upgrade:\n\n"+
			"  brew upgrade pilot\n\n"+
			"Or reinstall without Homebrew:\n\n"+
			"  brew uninstall pilot\n"+
			"  curl -fsSL https://raw.githubusercontent.com/ylcn91/pilot/main/install.sh | bash",
			resolvedPath)
	}

	return &Upgrader{
		currentVersion:      currentVersion,
		httpClient:          &http.Client{Timeout: DefaultTimeout},
		binaryPath:          resolvedPath,
		backupPath:          resolvedPath + BackupSuffix,
		prepareForExecution: PrepareForExecution,
	}, nil
}

// isHomebrewPath checks if a path is inside a Homebrew installation
func isHomebrewPath(path string) bool {
	homebrewPrefixes := []string{
		"/opt/homebrew/Cellar/",
		"/usr/local/Cellar/",
		"/home/linuxbrew/.linuxbrew/Cellar/",
	}
	for _, prefix := range homebrewPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// CheckVersion checks if a newer version is available
func (u *Upgrader) CheckVersion(ctx context.Context) (*VersionInfo, error) {
	release, err := u.fetchLatestRelease(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release: %w", err)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(u.currentVersion, "v")

	info := &VersionInfo{
		Current:       u.currentVersion,
		Latest:        release.TagName,
		LatestRelease: release,
		UpdateAvail:   compareVersions(current, latest) < 0,
		ReleaseNotes:  release.Body,
	}

	return info, nil
}

// fetchLatestRelease fetches the latest release from GitHub
// Uses /releases endpoint instead of /releases/latest to avoid GitHub API caching
// which can return stale data for several minutes after a new release is created.
func (u *Upgrader) fetchLatestRelease(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=10", GitHubRepo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to parse releases: %w", err)
	}

	// Find first non-draft, non-prerelease release
	for i := range releases {
		if !releases[i].Draft && !releases[i].Prerelease {
			return &releases[i], nil
		}
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found")
	}

	// Fallback to first release if all are drafts/prereleases
	return &releases[0], nil
}

// compareVersions compares two semantic versions
// Returns -1 if a < b, 0 if a == b, 1 if a > b
func compareVersions(a, b string) int {
	aParts := parseVersion(a)
	bParts := parseVersion(b)

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

// parseVersion parses a version string into [major, minor, patch]
func parseVersion(v string) [3]int {
	var parts [3]int
	v = strings.TrimPrefix(v, "v")

	// Handle dirty suffix
	v = strings.Split(v, "-")[0]

	_, _ = fmt.Sscanf(v, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
	return parts
}
