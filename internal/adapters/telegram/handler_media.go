package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/transcription"
)

// handlePhoto processes photo messages
func (h *Handler) handlePhoto(ctx context.Context, chatID string, msg *Message) {
	// Security check: only process from allowed users/chats
	if len(h.allowedIDs) > 0 {
		senderID := int64(0)
		if msg.From != nil {
			senderID = msg.From.ID
		}
		if !h.allowedIDs[msg.Chat.ID] && !h.allowedIDs[senderID] {
			logging.WithComponent("telegram").Debug("Ignoring photo from unauthorized chat/user",
				slog.Int64("chat_id", msg.Chat.ID), slog.Int64("sender_id", senderID))
			return
		}
	}

	// Get the largest photo size (last in array)
	photo := msg.Photo[len(msg.Photo)-1]
	logging.WithComponent("telegram").Debug("Received photo",
		slog.String("chat_id", chatID), slog.Int("width", photo.Width), slog.Int("height", photo.Height))

	// Send acknowledgment
	_, _ = h.client.SendMessage(ctx, chatID, "📷 Processing image...", "")

	// Download the image
	imagePath, err := h.downloadImage(ctx, photo.FileID)
	if err != nil {
		logging.WithComponent("telegram").Warn("Failed to download image", slog.Any("error", err))
		_, _ = h.client.SendMessage(ctx, chatID, "❌ Failed to download image. Please try again.", "")
		return
	}
	defer func() {
		// Cleanup temp file after processing
		_ = os.Remove(imagePath)
	}()

	// Build prompt with image context
	prompt := msg.Caption
	if prompt == "" {
		prompt = "Analyze this image and describe what you see."
	}

	// Execute with image via commsHandler
	taskID := fmt.Sprintf("IMG-%d", time.Now().Unix())
	if h.commsHandler != nil {
		h.commsHandler.ExecuteDirectTask(ctx, chatID, "", taskID, prompt, &comms.DirectTaskOpts{
			ImagePath: imagePath,
		})
	}
}

// handleVoice processes voice messages
func (h *Handler) handleVoice(ctx context.Context, chatID string, msg *Message) {
	// Security check: only process from allowed users/chats
	if len(h.allowedIDs) > 0 {
		senderID := int64(0)
		if msg.From != nil {
			senderID = msg.From.ID
		}
		if !h.allowedIDs[msg.Chat.ID] && !h.allowedIDs[senderID] {
			logging.WithComponent("telegram").Debug("Ignoring voice from unauthorized chat/user",
				slog.Int64("chat_id", msg.Chat.ID), slog.Int64("sender_id", senderID))
			return
		}
	}

	// Check if transcription is available
	if h.transcriber == nil {
		logging.WithComponent("telegram").Debug("Voice message received but transcription not configured")
		msg := h.voiceNotAvailableMessage()
		_, _ = h.client.SendMessage(ctx, chatID, msg, "")
		return
	}

	voice := msg.Voice
	logging.WithComponent("telegram").Debug("Received voice",
		slog.String("chat_id", chatID), slog.Int("duration", voice.Duration))

	// Send acknowledgment
	_, _ = h.client.SendMessage(ctx, chatID, "🎤 Transcribing voice message...", "")

	// Download the voice file
	audioPath, err := h.downloadAudio(ctx, voice.FileID)
	if err != nil {
		logging.WithComponent("telegram").Warn("Failed to download voice", slog.Any("error", err))
		_, _ = h.client.SendMessage(ctx, chatID, "❌ Failed to download voice message. Please try again.", "")
		return
	}
	defer func() {
		_ = os.Remove(audioPath)
	}()

	// Transcribe the audio
	transcribeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, err := h.transcriber.Transcribe(transcribeCtx, audioPath)
	if err != nil {
		logging.WithComponent("telegram").Warn("Transcription failed", slog.Any("error", err))
		_, _ = h.client.SendMessage(ctx, chatID,
			"❌ Failed to transcribe voice message. Please try again or send as text.", "")
		return
	}

	if result.Text == "" {
		_, _ = h.client.SendMessage(ctx, chatID,
			"🤷 Couldn't understand the voice message. Please try again or send as text.", "")
		return
	}

	// Show the transcription to the user
	langInfo := ""
	if result.Language != "" && result.Language != "unknown" {
		langInfo = fmt.Sprintf(" (%s)", result.Language)
	}

	transcriptMsg := fmt.Sprintf("🎤 Transcribed%s:\n%s", langInfo, result.Text)
	_, _ = h.client.SendMessage(ctx, chatID, transcriptMsg, "")

	// Delegate transcribed text to commsHandler
	text := strings.TrimSpace(result.Text)
	logging.WithComponent("telegram").Debug("Processing transcribed text", slog.String("chat_id", chatID))

	if h.commsHandler != nil {
		senderID := ""
		if msg.From != nil {
			senderID = strconv.FormatInt(msg.From.ID, 10)
		}
		h.commsHandler.HandleMessage(ctx, &comms.IncomingMessage{
			ContextID: chatID,
			SenderID:  senderID,
			Text:      text,
			VoiceText: text,
			Platform:  "telegram",
			Timestamp: time.Now(),
		})
	}
}

// downloadAudio downloads a voice file from Telegram and saves to temp file
func (h *Handler) downloadAudio(ctx context.Context, fileID string) (string, error) {
	// Get file path from Telegram
	file, err := h.client.GetFile(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("getFile failed: %w", err)
	}

	if file.FilePath == "" {
		return "", fmt.Errorf("file path not available")
	}

	// Download file data
	data, err := h.client.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	// Determine extension from file path (usually .oga for voice)
	ext := filepath.Ext(file.FilePath)
	if ext == "" {
		ext = ".oga" // Default to oga for voice messages
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "pilot-voice-*"+ext)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = tmpFile.Close() }()

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	return tmpFile.Name(), nil
}

// voiceNotAvailableMessage returns an actionable error message for voice transcription
func (h *Handler) voiceNotAvailableMessage() string {
	var sb strings.Builder
	sb.WriteString("❌ Voice transcription not available\n\n")

	// Check what's missing based on the error
	if h.transcriptionErr != nil {
		errStr := h.transcriptionErr.Error()
		if strings.Contains(errStr, "no backend") || strings.Contains(errStr, "API key") {
			sb.WriteString("Missing: OpenAI API key\n\n")
			sb.WriteString("Set openai_api_key in ~/.pilot/config.yaml\n")
			sb.WriteString("Then restart bot.")
			return sb.String()
		}
	}

	// Generic guidance
	sb.WriteString("To enable voice:\n")
	sb.WriteString("1. Set openai_api_key in ~/.pilot/config.yaml\n")
	sb.WriteString("2. Restart bot\n\n")
	sb.WriteString("Run 'pilot doctor' to check setup.")
	return sb.String()
}

// downloadImage downloads an image from Telegram and saves to temp file
func (h *Handler) downloadImage(ctx context.Context, fileID string) (string, error) {
	// Get file path from Telegram
	file, err := h.client.GetFile(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("getFile failed: %w", err)
	}

	if file.FilePath == "" {
		return "", fmt.Errorf("file path not available")
	}

	// Download file data
	data, err := h.client.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	// Determine extension from file path
	ext := filepath.Ext(file.FilePath)
	if ext == "" {
		ext = ".jpg" // Default to jpg for photos
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "pilot-image-*"+ext)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = tmpFile.Close() }()

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	return tmpFile.Name(), nil
}

// sendVoiceSetupPrompt sends an interactive voice setup message with install options
func (h *Handler) sendVoiceSetupPrompt(ctx context.Context, chatID string) {
	status := transcription.CheckSetup(nil)

	var sb strings.Builder

	if status.OpenAIKeySet {
		sb.WriteString("✅ Voice transcription is ready!\n")
		sb.WriteString("Backend: Whisper API")
		_, _ = h.client.SendMessage(ctx, chatID, sb.String(), "")
		return
	}

	sb.WriteString("🎤 Voice transcription not available\n\n")
	sb.WriteString("Missing: OpenAI API key for Whisper\n")
	sb.WriteString("Set openai_api_key in ~/.pilot/config.yaml\n")

	var buttons [][]InlineKeyboardButton
	buttons = append(buttons, []InlineKeyboardButton{
		{Text: "🔍 Check Status", CallbackData: "voice_check_status"},
	})

	_, _ = h.client.SendMessageWithKeyboard(ctx, chatID, sb.String(), "", buttons)
}
