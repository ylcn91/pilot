package briefs

import (
	"context"
	"errors"

	"github.com/ylcn91/pilot/internal/adapters/slack"
)

// Mock Slack client for testing
type mockSlackClient struct {
	shouldFail bool
	lastMsg    *slack.Message
	response   *slack.PostMessageResponse
}

func (m *mockSlackClient) PostMessage(ctx context.Context, msg *slack.Message) (*slack.PostMessageResponse, error) {
	m.lastMsg = msg
	if m.shouldFail {
		return nil, errors.New("mock slack error")
	}
	if m.response != nil {
		return m.response, nil
	}
	return &slack.PostMessageResponse{
		OK:      true,
		Channel: msg.Channel,
		TS:      "1234567890.123456",
	}, nil
}

// Mock email sender for testing
type mockEmailSender struct {
	shouldFail   bool
	lastTo       []string
	lastSubject  string
	lastHtmlBody string
}

func (m *mockEmailSender) Send(ctx context.Context, to []string, subject, htmlBody string) error {
	m.lastTo = to
	m.lastSubject = subject
	m.lastHtmlBody = htmlBody
	if m.shouldFail {
		return errors.New("mock email error")
	}
	return nil
}

// Helper to create a slack.Client wrapper that uses our mock
func createSlackClientWrapper(mock *mockSlackClient) *slack.Client {
	// Since we can't easily mock the actual slack.Client, we'll use an interface approach
	// For now, return nil and handle in tests
	return nil
}

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		(haystack == needle ||
			len(haystack) > len(needle) &&
				(haystack[:len(needle)] == needle ||
					containsString(haystack[1:], needle)))
}
