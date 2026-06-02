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
		log:    json.NewEncoder(os.Stdout),
		nextID: 1,
		prompt: prompt,
		cwd:    cwd,
	}

	if err := a.initialize(ctx, client); err != nil {
		return err
	}
	return a.read(ctx, client)
}

func (a *app) logRequest(method string) (int, error) {
	id := a.nextID
	a.nextID++

	if err := a.log.Encode(logEntry{Direction: "client", ID: id, Method: method}); err != nil {
		return 0, err
	}
	return id, nil
}

func (a *app) initialize(ctx context.Context, client *codexruntime.Client) error {
	id, err := a.logRequest("initialize")
	if err != nil {
		return err
	}
	title := "Pilot Spike"
	if _, err := client.Initialize(ctx, codexruntime.InitializeParams{
		ClientInfo: codexruntime.ClientInfo{
			Name:    "pilot-spike",
			Title:   &title,
			Version: "0.1.0",
		},
		Capabilities: &codexruntime.InitializeCapabilities{
			ExperimentalAPI:           true,
			RequestAttestation:        false,
			OptOutNotificationMethods: []string{},
		},
	}); err != nil {
		return err
	}
	a.initialized = true
	return a.logResponse(id, "initialize", "", "")
}

func (a *app) startThread(ctx context.Context, client *codexruntime.Client) error {
	id, err := a.logRequest("thread/start")
	if err != nil {
		return err
	}
	ephemeral := true
	resp, err := client.ThreadStart(ctx, codexruntime.ThreadStartParams{
		Cwd:                a.cwd,
		ApprovalPolicy:     codexruntime.ApprovalNever,
		ApprovalsReviewer:  codexruntime.ApprovalsReviewerUser,
		Sandbox:            codexruntime.SandboxReadOnly,
		Ephemeral:          &ephemeral,
		ThreadSource:       codexruntime.ThreadSourceUser,
		SessionStartSource: codexruntime.ThreadStartSourceStartup,
	})
	if err != nil {
		return err
	}
	a.threadID = resp.Thread.ID
	return a.logResponse(id, "thread/start", a.threadID, "")
}

func (a *app) startTurn(ctx context.Context, client *codexruntime.Client) error {
	id, err := a.logRequest("turn/start")
	if err != nil {
		return err
	}
	resp, err := client.TurnStart(ctx, codexruntime.TurnStartParams{
		ThreadID:       a.threadID,
		Cwd:            a.cwd,
		ApprovalPolicy: codexruntime.ApprovalNever,
		Input:          []codexruntime.UserInput{codexruntime.TextUserInput(a.prompt)},
	})
	if err != nil {
		return err
	}
	a.startedTurn = true
	a.turnID = resp.Turn.ID
	return a.logResponse(id, "turn/start", "", a.turnID)
}

func (a *app) logResponse(id int, method, threadID, turnID string) error {
	entry := logEntry{Direction: "server", ID: id, ResponseTo: method}
	if threadID != "" {
		entry.ThreadID = threadID
	}
	if turnID != "" {
		entry.TurnID = turnID
	}
	return a.log.Encode(entry)
}

func (a *app) read(ctx context.Context, client *codexruntime.Client) error {
	if err := client.Notify("initialized", nil); err != nil {
		return err
	}
	if err := a.log.Encode(logEntry{Direction: "client", Method: "initialized"}); err != nil {
		return err
	}

	if err := a.startThread(ctx, client); err != nil {
		return err
	}

	if err := a.startTurn(ctx, client); err != nil {
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
