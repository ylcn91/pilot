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
	"github.com/ylcn91/pilot/internal/executor"
)

type chatOptions struct {
	cwd       string
	command   string
	profile   string
	env       []string
	model     string
	sandbox   string
	prompt    string
	noPriming bool
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
	cmd.Flags().StringVar(&opts.profile, "profile", "", "Codex config profile passed to app-server")
	cmd.Flags().StringArrayVar(&opts.env, "env", nil, "Environment variable passed to app-server process as KEY=value")
	cmd.Flags().StringVar(&opts.model, "model", "", "Model override for this thread")
	cmd.Flags().StringVar(&opts.sandbox, "sandbox", string(codexruntime.SandboxReadOnly), "Sandbox mode: read-only, workspace-write, danger-full-access")
	cmd.Flags().BoolVar(&opts.noPriming, "no-priming", false, "Skip injecting .agent guidance on the first turn")

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
		Args:    chatAppServerArgs(opts.profile),
		Cwd:     cwd,
		Env:     opts.env,
		Stderr:  stderr,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	if err := initializeChat(ctx, client); err != nil {
		return fmt.Errorf("initialize app-server: %w", err)
	}
	if err := client.Notify("initialized", nil); err != nil {
		return fmt.Errorf("notify initialized: %w", err)
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
		return fmt.Errorf("thread/start: %w", err)
	}

	// Prime the first turn with .agent guidance so the codex-app-server backend
	// receives the same project context Claude Code gets via BuildPrompt.
	firstPrompt := opts.prompt
	if !opts.noPriming {
		if preamble := executor.BuildGuidancePreamble(filepath.Join(cwd, ".agent"), opts.prompt); preamble != "" {
			firstPrompt = preamble + "\n\n" + opts.prompt
		}
	}

	turn, err := client.TurnStart(ctx, codexruntime.TurnStartParams{
		ThreadID:       thread.Thread.ID,
		Cwd:            cwd,
		ApprovalPolicy: codexruntime.ApprovalNever,
		Model:          opts.model,
		Input:          []codexruntime.UserInput{codexruntime.TextUserInput(firstPrompt)},
	})
	if err != nil {
		return fmt.Errorf("turn/start: %w", err)
	}
	_ = turn

	assistantStarted := false
	traceChat := os.Getenv("PILOT_CHAT_TRACE") == "1"
	for {
		select {
		case msg, ok := <-client.Notifications():
			if !ok {
				return errors.New("codex app-server closed before turn completed")
			}
			if traceChat {
				_, _ = fmt.Fprintf(stderr, "[app-server notification] method=%s params=%s\n", msg.Method, string(msg.Params))
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
					if len(event.RawParams) > 0 {
						event.Error = fmt.Sprintf("codex app-server error: %s", string(event.RawParams))
					} else {
						event.Error = "codex app-server error"
					}
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
			if traceChat {
				_, _ = fmt.Fprintf(stderr, "[app-server request] method=%s params=%s\n", req.Method, string(req.Params))
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

func chatAppServerArgs(profile string) []string {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return nil
	}
	return []string{"--profile", profile, "app-server", "--stdio"}
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
