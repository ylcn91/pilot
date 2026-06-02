package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qf-studio/pilot/internal/codexruntime"
)

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

	if err := run(ctx, command, absCwd, prompt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, command, cwd, prompt string) error {
	client, err := codexruntime.Start(ctx, codexruntime.Config{
		Command: command,
		Cwd:     cwd,
		Stderr:  os.Stderr,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	a := &app{
		log:      json.NewEncoder(os.Stdout),
		requests: make(map[int]string),
		nextID:   1,
		prompt:   prompt,
		cwd:      cwd,
	}

	if err := a.request(ctx, client, "initialize", map[string]any{
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
	return a.read(ctx, client)
}

func (a *app) request(ctx context.Context, client *codexruntime.Client, method string, params any) error {
	id := a.nextID
	a.nextID++
	a.requests[id] = method

	if err := a.log.Encode(logEntry{Direction: "client", ID: id, Method: method}); err != nil {
		return err
	}
	msg, err := client.Request(ctx, method, params)
	if err != nil {
		return err
	}
	return a.handleResponse(id, method, msg)
}

func (a *app) handleResponse(id int, method string, msg codexruntime.Message) error {
	entry := logEntry{Direction: "server", ID: id, ResponseTo: method}
	switch method {
	case "initialize":
		a.initialized = true
		if err := a.log.Encode(entry); err != nil {
			return err
		}
		return nil
	case "thread/start":
		threadID, err := codexruntime.ExtractString(msg.Result, "thread", "id")
		if err != nil {
			return err
		}
		a.threadID = threadID
		entry.ThreadID = threadID
		return a.log.Encode(entry)
	case "turn/start":
		turnID, err := codexruntime.ExtractString(msg.Result, "turn", "id")
		if err != nil {
			return err
		}
		a.startedTurn = true
		a.turnID = turnID
		entry.TurnID = turnID
		return a.log.Encode(entry)
	default:
		return a.log.Encode(entry)
	}
}

func (a *app) read(ctx context.Context, client *codexruntime.Client) error {
	if err := client.Notify("initialized", nil); err != nil {
		return err
	}
	if err := a.log.Encode(logEntry{Direction: "client", Method: "initialized"}); err != nil {
		return err
	}

	if err := a.request(ctx, client, "thread/start", map[string]any{
		"cwd":                a.cwd,
		"approvalPolicy":     "never",
		"approvalsReviewer":  "user",
		"sandbox":            "read-only",
		"ephemeral":          true,
		"threadSource":       "user",
		"sessionStartSource": "startup",
	}); err != nil {
		return err
	}

	if err := a.request(ctx, client, "turn/start", map[string]any{
		"threadId":       a.threadID,
		"cwd":            a.cwd,
		"approvalPolicy": "never",
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          a.prompt,
				"text_elements": []any{},
			},
		},
	}); err != nil {
		return err
	}

	for {
		select {
		case msg, ok := <-client.Notifications():
			if !ok {
				return errors.New("app-server notification stream closed")
			}
			done, err := a.handleNotification(msg)
			if err != nil || done {
				return err
			}
		case err := <-client.Errors():
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (a *app) handleNotification(msg codexruntime.Message) (bool, error) {
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
