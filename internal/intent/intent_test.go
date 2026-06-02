package intent

import (
	"testing"
)

func TestDetectIntent(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected Intent
	}{
		// Commands
		{"command /help", "/help", IntentCommand},
		{"command /start", "/start", IntentCommand},
		{"command /status", "/status", IntentCommand},
		{"command /cancel", "/cancel", IntentCommand},

		// Greetings
		{"greeting hi", "hi", IntentGreeting},
		{"greeting hello", "hello", IntentGreeting},
		{"greeting hey", "hey", IntentGreeting},
		{"greeting hello!", "hello!", IntentGreeting},
		{"greeting good morning", "good morning", IntentGreeting},
		{"greeting hi there", "hi there", IntentGreeting},
		{"greeting привет", "привет", IntentGreeting},
		{"greeting yo", "yo", IntentGreeting},

		// Questions
		{"question with ?", "what is the auth handler?", IntentQuestion},
		{"question what is", "what is the project structure", IntentQuestion},
		{"question how do", "how do I run tests", IntentQuestion},
		{"question where is", "where is the config file", IntentQuestion},
		{"question can you tell", "can you tell me about the api", IntentQuestion},
		{"question show me", "show me the error handlers", IntentQuestion},
		{"question explain", "explain the auth flow", IntentQuestion},

		// Tasks
		{"task create", "create a new file", IntentTask},
		{"task add", "add a function to handle auth", IntentTask},
		{"task fix", "fix the bug in login", IntentTask},
		{"task update", "update the readme", IntentTask},
		{"task implement", "implement user logout", IntentTask},
		{"task refactor", "refactor the auth module", IntentTask},
		{"task please add", "please add error handling", IntentTask},
		{"task pick", "pick 04", IntentTask},
		{"task with ID", "work on TASK-04", IntentTask},
		{"task with number", "do 04", IntentTask},

		// Research
		{"research article", "research this article", IntentResearch},
		{"analyze codebase", "analyze the codebase", IntentResearch},
		{"review PR", "review PR #123", IntentResearch},
		{"investigate issue", "investigate the memory leak", IntentResearch},
		{"summarize doc", "summarize this document", IntentResearch},
		{"compare options", "compare these two approaches", IntentResearch},
		{"evaluate solution", "evaluate this solution", IntentResearch},
		{"assess risk", "assess the security risk", IntentResearch},
		{"please research", "please research this topic", IntentResearch},
		{"can you analyze", "can you analyze the logs", IntentResearch},

		// Planning
		{"plan auth", "plan how to add auth", IntentPlanning},
		{"design api", "design the API", IntentPlanning},
		{"strategy scaling", "strategy for scaling", IntentPlanning},
		{"how should we", "how should we handle errors", IntentPlanning},
		{"approach for", "approach for caching", IntentPlanning},
		{"architect system", "architect the system", IntentPlanning},
		{"outline feature", "outline the feature", IntentPlanning},

		// Chat (without ? to avoid question priority)
		{"what do you think", "what do you think about Redis", IntentChat},
		{"should I use", "should i use TypeScript", IntentChat},
		{"opinion on", "opinion on microservices", IntentChat},
		{"thoughts about", "thoughts about this approach", IntentChat},
		{"do you recommend", "do you recommend using Go", IntentChat},
		{"is it better", "is it better to use SQL or NoSQL", IntentChat},
		{"discuss topic", "discuss the tradeoffs", IntentChat},
		{"lets talk about", "lets talk about performance", IntentChat},
		// Chat with ? still becomes chat (chat checked before question)
		{"chat with question mark", "what do you think about Redis?", IntentChat},

		// Edge cases
		{"what does question", "what does the auth module do", IntentQuestion},
		{"ambiguous greeting", "hello world file", IntentGreeting}, // "hello" starts msg, <= 3 words
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectIntent(tt.message)
			if got != tt.expected {
				t.Errorf("DetectIntent(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

func TestIntentDescription(t *testing.T) {
	tests := []struct {
		intent   Intent
		expected string
	}{
		{IntentCommand, "Command"},
		{IntentGreeting, "Greeting"},
		{IntentResearch, "Research"},
		{IntentPlanning, "Planning"},
		{IntentQuestion, "Question"},
		{IntentChat, "Chat"},
		{IntentTask, "Task"},
		{Intent("unknown"), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.intent), func(t *testing.T) {
			got := tt.intent.Description()
			if got != tt.expected {
				t.Errorf("%v.Description() = %v, want %v", tt.intent, got, tt.expected)
			}
		})
	}
}

// TestDetectIntentEdgeCases tests edge cases in intent detection
func TestDetectIntentEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected Intent
	}{
		// Commands always win
		{"slash command with text", "/help me with something", IntentCommand},
		{"slash command uppercase", "/STATUS", IntentCommand},

		// Short greetings
		{"just hi", "hi", IntentGreeting},
		{"hi with punctuation", "hi!", IntentGreeting},

		// Questions with question patterns
		{"what with question mark", "what is this?", IntentQuestion},
		{"how question", "how do I run tests", IntentQuestion},
		{"where question", "where is the config", IntentQuestion},
		{"why question", "why is this failing", IntentQuestion},
		{"explain phrase", "explain the auth flow", IntentQuestion},
		{"show me phrase", "show me the handlers", IntentQuestion},
		{"list phrase", "list all endpoints", IntentQuestion},

		// Questions with quick-info keywords
		{"issues keyword", "what are the issues", IntentQuestion},
		{"backlog keyword", "show backlog", IntentQuestion},
		{"todos keyword", "show me the todos", IntentQuestion},
		{"status keyword", "check status", IntentQuestion},

		// Tasks with action words
		{"create task", "create a new handler", IntentTask},
		{"fix task", "fix the login bug", IntentTask},
		{"add task", "add error handling", IntentTask},

		// Task references
		{"task id reference", "TASK-07", IntentTask},
		{"number reference", "07", IntentTask},
		{"pick command", "pick 04", IntentTask},

		// Ambiguous (defaults to task)
		{"ambiguous long", "something about the code that is unclear", IntentTask},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectIntent(tt.message)
			if got != tt.expected {
				t.Errorf("DetectIntent(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestIntentConstants tests intent constant values
func TestIntentConstants(t *testing.T) {
	// Verify intent constants have expected string values
	tests := []struct {
		intent   Intent
		expected string
	}{
		{IntentCommand, "command"},
		{IntentGreeting, "greeting"},
		{IntentResearch, "research"},
		{IntentPlanning, "planning"},
		{IntentQuestion, "question"},
		{IntentChat, "chat"},
		{IntentTask, "task"},
	}

	for _, tt := range tests {
		t.Run(string(tt.intent), func(t *testing.T) {
			if string(tt.intent) != tt.expected {
				t.Errorf("Intent = %q, want %q", string(tt.intent), tt.expected)
			}
		})
	}
}

// TestShortMessages tests classification of very short messages
func TestShortMessages(t *testing.T) {
	tests := []struct {
		message  string
		expected Intent
	}{
		{"hi", IntentGreeting},
		{"yo", IntentGreeting},
		{"07", IntentTask},    // task reference
		{"#5", IntentTask},    // issue reference
		{"fix", IntentTask},   // action word
		{"?", IntentQuestion}, // question mark triggers question
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := DetectIntent(tt.message)
			if got != tt.expected {
				t.Errorf("DetectIntent(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestIntentStringConversion tests intent to string conversion
func TestIntentStringConversion(t *testing.T) {
	tests := []struct {
		intent Intent
		want   string
	}{
		{IntentCommand, "command"},
		{IntentGreeting, "greeting"},
		{IntentResearch, "research"},
		{IntentPlanning, "planning"},
		{IntentQuestion, "question"},
		{IntentChat, "chat"},
		{IntentTask, "task"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := string(tt.intent)
			if got != tt.want {
				t.Errorf("string(%v) = %q, want %q", tt.intent, got, tt.want)
			}
		})
	}
}

// TestNewIntentPriority tests that new intents have correct priority
func TestNewIntentPriority(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected Intent
	}{
		// Research takes priority over task (review is both)
		{"research over task", "research this article", IntentResearch},

		// Planning takes priority over question (even with ?)
		{"planning over question", "how should we handle this?", IntentPlanning},

		// Chat without action words stays chat (no ? mark)
		{"chat without action", "what do you think about Go", IntentChat},

		// Chat with ? still becomes chat (chat checked before question)
		{"chat with question mark", "what do you think about Go?", IntentChat},

		// Questions with ? still work
		{"question with ?", "what is this?", IntentQuestion},

		// Tasks with action words still work
		{"task with action", "create a new file", IntentTask},

		// Research with can you prefix
		{"research with prefix", "can you analyze this code", IntentResearch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectIntent(tt.message)
			if got != tt.expected {
				t.Errorf("DetectIntent(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}
