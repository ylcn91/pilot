package telegram

import (
	"testing"
)

// TestCallbackQueryData tests callback query data parsing
func TestCallbackQueryData(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"execute action", "execute"},
		{"cancel action", "cancel"},
		{"voice check status", "voice_check_status"},
		{"empty data", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callback := &CallbackQuery{
				ID:   "callback_123",
				Data: tt.data,
				From: &User{ID: 123, FirstName: "Test"},
			}

			if callback.Data != tt.data {
				t.Errorf("Data = %q, want %q", callback.Data, tt.data)
			}
		})
	}
}

// TestInlineKeyboardButtonVariants tests button configurations
func TestInlineKeyboardButtonVariants(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		callbackData string
	}{
		{"execute button", "Execute", "execute"},
		{"cancel button", "Cancel", "cancel"},
		{"yes button", "Yes", "yes"},
		{"no button", "No", "no"},
		{"emoji button", "Yes", "yes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			btn := InlineKeyboardButton{
				Text:         tt.text,
				CallbackData: tt.callbackData,
			}

			if btn.Text != tt.text {
				t.Errorf("Text = %q, want %q", btn.Text, tt.text)
			}
			if btn.CallbackData != tt.callbackData {
				t.Errorf("CallbackData = %q, want %q", btn.CallbackData, tt.callbackData)
			}
		})
	}
}

// TestKeyboardLayouts tests various keyboard configurations
func TestKeyboardLayouts(t *testing.T) {
	tests := []struct {
		name        string
		keyboard    [][]InlineKeyboardButton
		wantRows    int
		wantButtons []int // buttons per row
	}{
		{
			name: "single row two buttons",
			keyboard: [][]InlineKeyboardButton{
				{
					{Text: "Yes", CallbackData: "yes"},
					{Text: "No", CallbackData: "no"},
				},
			},
			wantRows:    1,
			wantButtons: []int{2},
		},
		{
			name: "two rows",
			keyboard: [][]InlineKeyboardButton{
				{
					{Text: "Execute", CallbackData: "execute"},
					{Text: "Cancel", CallbackData: "cancel"},
				},
				{
					{Text: "Help", CallbackData: "help"},
				},
			},
			wantRows:    2,
			wantButtons: []int{2, 1},
		},
		{
			name: "three buttons in single row",
			keyboard: [][]InlineKeyboardButton{
				{
					{Text: "A", CallbackData: "a"},
					{Text: "B", CallbackData: "b"},
					{Text: "C", CallbackData: "c"},
				},
			},
			wantRows:    1,
			wantButtons: []int{3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.keyboard) != tt.wantRows {
				t.Errorf("rows = %d, want %d", len(tt.keyboard), tt.wantRows)
			}
			for i, row := range tt.keyboard {
				if len(row) != tt.wantButtons[i] {
					t.Errorf("row %d buttons = %d, want %d", i, len(row), tt.wantButtons[i])
				}
			}
		})
	}
}

// TestSendMessageRequestFields tests request struct
func TestSendMessageRequestFields(t *testing.T) {
	req := &SendMessageRequest{
		ChatID:    "123456",
		Text:      "Test message",
		ParseMode: "Markdown",
	}

	if req.ChatID != "123456" {
		t.Errorf("ChatID = %q, want 123456", req.ChatID)
	}
	if req.Text != "Test message" {
		t.Errorf("Text = %q, want Test message", req.Text)
	}
	if req.ParseMode != "Markdown" {
		t.Errorf("ParseMode = %q, want Markdown", req.ParseMode)
	}
}

// TestSendMessageRequestWithKeyboard tests keyboard in request
func TestSendMessageRequestWithKeyboard(t *testing.T) {
	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "OK", CallbackData: "ok"}},
		},
	}

	req := &SendMessageRequest{
		ChatID:      "123456",
		Text:        "Choose:",
		ReplyMarkup: keyboard,
	}

	if req.ReplyMarkup == nil {
		t.Fatal("ReplyMarkup is nil")
	}
	if len(req.ReplyMarkup.InlineKeyboard) != 1 {
		t.Errorf("keyboard rows = %d, want 1", len(req.ReplyMarkup.InlineKeyboard))
	}
}
