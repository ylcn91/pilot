package telegram

import "fmt"

// SendMessageRequest represents a Telegram sendMessage request
type SendMessageRequest struct {
	ChatID      string                `json:"chat_id"`
	Text        string                `json:"text"`
	ParseMode   string                `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

// SendMessageResponse represents the response from sending a message
type SendMessageResponse struct {
	OK          bool    `json:"ok"`
	Result      *Result `json:"result,omitempty"`
	Description string  `json:"description,omitempty"`
	ErrorCode   int     `json:"error_code,omitempty"`
}

// Result represents the message result
type Result struct {
	MessageID int64 `json:"message_id"`
	ChatID    int64 `json:"chat_id,omitempty"`
}

// Update represents a Telegram update from getUpdates
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// CallbackQuery represents a callback query from inline keyboard
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

// InlineKeyboardMarkup represents an inline keyboard
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton represents a button in an inline keyboard
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// Message represents a Telegram message
type Message struct {
	MessageID int64        `json:"message_id"`
	From      *User        `json:"from,omitempty"`
	Chat      *Chat        `json:"chat"`
	Date      int64        `json:"date"`
	Text      string       `json:"text,omitempty"`
	Photo     []*PhotoSize `json:"photo,omitempty"`
	Voice     *Voice       `json:"voice,omitempty"`
	Caption   string       `json:"caption,omitempty"`
}

// Voice represents a voice message
type Voice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int    `json:"file_size,omitempty"`
}

// PhotoSize represents one size of a photo or file thumbnail
type PhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int    `json:"file_size,omitempty"`
}

// File represents a file ready to be downloaded
type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path,omitempty"`
	FileSize int    `json:"file_size,omitempty"`
}

// GetFileResponse represents the response from getFile API
type GetFileResponse struct {
	OK          bool   `json:"ok"`
	Result      *File  `json:"result,omitempty"`
	Description string `json:"description,omitempty"`
	ErrorCode   int    `json:"error_code,omitempty"`
}

// User represents a Telegram user
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// Chat represents a Telegram chat
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// GetUpdatesResponse represents the response from getUpdates
type GetUpdatesResponse struct {
	OK          bool      `json:"ok"`
	Result      []*Update `json:"result,omitempty"`
	Description string    `json:"description,omitempty"`
	ErrorCode   int       `json:"error_code,omitempty"`
}

// ErrConflict is returned when another bot instance is already running
var ErrConflict = fmt.Errorf("another bot instance is running")

// GetMeResponse represents the response from getMe
type GetMeResponse struct {
	OK          bool   `json:"ok"`
	Result      *User  `json:"result,omitempty"`
	Description string `json:"description,omitempty"`
	ErrorCode   int    `json:"error_code,omitempty"`
}

// BriefMessageResponse is a simplified response for brief delivery
type BriefMessageResponse struct {
	MessageID int64
}
