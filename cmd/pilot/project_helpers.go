package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ylcn91/pilot/internal/config"
)

// Helper functions

func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	// Try to show the end of the path with ...
	return "..." + path[len(path)-(maxLen-3):]
}

func expandProjectPath(path string) string {
	if strings.HasPrefix(path, "~") {
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, path[1:])
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absPath
}

func detectGitHubFromRemote(path string) *config.ProjectGitHubConfig {
	cmd := exec.Command("git", "-C", path, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	url := strings.TrimSpace(string(output))
	return parseGitHubURL(url)
}

func parseGitHubURL(url string) *config.ProjectGitHubConfig {
	// Handle SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		url = strings.TrimPrefix(url, "git@github.com:")
		url = strings.TrimSuffix(url, ".git")
		parts := strings.SplitN(url, "/", 2)
		if len(parts) == 2 {
			return &config.ProjectGitHubConfig{
				Owner: parts[0],
				Repo:  parts[1],
			}
		}
	}

	// Handle HTTPS format: https://github.com/owner/repo.git
	if strings.Contains(url, "github.com/") {
		idx := strings.Index(url, "github.com/")
		url = url[idx+len("github.com/"):]
		url = strings.TrimSuffix(url, ".git")
		parts := strings.SplitN(url, "/", 2)
		if len(parts) == 2 {
			return &config.ProjectGitHubConfig{
				Owner: parts[0],
				Repo:  parts[1],
			}
		}
	}

	return nil
}

func detectDefaultBranch(path string) string {
	// Try to get default branch from remote
	cmd := exec.Command("git", "-C", path, "symbolic-ref", "refs/remotes/origin/HEAD")
	output, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(output))
		// Extract branch name from refs/remotes/origin/main
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	// Fallback: check if main or master exists
	for _, branch := range []string{"main", "master"} {
		cmd := exec.Command("git", "-C", path, "rev-parse", "--verify", fmt.Sprintf("refs/heads/%s", branch))
		if err := cmd.Run(); err == nil {
			return branch
		}
	}

	return "main" // Default fallback
}

func detectNavigator(path string) bool {
	agentPath := filepath.Join(path, ".agent")
	info, err := os.Stat(agentPath)
	return err == nil && info.IsDir()
}
