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

// TestGetUserInfoCaching tests that user info is cached correctly
func TestGetUserInfoCaching(t *testing.T) {
	// Clear cache
	globalUserCache = &userCache{}

	callCount := 0
	transport := &mockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			callCount++
			response := usersInfoResponse{
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
						DisplayName: "cached.user",
						RealName:    "Cached User",
						Email:       "cached@example.com",
					},
				},
			}
			respBody, _ := json.Marshal(response)
			return &http.Response{
				StatusCode: http.StatusOK,
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

	// First call should hit the API
	result1, err := client.GetUserInfo(ctx, "U1234567890")
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("call count = %d, want 1 after first call", callCount)
	}

	// Second call should be cached
	result2, err := client.GetUserInfo(ctx, "U1234567890")
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("call count = %d, want 1 after second call (should be cached)", callCount)
	}

	// Verify both results are the same
	if result1.ID != result2.ID {
		t.Errorf("cached result differs: %q vs %q", result1.ID, result2.ID)
	}
	if result1.DisplayName != result2.DisplayName {
		t.Errorf("cached result differs: %q vs %q", result1.DisplayName, result2.DisplayName)
	}

	// Different user should hit the API
	_, err = client.GetUserInfo(ctx, "U9999999999")
	if err != nil {
		t.Fatalf("third call error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("call count = %d, want 2 after different user call", callCount)
	}
}

// TestGetUserInfoNetworkError tests network-level errors
func TestGetUserInfoNetworkError(t *testing.T) {
	// Clear cache
	globalUserCache = &userCache{}

	transport := &mockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			return nil, &mockNetworkError{msg: "connection refused"}
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
	_, err := client.GetUserInfo(ctx, "U1234567890")

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get user info") {
		t.Errorf("error = %q, want to contain 'failed to get user info'", err.Error())
	}
}

// mockNetworkError is a custom error for network failure testing
type mockNetworkError struct {
	msg string
}

func (e *mockNetworkError) Error() string {
	return e.msg
}

// TestGetUserInfoInvalidJSON tests handling of invalid JSON response
func TestGetUserInfoInvalidJSON(t *testing.T) {
	// Clear cache
	globalUserCache = &userCache{}

	transport := &mockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not valid json")),
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
	_, err := client.GetUserInfo(ctx, "U1234567890")

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("error = %q, want to contain 'failed to parse response'", err.Error())
	}
}

// TestUserCacheExpiry tests that cache entries expire
func TestUserCacheExpiry(t *testing.T) {
	cache := &userCache{}

	user := &UserInfo{
		ID:          "U123",
		DisplayName: "test",
		Email:       "test@test.com",
		IsBot:       false,
	}

	// Set with expired timestamp
	cache.entries.Store("U123", &userCacheEntry{
		user:      user,
		expiresAt: time.Now().Add(-1 * time.Minute), // expired 1 minute ago
	})

	// Get should return false for expired entry
	result, ok := cache.get("U123")
	if ok {
		t.Error("expected cache miss for expired entry")
	}
	if result != nil {
		t.Error("expected nil result for expired entry")
	}
}

// TestUserCacheValid tests that valid cache entries are returned
func TestUserCacheValid(t *testing.T) {
	cache := &userCache{}

	user := &UserInfo{
		ID:          "U456",
		DisplayName: "valid",
		Email:       "valid@test.com",
		IsBot:       false,
	}

	// Set with future expiration
	cache.entries.Store("U456", &userCacheEntry{
		user:      user,
		expiresAt: time.Now().Add(30 * time.Minute),
	})

	// Get should return the cached entry
	result, ok := cache.get("U456")
	if !ok {
		t.Error("expected cache hit for valid entry")
	}
	if result == nil {
		t.Fatal("expected non-nil result for valid entry")
	}
	if result.ID != user.ID {
		t.Errorf("ID = %q, want %q", result.ID, user.ID)
	}
}

// TestUserCacheMiss tests that cache miss returns nil
func TestUserCacheMiss(t *testing.T) {
	cache := &userCache{}

	result, ok := cache.get("UNOTEXIST")
	if ok {
		t.Error("expected cache miss for non-existent entry")
	}
	if result != nil {
		t.Error("expected nil result for non-existent entry")
	}
}
