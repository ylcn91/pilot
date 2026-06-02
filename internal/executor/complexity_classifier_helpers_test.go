package executor

import (
	"context"
)

// mockClaudeRunner creates a test runner that returns canned classification JSON.
func mockClaudeRunner(complexity, reason string) func(ctx context.Context, args ...string) ([]byte, error) {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte(`{"complexity":"` + complexity + `","reason":"` + reason + `"}`), nil
	}
}

// mockClaudeRunnerError creates a test runner that returns an error.
func mockClaudeRunnerError(err error) func(ctx context.Context, args ...string) ([]byte, error) {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		return nil, err
	}
}
