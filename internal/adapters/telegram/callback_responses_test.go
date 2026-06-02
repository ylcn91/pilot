package telegram

import (
	"testing"
)

// TestSendMessageResponseFields tests response struct
func TestSendMessageResponseFields(t *testing.T) {
	tests := []struct {
		name     string
		response SendMessageResponse
		wantOK   bool
	}{
		{
			name: "success response",
			response: SendMessageResponse{
				OK:     true,
				Result: &Result{MessageID: 100},
			},
			wantOK: true,
		},
		{
			name: "error response",
			response: SendMessageResponse{
				OK:          false,
				Description: "Bad Request",
				ErrorCode:   400,
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.response.OK != tt.wantOK {
				t.Errorf("OK = %v, want %v", tt.response.OK, tt.wantOK)
			}
		})
	}
}

// TestResultFields tests result struct
func TestResultFields(t *testing.T) {
	result := &Result{
		MessageID: 12345,
		ChatID:    67890,
	}

	if result.MessageID != 12345 {
		t.Errorf("MessageID = %d, want 12345", result.MessageID)
	}
	if result.ChatID != 67890 {
		t.Errorf("ChatID = %d, want 67890", result.ChatID)
	}
}

// TestGetUpdatesResponseFields tests updates response struct
func TestGetUpdatesResponseFields(t *testing.T) {
	tests := []struct {
		name        string
		response    GetUpdatesResponse
		wantOK      bool
		wantUpdates int
	}{
		{
			name: "empty updates",
			response: GetUpdatesResponse{
				OK:     true,
				Result: []*Update{},
			},
			wantOK:      true,
			wantUpdates: 0,
		},
		{
			name: "with updates",
			response: GetUpdatesResponse{
				OK: true,
				Result: []*Update{
					{UpdateID: 1},
					{UpdateID: 2},
				},
			},
			wantOK:      true,
			wantUpdates: 2,
		},
		{
			name: "error response",
			response: GetUpdatesResponse{
				OK:          false,
				ErrorCode:   401,
				Description: "Unauthorized",
			},
			wantOK:      false,
			wantUpdates: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.response.OK != tt.wantOK {
				t.Errorf("OK = %v, want %v", tt.response.OK, tt.wantOK)
			}
			if len(tt.response.Result) != tt.wantUpdates {
				t.Errorf("updates = %d, want %d", len(tt.response.Result), tt.wantUpdates)
			}
		})
	}
}

// TestGetFileResponseFields tests file response struct
func TestGetFileResponseFields(t *testing.T) {
	tests := []struct {
		name     string
		response GetFileResponse
		wantOK   bool
	}{
		{
			name: "success with file",
			response: GetFileResponse{
				OK: true,
				Result: &File{
					FileID:   "file123",
					FilePath: "photos/photo.jpg",
					FileSize: 10240,
				},
			},
			wantOK: true,
		},
		{
			name: "file not found",
			response: GetFileResponse{
				OK:          false,
				ErrorCode:   400,
				Description: "Bad Request: file not found",
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.response.OK != tt.wantOK {
				t.Errorf("OK = %v, want %v", tt.response.OK, tt.wantOK)
			}
		})
	}
}

// TestFileFields tests file struct
func TestFileFields(t *testing.T) {
	file := &File{
		FileID:   "file_abc123",
		FilePath: "voice/audio.oga",
		FileSize: 5000,
	}

	if file.FileID != "file_abc123" {
		t.Errorf("FileID = %q, want file_abc123", file.FileID)
	}
	if file.FilePath != "voice/audio.oga" {
		t.Errorf("FilePath = %q, want voice/audio.oga", file.FilePath)
	}
	if file.FileSize != 5000 {
		t.Errorf("FileSize = %d, want 5000", file.FileSize)
	}
}

// TestUserFields tests user struct
func TestUserFields(t *testing.T) {
	tests := []struct {
		name     string
		user     *User
		wantName string
	}{
		{
			name: "full user",
			user: &User{
				ID:        12345,
				FirstName: "John",
				LastName:  "Doe",
				Username:  "johndoe",
			},
			wantName: "John",
		},
		{
			name: "minimal user",
			user: &User{
				ID:        67890,
				FirstName: "Jane",
			},
			wantName: "Jane",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.user.FirstName != tt.wantName {
				t.Errorf("FirstName = %q, want %q", tt.user.FirstName, tt.wantName)
			}
		})
	}
}

// TestChatFields tests chat struct
func TestChatFields(t *testing.T) {
	tests := []struct {
		name     string
		chat     *Chat
		wantType string
	}{
		{
			name:     "private chat",
			chat:     &Chat{ID: 123, Type: "private"},
			wantType: "private",
		},
		{
			name:     "group chat",
			chat:     &Chat{ID: 456, Type: "group"},
			wantType: "group",
		},
		{
			name:     "supergroup",
			chat:     &Chat{ID: 789, Type: "supergroup"},
			wantType: "supergroup",
		},
		{
			name:     "channel",
			chat:     &Chat{ID: 999, Type: "channel"},
			wantType: "channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.chat.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", tt.chat.Type, tt.wantType)
			}
		})
	}
}
