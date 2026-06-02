package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// --- API Call with Streaming ---

func (b *AnthropicBackend) callAPI(ctx context.Context, req *apiRequest) (*apiResponse, error) {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	backoffs := b.retryWaits
	if backoffs == nil {
		backoffs = []time.Duration{30 * time.Second, 60 * time.Second, 90 * time.Second, 120 * time.Second, 180 * time.Second}
	}

	for attempt := 0; attempt <= apiMaxRetries; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", b.apiURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
		httpReq.Header.Set("Accept", "text/event-stream")

		// All sk-ant-* tokens (API keys AND OAuth) use x-api-key header.
		// This matches effort_classifier.go behavior — OAuth tokens work
		// with x-api-key but NOT with Authorization: Bearer.
		if strings.HasPrefix(b.apiKey, "sk-ant-") {
			httpReq.Header.Set("x-api-key", b.apiKey)
		} else {
			httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
		}

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			if attempt < apiMaxRetries {
				slog.Warn("HTTP error, retrying", slog.Int("attempt", attempt+1), slog.Any("error", err))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(backoffs[min(attempt, len(backoffs)-1)]):
				}
				continue
			}
			return nil, fmt.Errorf("HTTP request failed: %w", err)
		}

		// Handle non-200 responses
		if resp.StatusCode == 429 || resp.StatusCode == 529 || resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			if attempt < apiMaxRetries {
				wait := backoffs[min(attempt, len(backoffs)-1)]
				slog.Warn("API error, retrying", slog.Int("status", resp.StatusCode), slog.Duration("wait", wait))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			return nil, fmt.Errorf("API returned %d after %d retries", resp.StatusCode, apiMaxRetries)
		}

		if resp.StatusCode != 200 {
			respBody, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 500)]))
		}

		// Parse SSE stream → accumulate into final response
		result, err := b.parseSSEStream(resp.Body)
		_ = resp.Body.Close()

		if err != nil {
			// Retry on overloaded errors in response body
			if strings.Contains(err.Error(), "overloaded") && attempt < apiMaxRetries {
				wait := backoffs[min(attempt, len(backoffs)-1)]
				slog.Warn("Overloaded in response, retrying", slog.Duration("wait", wait))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			return nil, err
		}

		return result, nil
	}

	return nil, fmt.Errorf("exhausted %d retries", apiMaxRetries)
}

// parseSSEStream reads SSE events and accumulates the final message.
func (b *AnthropicBackend) parseSSEStream(body io.Reader) (*apiResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large responses

	var result apiResponse
	var currentBlocks []apiContentBlock
	currentBlockTexts := make(map[int]strings.Builder)
	currentBlockInputs := make(map[int]strings.Builder)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event sseEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "message_start":
			if event.Message != nil {
				result.ID = event.Message.ID
				result.Model = event.Message.Model
				result.Role = event.Message.Role
				result.Usage.InputTokens += event.Message.Usage.InputTokens
			}

		case "content_block_start":
			if event.ContentBlock != nil {
				// Grow blocks slice to fit
				for len(currentBlocks) <= event.Index {
					currentBlocks = append(currentBlocks, apiContentBlock{})
				}
				currentBlocks[event.Index] = *event.ContentBlock
			}

		case "content_block_delta":
			if event.Delta != nil {
				switch event.Delta.Type {
				case "text_delta":
					if _, ok := currentBlockTexts[event.Index]; !ok {
						currentBlockTexts[event.Index] = strings.Builder{}
					}
					b := currentBlockTexts[event.Index]
					b.WriteString(event.Delta.Text)
					currentBlockTexts[event.Index] = b
				case "input_json_delta":
					if _, ok := currentBlockInputs[event.Index]; !ok {
						currentBlockInputs[event.Index] = strings.Builder{}
					}
					b := currentBlockInputs[event.Index]
					b.WriteString(event.Delta.PartialJSON)
					currentBlockInputs[event.Index] = b
				case "thinking_delta":
					// Thinking content — we don't need to store it
				}
			}

		case "content_block_stop":
			// Finalize block text/input
			if sb, ok := currentBlockTexts[event.Index]; ok && event.Index < len(currentBlocks) {
				currentBlocks[event.Index].Text = sb.String()
			}
			if sb, ok := currentBlockInputs[event.Index]; ok && event.Index < len(currentBlocks) {
				currentBlocks[event.Index].Input = json.RawMessage(sb.String())
			}

		case "message_delta":
			if event.Delta != nil && event.Delta.StopReason != "" {
				result.StopReason = event.Delta.StopReason
			}
			if event.Usage != nil {
				result.Usage.OutputTokens += event.Usage.OutputTokens
			}

		case "message_stop":
			// Final event

		case "error":
			// Error in stream. Surface the inner error type/message so the retry
			// classification in callAPI (Contains "overloaded") works for in-stream
			// errors, not just HTTP-status ones — otherwise an overloaded_error
			// delivered mid-stream is returned immediately instead of retried.
			if event.Error != nil {
				detail := strings.TrimSpace(event.Error.Type + ": " + event.Error.Message)
				if strings.Contains(strings.ToLower(detail), "overloaded") {
					return nil, fmt.Errorf("overloaded: %s", detail)
				}
				return nil, fmt.Errorf("stream error: %s", detail)
			}
			errData, _ := json.Marshal(event)
			return nil, fmt.Errorf("stream error: %s", string(errData))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream read error: %w", err)
	}

	result.Content = currentBlocks

	// Check for error in response
	if result.Type == "error" || (result.Error != nil && result.Error.Type != "") {
		errMsg := "unknown error"
		if result.Error != nil {
			errMsg = result.Error.Message
		}
		if strings.Contains(strings.ToLower(errMsg), "overloaded") {
			return nil, fmt.Errorf("overloaded: %s", errMsg)
		}
		return nil, fmt.Errorf("API error: %s", errMsg)
	}

	return &result, nil
}
