package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// --- Tool Execution ---

// bashDenyPatterns are unambiguously destructive command fragments with no
// legitimate use in a coding task. The direct-API tool loop (anthropic-api /
// openai-api) runs bash without the OS-level sandbox the codex backend gets, so
// this narrow denylist is a backstop — it is intentionally conservative to
// avoid false positives on normal commands.
var bashDenyPatterns = []string{
	":(){:|:&};:", // fork bomb (whitespace-stripped form)
	"mkfs",        // format a filesystem
	"dd if=/dev/zero of=/dev",
	"dd of=/dev/sd",
	"dd of=/dev/disk",
	"> /dev/sda",
	"rm -rf /;",
	"rm -rf / ",
	"rm -rf --no-preserve-root",
}

// bashGuardViolation returns the matched deny pattern if the command is one of
// the catastrophic forms in bashDenyPatterns, else "".
func bashGuardViolation(command string) string {
	// Normalize whitespace so "rm  -rf  /" and ": ( ) {" collapse to the
	// canonical form before substring matching.
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	forkbomb := strings.ToLower(strings.Join(strings.Fields(":(){:|:&};:"), ""))
	if strings.Contains(strings.ReplaceAll(normalized, " ", ""), forkbomb) {
		return "fork bomb"
	}
	for _, p := range bashDenyPatterns {
		if strings.Contains(normalized, strings.ToLower(p)) {
			return p
		}
	}
	return ""
}

// confineToWorkspace resolves path against the workspace cwd and rejects any
// target that escapes it (absolute paths outside the tree, ../ traversal). It
// is a lexical guard (no symlink resolution) — enough to stop the direct-API
// tool loop from reading/writing arbitrary files like ~/.ssh or /etc.
func confineToWorkspace(cwd, path string) (string, error) {
	if cwd == "" {
		cwd = "."
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(absCwd, target)
	}
	target = filepath.Clean(target)
	if target != absCwd && !strings.HasPrefix(target, absCwd+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the workspace", path)
	}
	return target, nil
}

func execBash(command string, timeout int, cwd string) string {
	if reason := bashGuardViolation(command); reason != "" {
		return fmt.Sprintf("[BLOCKED: command matches a destructive pattern (%s) and was not run]", reason)
	}
	if timeout <= 0 || timeout > 600 {
		timeout = apiBashTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()

	output := string(out)
	if len(output) > apiOutputCap {
		output = output[:apiOutputCap/2] + "\n\n... [truncated] ...\n\n" + output[len(output)-apiOutputCap/2:]
	}

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("%s\n[TIMEOUT after %ds — command killed]", output, timeout)
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Sprintf("%s\n[exit code: %d]", output, exitErr.ExitCode())
		}
		return fmt.Sprintf("%s\n[ERROR: %v]", output, err)
	}
	return output
}

func execReadFile(cwd, path string, offset, limit int) string {
	resolved, err := confineToWorkspace(cwd, path)
	if err != nil {
		return fmt.Sprintf("[BLOCKED: %v]", err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Sprintf("[File not found: %s]", path)
	}
	if len(data) > 500000 {
		return fmt.Sprintf("[File too large: %d bytes. Use bash: head -100 %s]", len(data), path)
	}
	lines := strings.Split(string(data), "\n")
	if offset > 0 && offset <= len(lines) {
		lines = lines[offset-1:]
	}
	if limit > 0 && limit < len(lines) {
		lines = lines[:limit]
	}
	startLine := 1
	if offset > 0 {
		startLine = offset
	}
	var sb strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&sb, "%4d | %s\n", startLine+i, line)
	}
	return sb.String()
}

func execWriteFile(cwd, path, content string) string {
	resolved, err := confineToWorkspace(cwd, path)
	if err != nil {
		return fmt.Sprintf("[BLOCKED: %v]", err)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return fmt.Sprintf("[ERROR creating dirs: %v]", err)
	}
	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return fmt.Sprintf("[ERROR writing %s: %v]", path, err)
	}
	return fmt.Sprintf("[Wrote %d bytes to %s]", len(content), path)
}

func execEditFile(cwd, path, oldStr, newStr string) string {
	resolved, err := confineToWorkspace(cwd, path)
	if err != nil {
		return fmt.Sprintf("[BLOCKED: %v]", err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Sprintf("[File not found: %s]", path)
	}
	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return fmt.Sprintf("[old_string not found in %s]", path)
	}
	if count > 1 {
		return fmt.Sprintf("[old_string appears %d times — provide more context]", count)
	}
	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(resolved, []byte(newContent), 0644); err != nil {
		return fmt.Sprintf("[ERROR writing %s: %v]", path, err)
	}
	return fmt.Sprintf("[Edited %s]", path)
}

func executeTool(name string, input map[string]interface{}, cwd string) string {
	switch name {
	case "bash":
		cmd, _ := input["command"].(string)
		timeout := apiBashTimeout
		if t, ok := input["timeout"].(float64); ok {
			timeout = int(t)
		}
		return execBash(cmd, timeout, cwd)
	case "read_file":
		path, _ := input["path"].(string)
		offset, _ := input["offset"].(float64)
		limit, _ := input["limit"].(float64)
		return execReadFile(cwd, path, int(offset), int(limit))
	case "write_file":
		path, _ := input["path"].(string)
		content, _ := input["content"].(string)
		return execWriteFile(cwd, path, content)
	case "edit_file":
		path, _ := input["path"].(string)
		oldStr, _ := input["old_string"].(string)
		newStr, _ := input["new_string"].(string)
		return execEditFile(cwd, path, oldStr, newStr)
	default:
		return fmt.Sprintf("[Unknown tool: %s]", name)
	}
}
