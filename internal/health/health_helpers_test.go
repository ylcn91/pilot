package health

import (
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func findCheck(checks []Check, name string) *Check {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

// ghAuthFallbackStatus returns the expected status when config token is empty.
// If gh CLI is authenticated on the host, the fallback makes it OK; otherwise error.
func ghAuthFallbackStatus(t *testing.T) Status {
	t.Helper()
	err := exec.Command("gh", "auth", "status").Run()
	if err == nil {
		return StatusOK
	}
	return StatusError
}

// ghAuthFallbackCheckName returns the expected check name when config token is empty.
// If gh CLI is authenticated, the fallback passes and the check is named "github";
// otherwise it fails as "github.token".
func ghAuthFallbackCheckName(t *testing.T) string {
	t.Helper()
	err := exec.Command("gh", "auth", "status").Run()
	if err == nil {
		return "github"
	}
	return "github.token"
}

// checkNames returns all check names for error messages
func checkNames(checks []ConfigCheck) []string {
	names := make([]string, len(checks))
	for i, c := range checks {
		names[i] = c.Name
	}
	return names
}

func findConfigCheck(checks []ConfigCheck, name string) *ConfigCheck {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

func findFeature(features []FeatureStatus, name string) *FeatureStatus {
	for i := range features {
		if features[i].Name == name {
			return &features[i]
		}
	}
	return nil
}

// makeLines builds a string with n newline-terminated lines of "x".
func makeLines(n int) string {
	return strings.Repeat("x\n", n)
}

// makeBrewTapGetter returns an httpGetter that serves sequential responses
// from the provided (statusCode, body) pairs.
func makeBrewTapGetter(responses []struct {
	code int
	body string
}) httpGetter {
	idx := 0
	return func(_ string) (*http.Response, error) {
		if idx >= len(responses) {
			return nil, io.EOF
		}
		r := responses[idx]
		idx++
		return &http.Response{
			StatusCode: r.code,
			Body:       io.NopCloser(strings.NewReader(r.body)),
		}, nil
	}
}
