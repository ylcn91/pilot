package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewProjectBoardSync(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)

	tests := []struct {
		name    string
		config  *ProjectBoardConfig
		wantNil bool
	}{
		{
			name:    "nil config returns nil",
			config:  nil,
			wantNil: true,
		},
		{
			name:    "disabled config returns nil",
			config:  &ProjectBoardConfig{Enabled: false},
			wantNil: true,
		},
		{
			name: "enabled config returns instance",
			config: &ProjectBoardConfig{
				Enabled:       true,
				ProjectNumber: 1,
				StatusField:   "Status",
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewProjectBoardSync(client, tt.config, "testorg")
			if (result == nil) != tt.wantNil {
				t.Errorf("NewProjectBoardSync() nil = %v, wantNil %v", result == nil, tt.wantNil)
			}
		})
	}
}

func TestResolveProjectID_OrgFirstUserFallback(t *testing.T) {
	tests := []struct {
		name       string
		orgResp    string
		orgStatus  int
		userResp   string
		userStatus int
		wantID     string
		wantErr    bool
	}{
		{
			name:      "org found",
			orgResp:   `{"data":{"organization":{"projectV2":{"id":"PVT_org"}}}}`,
			orgStatus: http.StatusOK,
			wantID:    "PVT_org",
		},
		{
			name:       "org empty, user found",
			orgResp:    `{"data":{"organization":{"projectV2":{"id":""}}}}`,
			orgStatus:  http.StatusOK,
			userResp:   `{"data":{"user":{"projectV2":{"id":"PVT_user"}}}}`,
			userStatus: http.StatusOK,
			wantID:     "PVT_user",
		},
		{
			name:       "org error, user found",
			orgResp:    `{"data":null,"errors":[{"message":"not an org"}]}`,
			orgStatus:  http.StatusOK,
			userResp:   `{"data":{"user":{"projectV2":{"id":"PVT_user2"}}}}`,
			userStatus: http.StatusOK,
			wantID:     "PVT_user2",
		},
		{
			name:       "both fail",
			orgResp:    `{"data":null,"errors":[{"message":"not an org"}]}`,
			orgStatus:  http.StatusOK,
			userResp:   `{"data":null,"errors":[{"message":"not found"}]}`,
			userStatus: http.StatusOK,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var callNum int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req GraphQLRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode: %v", err)
				}

				callNum++
				if strings.Contains(req.Query, "organization") {
					w.WriteHeader(tt.orgStatus)
					_, _ = w.Write([]byte(tt.orgResp))
				} else if strings.Contains(req.Query, "user") {
					w.WriteHeader(tt.userStatus)
					_, _ = w.Write([]byte(tt.userResp))
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			pbs := &ProjectBoardSync{
				client: client,
				config: &ProjectBoardConfig{ProjectNumber: 3},
				owner:  "testowner",
			}

			id, err := pbs.resolveProjectID(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveProjectID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && id != tt.wantID {
				t.Errorf("resolveProjectID() = %q, want %q", id, tt.wantID)
			}
		})
	}
}

func TestEnsureResolved_ConcurrentAccess(t *testing.T) {
	var resolveCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}

		var resp string
		switch {
		case strings.Contains(req.Query, "organization"):
			resolveCount.Add(1)
			resp = `{"data":{"organization":{"projectV2":{"id":"PVT_conc"}}}}`
		case strings.Contains(req.Query, "field(name:"):
			resolveCount.Add(1)
			resp = `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_done","name":"Done"}]}}}}`
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := &ProjectBoardSync{
		client: client,
		config: &ProjectBoardConfig{ProjectNumber: 1, StatusField: "Status"},
		owner:  "testorg",
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pbs.ensureResolved(context.Background())
		}()
	}
	wg.Wait()

	// Should resolve at most 2 times (project + field), not 20.
	if resolveCount.Load() > 2 {
		t.Errorf("expected at most 2 resolve calls (cached), got %d", resolveCount.Load())
	}
}

func TestResolveFieldAndOptions_DefaultFieldName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}

		// Verify the default field name "Status" is used.
		if req.Variables["fieldName"] != "Status" {
			t.Errorf("expected fieldName=Status, got %v", req.Variables["fieldName"])
		}

		resp := `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_1","name":"Todo"}]}}}}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := &ProjectBoardSync{
		client:    client,
		config:    &ProjectBoardConfig{StatusField: ""}, // empty — should default to "Status"
		owner:     "testorg",
		projectID: "PVT_test",
	}

	fieldID, opts, err := pbs.resolveFieldAndOptions(context.Background())
	if err != nil {
		t.Fatalf("resolveFieldAndOptions() error = %v", err)
	}
	if fieldID != "PVTSSF_f1" {
		t.Errorf("fieldID = %q, want PVTSSF_f1", fieldID)
	}
	if opts["todo"] != "OPT_1" {
		t.Errorf("expected option todo=OPT_1, got %v", opts)
	}
}
