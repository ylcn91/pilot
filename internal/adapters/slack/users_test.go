package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// TestGetUserInfo tests the GetUserInfo method
func TestGetUserInfo(t *testing.T) {
	// Clear cache before tests
	globalUserCache = &userCache{}

	tests := []struct {
		name       string
		userID     string
		response   usersInfoResponse
		httpStatus int
		wantErr    bool
		errContain string
		want       *UserInfo
	}{
		{
			name:   "successful user info",
			userID: "U1234567890",
			response: usersInfoResponse{
				OK: true,
				User: struct {
					ID      string `json:"id"`
					IsBot   bool   `json:"is_bot"`
					Profile struct {
						DisplayName string `json:"display_name"`
						RealName    string `json:"real_name"`
						Email       string `json:"email"`
					} `json:"profile"`
				}{
					ID:    "U1234567890",
					IsBot: false,
					Profile: struct {
						DisplayName string `json:"display_name"`
						RealName    string `json:"real_name"`
						Email       string `json:"email"`
					}{
						DisplayName: "john.doe",
						RealName:    "John Doe",
						Email:       "john@example.com",
					},
				},
			},
			httpStatus: http.StatusOK,
			wantErr:    false,
			want: &UserInfo{
				ID:          "U1234567890",
				DisplayName: "john.doe",
				Email:       "john@example.com",
				IsBot:       false,
			},
		},
		{
			name:   "fallback to real name when display name empty",
			userID: "U9876543210",
			response: usersInfoResponse{
				OK: true,
				User: struct {
					ID      string `json:"id"`
					IsBot   bool   `json:"is_bot"`
					Profile struct {
						DisplayName string `json:"display_name"`
						RealName    string `json:"real_name"`
						Email       string `json:"email"`
					} `json:"profile"`
				}{
					ID:    "U9876543210",
					IsBot: false,
					Profile: struct {
						DisplayName string `json:"display_name"`
						RealName    string `json:"real_name"`
						Email       string `json:"email"`
					}{
						DisplayName: "",
						RealName:    "Jane Smith",
						Email:       "jane@example.com",
					},
				},
			},
			httpStatus: http.StatusOK,
			wantErr:    false,
			want: &UserInfo{
				ID:          "U9876543210",
				DisplayName: "Jane Smith",
				Email:       "jane@example.com",
				IsBot:       false,
			},
		},
		{
			name:   "bot user",
			userID: "UBOT123",
			response: usersInfoResponse{
				OK: true,
				User: struct {
					ID      string `json:"id"`
					IsBot   bool   `json:"is_bot"`
					Profile struct {
						DisplayName string `json:"display_name"`
						RealName    string `json:"real_name"`
						Email       string `json:"email"`
					} `json:"profile"`
				}{
					ID:    "UBOT123",
					IsBot: true,
					Profile: struct {
						DisplayName string `json:"display_name"`
						RealName    string `json:"real_name"`
						Email       string `json:"email"`
					}{
						DisplayName: "Bot User",
						RealName:    "Bot User",
						Email:       "",
					},
				},
			},
			httpStatus: http.StatusOK,
			wantErr:    false,
			want: &UserInfo{
				ID:          "UBOT123",
				DisplayName: "Bot User",
				Email:       "",
				IsBot:       true,
			},
		},
		{
			name:   "user not found",
			userID: "UNOTFOUND",
			response: usersInfoResponse{
				OK:    false,
				Error: "user_not_found",
			},
			httpStatus: http.StatusOK,
			wantErr:    true,
			errContain: "user_not_found",
		},
		{
			name:   "invalid auth",
			userID: "U1234567890",
			response: usersInfoResponse{
				OK:    false,
				Error: "invalid_auth",
			},
			httpStatus: http.StatusOK,
			wantErr:    true,
			errContain: "invalid_auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear cache for each test
			globalUserCache = &userCache{}

			transport := &mockTransport{
				handler: func(req *http.Request) (*http.Response, error) {
					// Verify method
					if req.Method != http.MethodGet {
						t.Errorf("method = %q, want GET", req.Method)
					}

					// Verify path contains users.info
					if !strings.Contains(req.URL.Path, "/users.info") {
						t.Errorf("path = %q, want to contain /users.info", req.URL.Path)
					}

					// Verify user parameter
					if !strings.Contains(req.URL.RawQuery, "user="+tt.userID) {
						t.Errorf("query = %q, want to contain user=%s", req.URL.RawQuery, tt.userID)
					}

					// Verify authorization
					auth := req.Header.Get("Authorization")
					if !strings.HasPrefix(auth, "Bearer ") {
						t.Errorf("Authorization = %q, want Bearer prefix", auth)
					}

					// Create response
					respBody, _ := json.Marshal(tt.response)
					return &http.Response{
						StatusCode: tt.httpStatus,
						Body:       io.NopCloser(strings.NewReader(string(respBody))),
						Header:     make(http.Header),
					}, nil
				},
			}

			client := &Client{
				botToken: testutil.FakeSlackBotToken,
				httpClient: &http.Client{
					Transport: transport,
					Timeout:   30 * time.Second,
				},
			}

			ctx := context.Background()
			result, err := client.GetUserInfo(ctx, tt.userID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContain)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Error("result is nil")
				} else {
					if result.ID != tt.want.ID {
						t.Errorf("ID = %q, want %q", result.ID, tt.want.ID)
					}
					if result.DisplayName != tt.want.DisplayName {
						t.Errorf("DisplayName = %q, want %q", result.DisplayName, tt.want.DisplayName)
					}
					if result.Email != tt.want.Email {
						t.Errorf("Email = %q, want %q", result.Email, tt.want.Email)
					}
					if result.IsBot != tt.want.IsBot {
						t.Errorf("IsBot = %v, want %v", result.IsBot, tt.want.IsBot)
					}
				}
			}
		})
	}
}
