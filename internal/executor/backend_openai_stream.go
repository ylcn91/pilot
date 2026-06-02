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

func (b *OpenAIBackend) callAPI(ctx context.Context, req *openaiRequest) (*openaiAccum, error) {
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
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)

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

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
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

		result, err := b.parseSSEStream(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	return nil, fmt.Errorf("exhausted %d retries", apiMaxRetries)
}

// parseSSEStream reads an OpenAI SSE stream and accumulates the full response.
// Tool call argument fragments are joined by index before JSON-parsing.
func (b *OpenAIBackend) parseSSEStream(body io.Reader) (*openaiAccum, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var result openaiAccum
	var contentBuf strings.Builder
	pending := make(map[int]*pendingOAIToolCall)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openaiChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if result.Model == "" && chunk.Model != "" {
			result.Model = chunk.Model
		}

		if chunk.Usage != nil {
			result.PromptTokens = chunk.Usage.PromptTokens
			result.CompletionTokens = chunk.Usage.CompletionTokens
		}

		for _, choice := range chunk.Choices {
			// Accumulate text
			if choice.Delta.Content != "" {
				contentBuf.WriteString(choice.Delta.Content)
			}

			// Accumulate tool call fragments by index
			for _, dtc := range choice.Delta.ToolCalls {
				p, ok := pending[dtc.Index]
				if !ok {
					p = &pendingOAIToolCall{}
					pending[dtc.Index] = p
				}
				if dtc.ID != "" {
					p.id = dtc.ID
				}
				if dtc.Function.Name != "" {
					p.name = dtc.Function.Name
				}
				p.arguments.WriteString(dtc.Function.Arguments)
			}

			if choice.FinishReason != nil && *choice.FinishReason != "" {
				result.FinishReason = *choice.FinishReason
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream read error: %w", err)
	}

	result.Content = contentBuf.String()

	// Finalize tool calls in index order
	for i := 0; i < len(pending); i++ {
		p, ok := pending[i]
		if !ok {
			break
		}
		result.ToolCalls = append(result.ToolCalls, openaiToolCall{
			ID:   p.id,
			Type: "function",
			Function: openaiCallFunc{
				Name:      p.name,
				Arguments: p.arguments.String(),
			},
		})
	}

	return &result, nil
}
