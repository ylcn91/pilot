package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type rpcError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type logEntry struct {
	Direction  string `json:"direction"`
	ID         int    `json:"id,omitempty"`
	Method     string `json:"method,omitempty"`
	ResponseTo string `json:"responseTo,omitempty"`
	ThreadID   string `json:"threadId,omitempty"`
	TurnID     string `json:"turnId,omitempty"`
	ItemID     string `json:"itemId,omitempty"`
	Status     any    `json:"status,omitempty"`
	Delta      string `json:"delta,omitempty"`
	Error      string `json:"error,omitempty"`
	Assistant  string `json:"assistant,omitempty"`
}

type app struct {
	stdin       io.Writer
	log         *json.Encoder
	requests    map[int]string
	nextID      int
	threadID    string
	turnID      string
	assistant   strings.Builder
	prompt      string
	cwd         string
	initialized bool
	startedTurn bool
}

func main() {
	var (
		cwd        string
		prompt     string
		command    string
		timeoutSec int
	)

	flag.StringVar(&cwd, "cwd", ".", "repository working directory")
	flag.StringVar(&prompt, "prompt", "Reply with exactly PONG. Do not run commands.", "free-form user prompt for turn/start")
	flag.StringVar(&command, "command", "codex", "codex CLI command")
	flag.IntVar(&timeoutSec, "timeout", 60, "timeout in seconds")
	flag.Parse()

	if prompt == "" {
		fmt.Fprintln(os.Stderr, "prompt is required")
		os.Exit(2)
	}

	absCwd, err := realCWD(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	if err := run(ctx, command, absCwd, prompt, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, command, cwd, prompt string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, command, "app-server", "--stdio")
	cmd.Dir = cwd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	errs := make(chan error, 2)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Fprintln(os.Stderr, scanner.Text())
		}
		errs <- scanner.Err()
	}()

	a := &app{
		stdin:    stdin,
		log:      json.NewEncoder(out),
		requests: make(map[int]string),
		nextID:   1,
		prompt:   prompt,
		cwd:      cwd,
	}

	if err := a.send("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "pilot-spike",
			"title":   "Pilot Spike",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"experimentalApi":           true,
			"requestAttestation":        false,
			"optOutNotificationMethods": []string{},
		},
	}); err != nil {
		return err
	}

	done, err := a.read(stdout)
	if closeErr := stdin.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if done && cmd.Process != nil {
		if signalErr := cmd.Process.Signal(os.Interrupt); signalErr != nil {
			_ = cmd.Process.Kill()
		}
	}
	if waitErr := cmd.Wait(); waitErr != nil && !done && err == nil {
		err = waitErr
	}
	if stderrErr := <-errs; stderrErr != nil && !done && err == nil {
		err = stderrErr
	}
	if ctx.Err() != nil && err == nil {
		err = ctx.Err()
	}
	return err
}

func (a *app) read(r io.Reader) (bool, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		var msg rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return false, err
		}
		done, err := a.handle(msg)
		if err != nil || done {
			return done, err
		}
	}
	return false, scanner.Err()
}

func (a *app) handle(msg rpcMessage) (bool, error) {
	if len(msg.ID) > 0 {
		return a.handleResponse(msg)
	}
	return a.handleNotification(msg)
}

func (a *app) handleResponse(msg rpcMessage) (bool, error) {
	id, err := parseID(msg.ID)
	if err != nil {
		return false, err
	}
	method := a.requests[id]

	entry := logEntry{Direction: "server", ID: id, ResponseTo: method}
	if msg.Error != nil {
		entry.Error = msg.Error.Message
		if logErr := a.log.Encode(entry); logErr != nil {
			return false, logErr
		}
		return false, fmt.Errorf("%s failed: %s", method, msg.Error.Message)
	}

	switch method {
	case "initialize":
		a.initialized = true
		if err := a.log.Encode(entry); err != nil {
			return false, err
		}
		if err := a.notify("initialized"); err != nil {
			return false, err
		}
		return false, a.send("thread/start", map[string]any{
			"cwd":                a.cwd,
			"approvalPolicy":     "never",
			"approvalsReviewer":  "user",
			"sandbox":            "read-only",
			"ephemeral":          true,
			"threadSource":       "user",
			"sessionStartSource": "startup",
		})
	case "thread/start":
		threadID, err := extractString(msg.Result, "thread", "id")
		if err != nil {
			return false, err
		}
		a.threadID = threadID
		entry.ThreadID = threadID
		if err := a.log.Encode(entry); err != nil {
			return false, err
		}
		return false, a.send("turn/start", map[string]any{
			"threadId":       threadID,
			"cwd":            a.cwd,
			"approvalPolicy": "never",
			"input": []map[string]any{
				{
					"type":          "text",
					"text":          a.prompt,
					"text_elements": []any{},
				},
			},
		})
	case "turn/start":
		turnID, err := extractString(msg.Result, "turn", "id")
		if err != nil {
			return false, err
		}
		a.startedTurn = true
		a.turnID = turnID
		entry.TurnID = turnID
		return false, a.log.Encode(entry)
	default:
		return false, a.log.Encode(entry)
	}
}

func (a *app) handleNotification(msg rpcMessage) (bool, error) {
	entry := logEntry{Direction: "server", Method: msg.Method}
	fields, err := objectFields(msg.Params)
	if err != nil {
		return false, err
	}

	entry.ThreadID = stringField(fields, "threadId")
	entry.TurnID = stringField(fields, "turnId")
	entry.ItemID = stringField(fields, "itemId")
	entry.Delta = stringField(fields, "delta")
	if status, ok := fields["status"]; ok {
		entry.Status = status
	}

	if msg.Method == "item/agentMessage/delta" && entry.Delta != "" {
		a.assistant.WriteString(entry.Delta)
	}
	if msg.Method == "error" {
		entry.Error = stringField(fields, "message")
	}
	if msg.Method == "turn/completed" {
		entry.Assistant = a.assistant.String()
	}

	if err := a.log.Encode(entry); err != nil {
		return false, err
	}
	if msg.Method == "error" {
		if entry.Error == "" {
			entry.Error = "app-server error"
		}
		return false, errors.New(entry.Error)
	}
	return msg.Method == "turn/completed", nil
}

func (a *app) send(method string, params any) error {
	id := a.nextID
	a.nextID++
	a.requests[id] = method

	msg := map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	}
	if err := writeLine(a.stdin, msg); err != nil {
		return err
	}
	return a.log.Encode(logEntry{Direction: "client", ID: id, Method: method})
}

func (a *app) notify(method string) error {
	if err := writeLine(a.stdin, map[string]any{"method": method}); err != nil {
		return err
	}
	return a.log.Encode(logEntry{Direction: "client", Method: method})
}

func writeLine(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func parseID(raw json.RawMessage) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

func extractString(raw json.RawMessage, path ...string) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}

	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("missing object at %q", key)
		}
		current, ok = object[key]
		if !ok {
			return "", fmt.Errorf("missing field %q", key)
		}
	}

	text, ok := current.(string)
	if !ok {
		return "", fmt.Errorf("field %q is not a string", strings.Join(path, "."))
	}
	return text, nil
}

func objectFields(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func stringField(fields map[string]any, key string) string {
	value, ok := fields[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

func realCWD(path string) (string, error) {
	if path == "" {
		path = "."
	}
	return filepath.Abs(path)
}
