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

func execBash(command string, timeout int, cwd string) string {
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

func execReadFile(path string, offset, limit int) string {
	data, err := os.ReadFile(path)
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

func execWriteFile(path, content string) string {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Sprintf("[ERROR creating dirs: %v]", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Sprintf("[ERROR writing %s: %v]", path, err)
	}
	return fmt.Sprintf("[Wrote %d bytes to %s]", len(content), path)
}

func execEditFile(path, oldStr, newStr string) string {
	data, err := os.ReadFile(path)
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
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
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
		return execReadFile(path, int(offset), int(limit))
	case "write_file":
		path, _ := input["path"].(string)
		content, _ := input["content"].(string)
		return execWriteFile(path, content)
	case "edit_file":
		path, _ := input["path"].(string)
		oldStr, _ := input["old_string"].(string)
		newStr, _ := input["new_string"].(string)
		return execEditFile(path, oldStr, newStr)
	default:
		return fmt.Sprintf("[Unknown tool: %s]", name)
	}
}
