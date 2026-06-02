package health

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// agentDocWarnLines is the line count at which a .agent/*.md file triggers a warning.
const agentDocWarnLines = 500

// agentDocFailLines is the line count at which a .agent/*.md file triggers an error.
const agentDocFailLines = 1000

// checkAgentDocSize walks agentDir for .md files and returns ConfigChecks for
// files that exceed the warn or fail thresholds.
func checkAgentDocSize(agentDir string) []ConfigCheck {
	var checks []ConfigCheck

	_ = filepath.WalkDir(agentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lineCount := strings.Count(string(data), "\n")
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lineCount++
		}
		if lineCount <= agentDocWarnLines {
			return nil
		}

		rel, _ := filepath.Rel(filepath.Dir(agentDir), path)
		if rel == "" {
			rel = path
		}

		if lineCount > agentDocFailLines {
			checks = append(checks, ConfigCheck{
				Name:    rel,
				Status:  StatusError,
				Message: fmt.Sprintf("%d lines (limit: %d)", lineCount, agentDocFailLines),
				Fix:     fmt.Sprintf("Archive or trim %s to under %d lines", rel, agentDocFailLines),
			})
		} else {
			checks = append(checks, ConfigCheck{
				Name:    rel,
				Status:  StatusWarning,
				Message: fmt.Sprintf("%d lines (warn at: %d)", lineCount, agentDocWarnLines),
				Fix:     fmt.Sprintf("Consider archiving sections of %s", rel),
			})
		}
		return nil
	})

	return checks
}

// httpGetter is an injectable HTTP GET function for testability.
type httpGetter func(url string) (*http.Response, error)

// brewTapHTTPGet is the default HTTP getter. Override in tests.
var brewTapHTTPGet httpGetter = func(url string) (*http.Response, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pilot-doctor/1.0")
	return client.Do(req)
}

// checkBrewTapHealth checks whether the last release.yml run failed at a
// homebrew step, which indicates that HOMEBREW_TAP_GITHUB_TOKEN has expired.
// Uses unauthenticated GitHub API calls (public repo, 60 req/hour limit).
func checkBrewTapHealth(get httpGetter) ConfigCheck {
	const checkName = "brew-tap-token"
	const runsURL = "https://api.github.com/repos/ylcn91/pilot/actions/workflows/release.yml/runs?per_page=1"

	resp, err := get(runsURL)
	if err != nil {
		return ConfigCheck{Name: checkName, Status: StatusWarning, Message: "could not reach GitHub API"}
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return ConfigCheck{
			Name:    checkName,
			Status:  StatusWarning,
			Message: fmt.Sprintf("GitHub API returned %d", resp.StatusCode),
		}
	}

	var runsPayload struct {
		WorkflowRuns []struct {
			ID         int64  `json:"id"`
			Conclusion string `json:"conclusion"`
		} `json:"workflow_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&runsPayload); err != nil || len(runsPayload.WorkflowRuns) == 0 {
		return ConfigCheck{Name: checkName, Status: StatusOK, Message: "no recent release runs"}
	}

	lastRun := runsPayload.WorkflowRuns[0]
	if lastRun.Conclusion != "failure" {
		return ConfigCheck{
			Name:    checkName,
			Status:  StatusOK,
			Message: fmt.Sprintf("last release: %s", lastRun.Conclusion),
		}
	}

	// Run failed — check if the failed step name contains "homebrew".
	jobsURL := fmt.Sprintf("https://api.github.com/repos/ylcn91/pilot/actions/runs/%d/jobs", lastRun.ID)
	jobsResp, err := get(jobsURL)
	if err != nil || jobsResp.StatusCode != http.StatusOK {
		return ConfigCheck{
			Name:    checkName,
			Status:  StatusWarning,
			Message: "last release failed (could not fetch job steps)",
		}
	}
	defer jobsResp.Body.Close() //nolint:errcheck

	var jobsPayload struct {
		Jobs []struct {
			Steps []struct {
				Name       string `json:"name"`
				Conclusion string `json:"conclusion"`
			} `json:"steps"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(jobsResp.Body).Decode(&jobsPayload); err != nil {
		return ConfigCheck{
			Name:    checkName,
			Status:  StatusWarning,
			Message: "last release failed (could not parse step data)",
		}
	}

	for _, job := range jobsPayload.Jobs {
		for _, step := range job.Steps {
			if step.Conclusion == "failure" && strings.Contains(strings.ToLower(step.Name), "homebrew") {
				return ConfigCheck{
					Name:    checkName,
					Status:  StatusWarning,
					Message: fmt.Sprintf("last release failed at %q — HOMEBREW_TAP_GITHUB_TOKEN may be expired", step.Name),
					Fix:     "Rotate HOMEBREW_TAP_GITHUB_TOKEN in GitHub repo secrets",
				}
			}
		}
	}

	return ConfigCheck{Name: checkName, Status: StatusOK, Message: "last release failed (not at brew step)"}
}
