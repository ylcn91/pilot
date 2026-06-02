package autopilot

import (
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// makeCommit creates a test commit with the given message
func makeCommit(msg string) *github.Commit {
	return &github.Commit{
		SHA: "abc123",
		Commit: struct {
			Message string `json:"message"`
			Author  struct {
				Name  string    `json:"name"`
				Email string    `json:"email"`
				Date  time.Time `json:"date"`
			} `json:"author"`
		}{
			Message: msg,
		},
	}
}
