package slack

import (
	"encoding/json"
	"testing"
)

// TestMessageStructure tests Message struct fields and JSON serialization
func TestMessageStructure(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
	}{
		{
			name: "text only",
			msg: Message{
				Channel: "#general",
				Text:    "Hello",
			},
		},
		{
			name: "with blocks",
			msg: Message{
				Channel: "#dev",
				Blocks: []Block{
					{
						Type: "section",
						Text: &TextObject{
							Type: "mrkdwn",
							Text: "*Bold*",
						},
					},
				},
			},
		},
		{
			name: "with attachments",
			msg: Message{
				Channel: "#alerts",
				Text:    "Alert",
				Attachments: []Attachment{
					{
						Color:  "danger",
						Title:  "Error",
						Text:   "Something went wrong",
						Footer: "Pilot Bot",
					},
				},
			},
		},
		{
			name: "with thread",
			msg: Message{
				Channel:  "#general",
				Text:     "Reply",
				ThreadTS: "1234567890.000000",
			},
		},
		{
			name: "full message",
			msg: Message{
				Channel: "#dev-notifications",
				Text:    "Full message",
				Blocks: []Block{
					{
						Type: "section",
						Text: &TextObject{
							Type: "mrkdwn",
							Text: "Main content",
						},
					},
					{
						Type: "context",
						Elements: []TextObject{
							{Type: "mrkdwn", Text: "Footer context"},
						},
					},
				},
				Attachments: []Attachment{
					{Color: "good"},
				},
				ThreadTS: "1234567890.000000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test JSON serialization
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			// Test JSON deserialization
			var decoded Message
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Verify fields
			if decoded.Channel != tt.msg.Channel {
				t.Errorf("Channel = %q, want %q", decoded.Channel, tt.msg.Channel)
			}
			if decoded.Text != tt.msg.Text {
				t.Errorf("Text = %q, want %q", decoded.Text, tt.msg.Text)
			}
			if len(decoded.Blocks) != len(tt.msg.Blocks) {
				t.Errorf("Blocks len = %d, want %d", len(decoded.Blocks), len(tt.msg.Blocks))
			}
			if len(decoded.Attachments) != len(tt.msg.Attachments) {
				t.Errorf("Attachments len = %d, want %d", len(decoded.Attachments), len(tt.msg.Attachments))
			}
			if decoded.ThreadTS != tt.msg.ThreadTS {
				t.Errorf("ThreadTS = %q, want %q", decoded.ThreadTS, tt.msg.ThreadTS)
			}
		})
	}
}

// TestBlockStructure tests Block struct fields
func TestBlockStructure(t *testing.T) {
	tests := []struct {
		name  string
		block Block
	}{
		{
			name: "section block",
			block: Block{
				Type: "section",
				Text: &TextObject{
					Type: "mrkdwn",
					Text: "*Bold* text",
				},
			},
		},
		{
			name: "context block",
			block: Block{
				Type: "context",
				Elements: []TextObject{
					{Type: "mrkdwn", Text: "Context 1"},
					{Type: "plain_text", Text: "Context 2"},
				},
			},
		},
		{
			name: "divider block",
			block: Block{
				Type: "divider",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.block)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded Block
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.Type != tt.block.Type {
				t.Errorf("Type = %q, want %q", decoded.Type, tt.block.Type)
			}
		})
	}
}

// TestAttachmentStructure tests Attachment struct fields
func TestAttachmentStructure(t *testing.T) {
	tests := []struct {
		name       string
		attachment Attachment
	}{
		{
			name: "success attachment",
			attachment: Attachment{
				Color: "good",
			},
		},
		{
			name: "warning attachment",
			attachment: Attachment{
				Color: "warning",
				Title: "Warning",
				Text:  "Something needs attention",
			},
		},
		{
			name: "danger attachment",
			attachment: Attachment{
				Color:  "danger",
				Title:  "Error",
				Text:   "Something went wrong",
				Footer: "Pilot Bot v1.0",
			},
		},
		{
			name: "custom color",
			attachment: Attachment{
				Color: "#6366f1",
				Title: "Info",
				Text:  "Custom colored attachment",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.attachment)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded Attachment
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.Color != tt.attachment.Color {
				t.Errorf("Color = %q, want %q", decoded.Color, tt.attachment.Color)
			}
			if decoded.Title != tt.attachment.Title {
				t.Errorf("Title = %q, want %q", decoded.Title, tt.attachment.Title)
			}
			if decoded.Text != tt.attachment.Text {
				t.Errorf("Text = %q, want %q", decoded.Text, tt.attachment.Text)
			}
			if decoded.Footer != tt.attachment.Footer {
				t.Errorf("Footer = %q, want %q", decoded.Footer, tt.attachment.Footer)
			}
		})
	}
}

// TestTextObjectStructure tests TextObject struct fields
func TestTextObjectStructure(t *testing.T) {
	tests := []struct {
		name string
		text TextObject
	}{
		{
			name: "markdown text",
			text: TextObject{
				Type: "mrkdwn",
				Text: "*Bold* and _italic_",
			},
		},
		{
			name: "plain text",
			text: TextObject{
				Type: "plain_text",
				Text: "Simple text",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.text)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded TextObject
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.Type != tt.text.Type {
				t.Errorf("Type = %q, want %q", decoded.Type, tt.text.Type)
			}
			if decoded.Text != tt.text.Text {
				t.Errorf("Text = %q, want %q", decoded.Text, tt.text.Text)
			}
		})
	}
}

// TestPostMessageResponseStructure tests PostMessageResponse struct fields
func TestPostMessageResponseStructure(t *testing.T) {
	tests := []struct {
		name     string
		response PostMessageResponse
	}{
		{
			name: "success response",
			response: PostMessageResponse{
				OK:      true,
				TS:      "1234567890.123456",
				Channel: "C1234567890",
			},
		},
		{
			name: "error response",
			response: PostMessageResponse{
				OK:       false,
				Error:    "channel_not_found",
				ErrorMsg: "Channel not found",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded PostMessageResponse
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.OK != tt.response.OK {
				t.Errorf("OK = %v, want %v", decoded.OK, tt.response.OK)
			}
			if decoded.TS != tt.response.TS {
				t.Errorf("TS = %q, want %q", decoded.TS, tt.response.TS)
			}
			if decoded.Channel != tt.response.Channel {
				t.Errorf("Channel = %q, want %q", decoded.Channel, tt.response.Channel)
			}
			if decoded.Error != tt.response.Error {
				t.Errorf("Error = %q, want %q", decoded.Error, tt.response.Error)
			}
		})
	}
}
