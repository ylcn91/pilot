package azuredevops

import (
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewClient(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "my-org", "my-project")

	if client.pat != testutil.FakeAzureDevOpsPAT {
		t.Errorf("expected PAT %s, got %s", testutil.FakeAzureDevOpsPAT, client.pat)
	}
	if client.organization != "my-org" {
		t.Errorf("expected organization my-org, got %s", client.organization)
	}
	if client.project != "my-project" {
		t.Errorf("expected project my-project, got %s", client.project)
	}
	if client.repository != "my-project" {
		t.Errorf("expected repository to default to project name, got %s", client.repository)
	}
	if client.baseURL != defaultBaseURL {
		t.Errorf("expected baseURL %s, got %s", defaultBaseURL, client.baseURL)
	}
}

func TestNewClientWithConfig(t *testing.T) {
	config := &Config{
		PAT:          testutil.FakeAzureDevOpsPAT,
		Organization: "test-org",
		Project:      "test-project",
		Repository:   "test-repo",
		BaseURL:      "https://azure.example.com",
	}

	client := NewClientWithConfig(config)

	if client.pat != testutil.FakeAzureDevOpsPAT {
		t.Errorf("expected PAT %s, got %s", testutil.FakeAzureDevOpsPAT, client.pat)
	}
	if client.organization != "test-org" {
		t.Errorf("expected organization test-org, got %s", client.organization)
	}
	if client.project != "test-project" {
		t.Errorf("expected project test-project, got %s", client.project)
	}
	if client.repository != "test-repo" {
		t.Errorf("expected repository test-repo, got %s", client.repository)
	}
	if client.baseURL != "https://azure.example.com" {
		t.Errorf("expected baseURL https://azure.example.com, got %s", client.baseURL)
	}
}

func TestGetWorkItemWebURL(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "my-org", "my-project")

	url := client.GetWorkItemWebURL(123)
	expected := "https://dev.azure.com/my-org/my-project/_workitems/edit/123"

	if url != expected {
		t.Errorf("expected %s, got %s", expected, url)
	}
}

func TestGetPullRequestWebURL(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "my-org", "my-project")
	client.SetRepository("my-repo")

	url := client.GetPullRequestWebURL(42)
	expected := "https://dev.azure.com/my-org/my-project/_git/my-repo/pullrequest/42"

	if url != expected {
		t.Errorf("expected %s, got %s", expected, url)
	}
}
