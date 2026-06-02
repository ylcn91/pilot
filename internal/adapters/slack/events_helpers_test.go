package slack

import (
	"encoding/json"
	"testing"
)

func TestStripBotMention(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single mention at start",
			input: "<@UBOT123> deploy staging",
			want:  "deploy staging",
		},
		{
			name:  "multiple mentions",
			input: "<@UBOT123> <@UOTHER456> check this",
			want:  "check this",
		},
		{
			name:  "mention in middle",
			input: "hey <@UBOT123> do something",
			want:  "hey do something",
		},
		{
			name:  "no mention",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "only mention",
			input: "<@UBOT123>",
			want:  "",
		},
		{
			name:  "mention with no space after",
			input: "<@UBOT123>deploy",
			want:  "deploy",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripBotMention(tt.input)
			if got != tt.want {
				t.Errorf("stripBotMention(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSocketEventIsBotMessage(t *testing.T) {
	tests := []struct {
		name  string
		event SocketEvent
		want  bool
	}{
		{
			name:  "bot message",
			event: SocketEvent{BotID: "B999"},
			want:  true,
		},
		{
			name:  "user message",
			event: SocketEvent{BotID: ""},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.IsBotMessage(); got != tt.want {
				t.Errorf("IsBotMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSocketEventIsFromBot(t *testing.T) {
	tests := []struct {
		name  string
		event SocketEvent
		botID string
		want  bool
	}{
		{
			name:  "matches own bot ID",
			event: SocketEvent{BotID: "B999"},
			botID: "B999",
			want:  true,
		},
		{
			name:  "different bot ID",
			event: SocketEvent{BotID: "B999"},
			botID: "B111",
			want:  false,
		},
		{
			name:  "no bot ID on event",
			event: SocketEvent{BotID: ""},
			botID: "B999",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.IsFromBot(tt.botID); got != tt.want {
				t.Errorf("IsFromBot(%q) = %v, want %v", tt.botID, got, tt.want)
			}
		})
	}
}

func TestSlackFileJSONRoundTrip(t *testing.T) {
	original := SlackFile{
		ID:       "F001",
		Name:     "report.pdf",
		Mimetype: "application/pdf",
		URL:      "https://files.slack.com/files-pri/T123-F001/report.pdf",
		Size:     204800,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SlackFile
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded != original {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", decoded, original)
	}
}

// contains checks if s contains substr (helper to avoid strings import in tests).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
