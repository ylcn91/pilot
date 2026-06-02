package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/codexruntime"
)

type chatOptions struct {
	cwd     string
	command string
	model   string
	sandbox string
	prompt  string
}

func newChatCmd() *cobra.Command {
	opts := chatOptions{
		cwd:     ".",
		command: "codex",
		sandbox: string(codexruntime.SandboxReadOnly),
	}

	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Chat with the current repository through Codex app-server",
		Long: `Start a free-form Codex app-server turn without a ticket.

Examples:
  pilot chat "Review this repository's test strategy"
  pilot chat --sandbox workspace-write "Update README with a short usage note"`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt, err := chatPrompt(args, cmd.InOrStdin())
			if err != nil {
				return err
			}
			opts.prompt = prompt

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return runChat(ctx, opts, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&opts.cwd, "cwd", ".", "Repository working directory")
	cmd.Flags().StringVar(&opts.command, "command", "codex", "Codex CLI command")
	cmd.Flags().StringVar(&opts.model, "model", "", "Model override for this thread")
	cmd.Flags().StringVar(&opts.sandbox, "sandbox", string(codexruntime.SandboxReadOnly), "Sandbox mode: read-only, workspace-write, danger-full-access")

	return cmd
}

func chatPrompt(args []string, stdin io.Reader) (string, error) {
	if len(args) > 0 {
		prompt := strings.TrimSpace(strings.Join(args, " "))
		if prompt == "" {
			return "", errors.New("prompt is required")
		}
		return prompt, nil
	}

	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		var b strings.Builder
		scanner := bufio.NewScanner(stdin)
		for scanner.Scan() {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}
		prompt := strings.TrimSpace(b.String())
		if prompt != "" {
			return prompt, nil
		}
	}

	return "", errors.New("prompt is required")
}

func runChat(ctx context.Context, opts chatOptions, stdout, stderr io.Writer) error {
	cwd, err := filepath.Abs(opts.cwd)
	if err != nil {
		return err
	}
	sandbox, err := parseChatSandbox(opts.sandbox)
	if err != nil {
		return err
	}

	client, err := codexruntime.Start(ctx, codexruntime.Config{
		Command: opts.command,
		Cwd:     cwd,
		Stderr:  stderr,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	if err := initializeChat(ctx, client); err != nil {
		return err
	}
	if err := client.Notify("initialized", nil); err != nil {
		return err
	}

	ephemeral := true
	thread, err := client.ThreadStart(ctx, codexruntime.ThreadStartParams{
		Cwd:                cwd,
		Model:              opts.model,
		ApprovalPolicy:     codexruntime.ApprovalNever,
		ApprovalsReviewer:  codexruntime.ApprovalsReviewerUser,
		Sandbox:            sandbox,
		Ephemeral:          &ephemeral,
		ThreadSource:       codexruntime.ThreadSourceUser,
		SessionStartSource: codexruntime.ThreadStartSourceStartup,
	})
	if err != nil {
		return err
	}

	turn, err := client.TurnStart(ctx, codexruntime.TurnStartParams{
		ThreadID:       thread.Thread.ID,
		Cwd:            cwd,
		ApprovalPolicy: codexruntime.ApprovalNever,
		Model:          opts.model,
		Input:          []codexruntime.UserInput{codexruntime.TextUserInput(opts.prompt)},
	})
	if err != nil {
		return err
	}
	_ = turn

	assistantStarted := false
	for {
		select {
		case msg, ok := <-client.Notifications():
			if !ok {
				return errors.New("codex app-server closed before turn completed")
			}
			event, err := codexruntime.MapNotification(msg)
			if err != nil {
				return err
			}
			switch event.Type {
			case codexruntime.EventAgentMessageDelta:
				assistantStarted = true
				if _, err := fmt.Fprint(stdout, event.Delta); err != nil {
					return err
				}
			case codexruntime.EventReasoningDelta:
				if !quietMode {
					if _, err := fmt.Fprint(stderr, event.Delta); err != nil {
						return err
					}
				}
			case codexruntime.EventError:
				if event.Error == "" {
					event.Error = "codex app-server error"
				}
				return errors.New(event.Error)
			case codexruntime.EventTurnCompleted:
				if assistantStarted {
					_, _ = fmt.Fprintln(stdout)
				}
				return nil
			}
		case req, ok := <-client.ServerRequests():
			if !ok {
				continue
			}
			if err := denyChatServerRequest(client, req); err != nil {
				return err
			}
		case err := <-client.Errors():
			return err
		case <-ctx.Done():
			if thread.Thread.ID != "" && turn.Turn.ID != "" {
				_, _ = client.TurnInterrupt(context.Background(), codexruntime.TurnInterruptParams{
					ThreadID: thread.Thread.ID,
					TurnID:   turn.Turn.ID,
				})
			}
			return ctx.Err()
		}
	}
}

func initializeChat(ctx context.Context, client *codexruntime.Client) error {
	title := "Pilot CLI"
	_, err := client.Initialize(ctx, codexruntime.InitializeParams{
		ClientInfo: codexruntime.ClientInfo{
			Name:    "pilot",
			Title:   &title,
			Version: version,
		},
		Capabilities: &codexruntime.InitializeCapabilities{
			ExperimentalAPI:           true,
			RequestAttestation:        false,
			OptOutNotificationMethods: []string{},
		},
	})
	return err
}

func parseChatSandbox(value string) (codexruntime.SandboxMode, error) {
	switch codexruntime.SandboxMode(value) {
	case codexruntime.SandboxReadOnly:
		return codexruntime.SandboxReadOnly, nil
	case codexruntime.SandboxWorkspaceWrite:
		return codexruntime.SandboxWorkspaceWrite, nil
	case codexruntime.SandboxDangerFull:
		return codexruntime.SandboxDangerFull, nil
	default:
		return "", fmt.Errorf("invalid sandbox %q", value)
	}
}

func denyChatServerRequest(client *codexruntime.Client, req codexruntime.Message) error {
	switch req.Method {
	case "execCommandApproval", "applyPatchApproval":
		return client.RespondReviewApproval(req, codexruntime.ReviewDenied)
	case "item/commandExecution/requestApproval":
		return client.RespondCommandExecutionApproval(req, codexruntime.CommandExecutionDecline)
	case "item/fileChange/requestApproval":
		return client.RespondFileChangeApproval(req, codexruntime.FileChangeDecline)
	case "item/permissions/requestApproval":
		return client.RespondPermissionsApproval(req, codexruntime.PermissionsApprovalResponse{
			Permissions: codexruntime.GrantedPermissionProfile{},
			Scope:       codexruntime.PermissionGrantTurn,
		})
	default:
		return client.RespondError(req, -32601, "unsupported server request", nil)
	}
}
