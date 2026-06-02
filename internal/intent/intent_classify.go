package intent

import (
	"regexp"
	"strings"
)

// ContainsTaskReference checks if message references a task, file, or specific item
func ContainsTaskReference(msg string) bool {
	// Task IDs, issue numbers, file names
	patterns := []string{
		`task[- ]?\d+`, // TASK-01, task 01
		`#\d+`,         // #123
		`\d{2,}`,       // numbers like 04, 123
		`\.\w{2,4}$`,   // file extensions
		`pick|select|open|show|do|run|work on|start`,
	}
	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, msg); matched {
			return true
		}
	}
	return false
}

// IsLikelyGreeting checks if a short message is likely just a greeting
func IsLikelyGreeting(msg string) bool {
	words := strings.Fields(msg)
	if len(words) > 3 {
		return false
	}
	for _, pattern := range greetingPatterns {
		if msg == pattern || strings.HasPrefix(msg, pattern+" ") ||
			strings.HasPrefix(msg, pattern+"!") || strings.HasPrefix(msg, pattern+",") {
			return true
		}
	}
	return false
}

// StartsWithGreeting returns true when the message opens with a greeting word
// regardless of length. This catches conversational greetings like "Hello! How
// is it going?" that are too long for IsGreeting but clearly not codebase questions.
func StartsWithGreeting(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	words := strings.Fields(lower)
	if len(words) == 0 || len(words) > 10 {
		return false
	}
	for _, pattern := range greetingPatterns {
		pWords := strings.Fields(pattern)
		if len(pWords) == 1 {
			// Single-word greeting: match word or word + punctuation
			w := strings.TrimRight(words[0], "!?,.")
			if w == pattern {
				return true
			}
		} else if len(words) >= len(pWords) {
			// Multi-word greeting (e.g. "good morning")
			// Strip trailing punctuation from the last pattern word for matching
			cleaned := make([]string, len(pWords))
			for i, w := range words[:len(pWords)] {
				cleaned[i] = strings.TrimRight(w, "!?,.")
			}
			if strings.Join(cleaned, " ") == pattern {
				return true
			}
		}
	}
	return false
}

// IsGreeting checks if the message is a greeting
func IsGreeting(msg string) bool {
	// Very short messages that are just greetings
	words := strings.Fields(msg)
	if len(words) <= 3 {
		for _, pattern := range greetingPatterns {
			if msg == pattern || strings.HasPrefix(msg, pattern+" ") || strings.HasPrefix(msg, pattern+"!") || strings.HasPrefix(msg, pattern+",") {
				return true
			}
		}
	}
	return false
}

// IsQuestion checks if the message is a question
func IsQuestion(msg string) bool {
	// Ends with question mark
	if strings.HasSuffix(msg, "?") {
		return true
	}

	// Starts with question patterns
	for _, pattern := range questionPatterns {
		if strings.HasPrefix(msg, pattern) {
			return true
		}
	}

	// Quick-info keywords that should be treated as questions (fast-path eligible)
	quickInfoKeywords := []string{
		"issues", "tasks", "backlog", "todos", "fixmes",
		"status", "progress", "state",
	}
	for _, keyword := range quickInfoKeywords {
		if strings.Contains(msg, keyword) && !ContainsActionWord(msg) {
			return true
		}
	}

	// Contains question-like phrases
	questionPhrases := []string{
		"tell me about", "explain", "describe",
		"show me", "list all", "find all", "list",
	}
	for _, phrase := range questionPhrases {
		if strings.Contains(msg, phrase) && !ContainsActionWord(msg) {
			return true
		}
	}

	return false
}

// IsTask checks if the message looks like a task request
func IsTask(msg string) bool {
	return ContainsActionWord(msg)
}

// IsResearch checks if the message is a research/analysis request
func IsResearch(msg string) bool {
	for _, pattern := range researchPatterns {
		// Check for pattern at start or after common prefixes
		patterns := []string{
			"^" + pattern + "\\b",           // starts with pattern
			"\\bplease " + pattern + "\\b",  // please + pattern
			"\\bcan you " + pattern + "\\b", // can you + pattern
			"\\bi need " + pattern + "\\b",  // i need + pattern
			"\\bi want " + pattern + "\\b",  // i want + pattern
		}
		for _, p := range patterns {
			if matched, _ := regexp.MatchString(p, msg); matched {
				return true
			}
		}
	}
	return false
}

// IsPlanning checks if the message is a planning/design request
func IsPlanning(msg string) bool {
	for _, pattern := range planningPatterns {
		// Use word boundary matching to avoid "architect" matching "architecture"
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(pattern) + `\b`)
		if re.MatchString(msg) {
			return true
		}
	}
	return false
}

// IsChat checks if the message is conversational/opinion-seeking
func IsChat(msg string) bool {
	for _, pattern := range chatPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// ContainsActionWord checks if message contains task action words
func ContainsActionWord(msg string) bool {
	for _, action := range taskActionWords {
		// Check for action word at start or after common prefixes
		patterns := []string{
			"^" + action + "\\b",           // starts with action
			"\\bplease " + action + "\\b",  // please + action
			"\\bcan you " + action + "\\b", // can you + action
			"\\bi need " + action + "\\b",  // i need + action
			"\\bi want " + action + "\\b",  // i want + action
		}
		for _, pattern := range patterns {
			if matched, _ := regexp.MatchString(pattern, msg); matched {
				return true
			}
		}
	}
	return false
}

// IsClearQuestion checks if message is unambiguously a question.
// These patterns are high-confidence and don't need LLM verification.
// Used as pre-check before LLM classification to avoid false Task classifications.
func IsClearQuestion(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))

	// Ends with question mark - very clear signal
	if strings.HasSuffix(lower, "?") {
		return true
	}

	// Clear question starters that rarely indicate tasks
	clearPatterns := []string{
		"what's in", "what is in", "whats in",
		"what's the", "what is the", "whats the",
		"how does", "how do", "how can",
		"where is", "where are", "where's",
		"why is", "why are", "why does",
		"when is", "when does", "when will",
		"who is", "who are",
		"which", "can you explain", "could you explain",
	}

	for _, pattern := range clearPatterns {
		if strings.HasPrefix(lower, pattern) {
			return true
		}
	}

	return false
}
