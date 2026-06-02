package telegram

import (
	"testing"
)

// TestUpdateStructure tests Update struct fields
func TestUpdateStructure(t *testing.T) {
	// Test that Update can hold both message and callback
	update := &Update{
		UpdateID: 12345,
		Message: &Message{
			MessageID: 100,
			Chat:      &Chat{ID: 999, Type: "private"},
			Text:      "Hello",
		},
	}

	if update.UpdateID != 12345 {
		t.Errorf("UpdateID = %d, want 12345", update.UpdateID)
	}
	if update.Message == nil {
		t.Error("Message is nil")
	}
	if update.CallbackQuery != nil {
		t.Error("CallbackQuery should be nil")
	}

	// Test with callback
	update2 := &Update{
		UpdateID: 12346,
		CallbackQuery: &CallbackQuery{
			ID:   "callback123",
			Data: "execute",
		},
	}

	if update2.CallbackQuery == nil {
		t.Error("CallbackQuery is nil")
	}
	if update2.Message != nil {
		t.Error("Message should be nil")
	}
}

// TestMessageStructure tests Message struct fields
func TestMessageStructure(t *testing.T) {
	msg := &Message{
		MessageID: 100,
		From: &User{
			ID:        123,
			FirstName: "John",
			LastName:  "Doe",
			Username:  "johndoe",
		},
		Chat: &Chat{
			ID:   456,
			Type: "private",
		},
		Date:    1234567890,
		Text:    "Hello, world!",
		Caption: "Photo caption",
	}

	if msg.MessageID != 100 {
		t.Errorf("MessageID = %d, want 100", msg.MessageID)
	}
	if msg.From.FirstName != "John" {
		t.Errorf("From.FirstName = %q, want John", msg.From.FirstName)
	}
	if msg.Chat.Type != "private" {
		t.Errorf("Chat.Type = %q, want private", msg.Chat.Type)
	}
}

// TestPhotoAndVoiceStructures tests media message structures
func TestPhotoAndVoiceStructures(t *testing.T) {
	// Photo message
	photoMsg := &Message{
		MessageID: 101,
		Photo: []*PhotoSize{
			{FileID: "small", Width: 100, Height: 100, FileSize: 1000},
			{FileID: "medium", Width: 320, Height: 320, FileSize: 5000},
			{FileID: "large", Width: 800, Height: 800, FileSize: 20000},
		},
		Caption: "My photo",
	}

	if len(photoMsg.Photo) != 3 {
		t.Errorf("Photo len = %d, want 3", len(photoMsg.Photo))
	}
	if photoMsg.Photo[2].FileID != "large" {
		t.Errorf("largest photo FileID = %q, want large", photoMsg.Photo[2].FileID)
	}

	// Voice message
	voiceMsg := &Message{
		MessageID: 102,
		Voice: &Voice{
			FileID:   "voice123",
			Duration: 15,
			MimeType: "audio/ogg",
			FileSize: 12000,
		},
	}

	if voiceMsg.Voice.Duration != 15 {
		t.Errorf("Voice.Duration = %d, want 15", voiceMsg.Voice.Duration)
	}
}

// TestInlineKeyboardMarkup tests keyboard structure
func TestInlineKeyboardMarkup(t *testing.T) {
	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Yes", CallbackData: "yes"},
				{Text: "No", CallbackData: "no"},
			},
		},
	}

	if len(keyboard.InlineKeyboard) != 1 {
		t.Errorf("rows = %d, want 1", len(keyboard.InlineKeyboard))
	}
	if len(keyboard.InlineKeyboard[0]) != 2 {
		t.Errorf("buttons in row = %d, want 2", len(keyboard.InlineKeyboard[0]))
	}
	if keyboard.InlineKeyboard[0][0].Text != "Yes" {
		t.Errorf("first button text = %q, want Yes", keyboard.InlineKeyboard[0][0].Text)
	}
}

// TestCallbackQueryStructure tests CallbackQuery fields
func TestCallbackQueryStructure(t *testing.T) {
	callback := &CallbackQuery{
		ID: "callback123",
		From: &User{
			ID:        789,
			FirstName: "Jane",
		},
		Message: &Message{
			MessageID: 200,
			Chat:      &Chat{ID: 456},
		},
		Data: "execute",
	}

	if callback.ID != "callback123" {
		t.Errorf("ID = %q, want callback123", callback.ID)
	}
	if callback.Data != "execute" {
		t.Errorf("Data = %q, want execute", callback.Data)
	}
	if callback.Message.MessageID != 200 {
		t.Errorf("Message.MessageID = %d, want 200", callback.Message.MessageID)
	}
}
