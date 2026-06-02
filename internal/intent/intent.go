package intent

import (
	"strings"
)

// Intent represents the detected intent of a message
type Intent string

const (
	IntentCommand  Intent = "command"
	IntentGreeting Intent = "greeting"
	IntentResearch Intent = "research"
	IntentPlanning Intent = "planning"
	IntentQuestion Intent = "question"
	IntentChat     Intent = "chat"
	IntentTask     Intent = "task"
)

// Common greeting patterns
var greetingPatterns = []string{
	"hi", "hello", "hey", "hola", "привет", "yo", "sup",
	"good morning", "good afternoon", "good evening",
	"howdy", "greetings", "what's up", "whats up",
}

// Question indicators
var questionPatterns = []string{
	"what is", "what are", "what's", "whats", "what does", "what do",
	"how do", "how does", "how can", "how to",
	"where is", "where are", "where's",
	"why is", "why are", "why does",
	"when is", "when does", "when will",
	"which", "who is", "who are",
	"can you tell", "could you explain",
	"do you know", "is there", "are there",
}

// Research patterns - indicate research/analysis requests
var researchPatterns = []string{
	"research", "analyze", "review", "investigate",
	"summarize", "compare", "evaluate", "assess",
}

// Planning patterns - indicate planning/design requests
var planningPatterns = []string{
	"plan", "design", "strategy", "how should we",
	"approach for", "architect", "outline",
}

// Chat patterns - indicate conversational/opinion requests
var chatPatterns = []string{
	"what do you think", "opinion on", "thoughts about",
	"do you recommend", "should i", "is it better",
	"discuss", "let's talk about", "lets talk about",
}

// Task action words that indicate a task request
var taskActionWords = []string{
	"create", "add", "make", "build", "implement",
	"fix", "update", "modify", "change", "edit",
	"delete", "remove", "refactor", "write",
	"generate", "setup", "configure", "install",
	// Meta-task actions (managing backlog, priorities, etc.)
	// Note: "review" moved to researchPatterns per GH-290
	"prioritize", "reprioritize", "reorder",
	"sort", "organize", "rank", "triage", "set priority",
}

// DetectIntent analyzes a message and returns the detected intent
// Priority order: Command > Greeting > Research > Planning > Question > Chat > Task
func DetectIntent(message string) Intent {
	// Normalize message
	msg := strings.ToLower(strings.TrimSpace(message))

	// 1. Commands start with /
	if strings.HasPrefix(msg, "/") {
		return IntentCommand
	}

	// 2. Check for greetings (short messages that are just greetings)
	if IsGreeting(msg) {
		return IntentGreeting
	}

	// 3. Check for research requests (research patterns with topic/URL)
	if IsResearch(msg) {
		return IntentResearch
	}

	// 4. Check for planning requests
	if IsPlanning(msg) {
		return IntentPlanning
	}

	// 5. Check for chat/conversational (opinion-seeking, no action words)
	// Checked before questions because "what do you think" starts with "what"
	if IsChat(msg) && !ContainsActionWord(msg) {
		return IntentChat
	}

	// 6. Check for questions (ends with ? or question starters)
	if IsQuestion(msg) {
		return IntentQuestion
	}

	// 7. Check for task action words
	if IsTask(msg) {
		return IntentTask
	}

	// Check for task-like references (numbers, IDs, file names)
	if ContainsTaskReference(msg) {
		return IntentTask
	}

	// Default: if message is very short AND looks like a greeting, treat as greeting
	// Otherwise treat as task (will get confirmation anyway)
	if len(msg) < 15 && IsLikelyGreeting(msg) {
		return IntentGreeting
	}

	return IntentTask
}

// Description returns a human-readable description of the intent
func (i Intent) Description() string {
	switch i {
	case IntentCommand:
		return "Command"
	case IntentGreeting:
		return "Greeting"
	case IntentResearch:
		return "Research"
	case IntentPlanning:
		return "Planning"
	case IntentQuestion:
		return "Question"
	case IntentChat:
		return "Chat"
	case IntentTask:
		return "Task"
	default:
		return "Unknown"
	}
}
