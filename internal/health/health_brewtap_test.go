package health

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// checkBrewTapHealth
// ---------------------------------------------------------------------------

func TestCheckBrewTapHealth_NetworkError(t *testing.T) {
	get := func(_ string) (*http.Response, error) { return nil, io.ErrUnexpectedEOF }
	check := checkBrewTapHealth(get)
	if check.Status != StatusWarning {
		t.Errorf("status = %v, want StatusWarning", check.Status)
	}
	if !strings.Contains(check.Message, "could not reach") {
		t.Errorf("message = %q, want mention of 'could not reach'", check.Message)
	}
}

func TestCheckBrewTapHealth_APIError(t *testing.T) {
	get := makeBrewTapGetter([]struct {
		code int
		body string
	}{{503, `{}`}})
	check := checkBrewTapHealth(get)
	if check.Status != StatusWarning {
		t.Errorf("status = %v, want StatusWarning", check.Status)
	}
	if !strings.Contains(check.Message, "503") {
		t.Errorf("message = %q, want HTTP status code", check.Message)
	}
}

func TestCheckBrewTapHealth_LastRunSuccess(t *testing.T) {
	runsBody := `{"workflow_runs":[{"id":1,"conclusion":"success"}]}`
	get := makeBrewTapGetter([]struct {
		code int
		body string
	}{{200, runsBody}})
	check := checkBrewTapHealth(get)
	if check.Status != StatusOK {
		t.Errorf("status = %v, want StatusOK", check.Status)
	}
}

func TestCheckBrewTapHealth_LastRunFailedNoHomebrewStep(t *testing.T) {
	runsBody := `{"workflow_runs":[{"id":42,"conclusion":"failure"}]}`
	jobsBody := `{"jobs":[{"steps":[{"name":"Set up Go","conclusion":"failure"},{"name":"Run GoReleaser","conclusion":"success"}]}]}`
	get := makeBrewTapGetter([]struct {
		code int
		body string
	}{{200, runsBody}, {200, jobsBody}})
	check := checkBrewTapHealth(get)
	if check.Status != StatusOK {
		t.Errorf("status = %v, want StatusOK (failure not at brew step)", check.Status)
	}
}

func TestCheckBrewTapHealth_LastRunFailedAtHomebrewStep(t *testing.T) {
	runsBody := `{"workflow_runs":[{"id":42,"conclusion":"failure"}]}`
	jobsBody := `{"jobs":[{"steps":[{"name":"Publish Homebrew Formula","conclusion":"failure"}]}]}`
	get := makeBrewTapGetter([]struct {
		code int
		body string
	}{{200, runsBody}, {200, jobsBody}})
	check := checkBrewTapHealth(get)
	if check.Status != StatusWarning {
		t.Errorf("status = %v, want StatusWarning", check.Status)
	}
	if check.Fix == "" {
		t.Error("expected Fix hint for brew token rotation")
	}
	if !strings.Contains(check.Message, "Homebrew") {
		t.Errorf("message = %q, want mention of brew step name", check.Message)
	}
}

func TestCheckBrewTapHealth_NoRuns(t *testing.T) {
	runsBody := `{"workflow_runs":[]}`
	get := makeBrewTapGetter([]struct {
		code int
		body string
	}{{200, runsBody}})
	check := checkBrewTapHealth(get)
	if check.Status != StatusOK {
		t.Errorf("status = %v, want StatusOK for empty run list", check.Status)
	}
}

func TestCheckBrewTapHealth_JobFetchFails(t *testing.T) {
	runsBody := `{"workflow_runs":[{"id":42,"conclusion":"failure"}]}`
	get := makeBrewTapGetter([]struct {
		code int
		body string
	}{{200, runsBody}, {503, `{}`}})
	check := checkBrewTapHealth(get)
	if check.Status != StatusWarning {
		t.Errorf("status = %v, want StatusWarning when jobs fetch fails", check.Status)
	}
}
