package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ylcn91/pilot/internal/executor/workflow"
	"github.com/ylcn91/pilot/internal/replay"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// executePrepare builds the logger, resolves model/effort, performs git/branch
// setup, runs research, loads the per-repo workflow, builds the prompt, and sets
// up recording and Claude Code hooks (original lines ~470-737).
//
// It returns a non-nil result/error only on a branch-switch abort path. On
// success it returns (nil, nil) and the outer function registers the
// hookRestore defer (s.hookRestoreFunc) before continuing to execution.
func (r *Runner) executePrepare(s *executeState) (*ExecutionResult, error) {
	task := s.task
	ctx := s.ctx
	executionPath := s.executionPath
	complexity := s.complexity
	timeout := s.timeout

	log := r.log.With(
		slog.String("task_id", task.ID),
		slog.String("backend", r.backend.Name()),
		slog.String("complexity", complexity.String()),
		slog.Duration("timeout", timeout),
	)

	selectedModel := r.resolveSelectedModel(task)
	if selectedModel != "" {
		log = log.With(slog.String("routed_model", selectedModel))
	}

	// Select effort if routing is enabled
	selectedEffort := r.modelRouter.SelectEffort(task)
	if selectedEffort != "" {
		log = log.With(slog.String("routed_effort", selectedEffort))
	}

	log.Info("Starting task execution",
		slog.String("project", task.ProjectPath),
		slog.String("branch", task.Branch),
		slog.Bool("create_pr", task.CreatePR),
	)

	// Emit task started event
	r.emitAlertEvent(AlertEvent{
		Type:      AlertEventTypeTaskStarted,
		TaskID:    task.ID,
		TaskTitle: task.Title,
		Project:   task.ProjectPath,
		Timestamp: time.Now(),
	})

	// Dispatch webhook for task started
	r.dispatchWebhook(ctx, webhooks.EventTaskStarted, webhooks.TaskStartedData{
		TaskID:      task.ID,
		Title:       task.Title,
		Description: task.Description,
		Project:     task.ProjectPath,
		Source:      "pilot",
	})

	// Initialize git operations in execution path (worktree or original)
	git := NewGitOperations(executionPath)

	// Create branch if specified (skip for direct commit mode and worktree mode)
	// When using worktree, CreateWorktreeWithBranch already created the branch
	useWorktree := r.config != nil && r.config.UseWorktree && task.Branch != "" && !task.DirectCommit
	if task.Branch != "" && !task.DirectCommit && !useWorktree {
		r.reportProgress(task.ID, "Branching", 3, "Switching to default branch...")

		// GH-279: Always switch to default branch and pull latest before creating new branch.
		// This prevents new branches from forking off previous pilot branches instead of main.
		// GH-836: Hard fail if we can't switch - continuing from wrong branch causes corrupted PRs.
		// GH-2290: Honor task.BaseBranch (sourced from project.default_branch / branch_from) so
		// `main → dev → feature` workflows branch off dev rather than git's HEAD.
		var defaultBranch string
		var err error
		if task.BaseBranch != "" {
			defaultBranch, err = git.SwitchToBranchAndPull(ctx, task.BaseBranch)
		} else {
			defaultBranch, err = git.SwitchToDefaultBranchAndPull(ctx)
		}
		if err != nil {
			return nil, fmt.Errorf("branch switch failed, aborting execution: failed to switch to default branch: %w", err)
		}
		r.reportProgress(task.ID, "Branching", 5, fmt.Sprintf("On %s, creating %s...", defaultBranch, task.Branch))

		if err := git.CreateBranch(ctx, task.Branch); err != nil {
			// Branch already exists - check if it's stale (GH-912)
			behindCount, behindErr := git.CommitsBehindMain(ctx, task.Branch)
			if behindErr != nil {
				log.Warn("Failed to check if branch is behind main",
					slog.String("branch", task.Branch),
					slog.Any("error", behindErr),
				)
			}

			if behindCount > 0 {
				// Branch is stale - delete and recreate from main
				log.Info("Stale branch detected, recreating from main",
					slog.String("branch", task.Branch),
					slog.Int("commits_behind", behindCount),
				)
				r.reportProgress(task.ID, "Branching", 6, fmt.Sprintf("Stale branch %s (%d behind), recreating...", task.Branch, behindCount))

				if delErr := git.DeleteBranch(ctx, task.Branch); delErr != nil {
					log.Warn("Failed to delete stale branch",
						slog.String("branch", task.Branch),
						slog.Any("error", delErr),
					)
				}
				// Create fresh branch from main
				if createErr := git.CreateBranch(ctx, task.Branch); createErr != nil {
					return nil, fmt.Errorf("failed to recreate branch after stale detection: %w", createErr)
				}
				r.reportProgress(task.ID, "Branching", 8, fmt.Sprintf("Recreated fresh branch %s", task.Branch))
			} else {
				// Branch exists and is not stale - switch to it
				if switchErr := git.SwitchBranch(ctx, task.Branch); switchErr != nil {
					return nil, fmt.Errorf("failed to create/switch branch: %w", err)
				}
				r.reportProgress(task.ID, "Branching", 8, fmt.Sprintf("Switched to existing branch %s", task.Branch))
			}
		} else {
			r.reportProgress(task.ID, "Branching", 8, fmt.Sprintf("Created branch %s", task.Branch))
			r.saveLogEntry(task.ID, "info", "Branch created: "+task.Branch)
		}
	}

	// GH-994: Create task documentation if Navigator is present
	agentPath := filepath.Join(executionPath, ".agent")
	if _, err := os.Stat(agentPath); err == nil {
		if err := CreateTaskDoc(agentPath, task); err != nil {
			log.Warn("Failed to create task doc", slog.Any("error", err))
		}
	}

	// Run parallel research phase for medium/complex tasks (GH-217)
	var researchResult *ResearchResult
	if r.parallelRunner != nil && complexity.ShouldRunResearch() {
		r.reportProgress(task.ID, "Research", 10, "Running parallel research...")
		r.saveLogEntry(task.ID, "info", "Exploring codebase...")
		var researchErr error
		researchResult, researchErr = r.parallelRunner.ExecuteResearchPhase(ctx, task)
		if researchErr != nil {
			log.Warn("Research phase failed, continuing without research context",
				slog.String("task_id", task.ID),
				slog.Any("error", researchErr),
			)
		} else if researchResult != nil && len(researchResult.Findings) > 0 {
			log.Info("Research phase completed",
				slog.String("task_id", task.ID),
				slog.Int("findings", len(researchResult.Findings)),
				slog.Duration("duration", researchResult.Duration),
				slog.Int64("tokens", researchResult.TotalTokens),
			)
		}
	}

	// Load per-repo workflow override (.pilot/workflow.yaml) — TASK-304.
	// Applied before prompt construction so overrides are visible immediately.
	var workflowMaxTurns int
	repoWorkflow, wfErr := workflow.Load(executionPath)
	if wfErr != nil {
		log.Warn("Failed to load .pilot/workflow.yaml, using defaults",
			slog.String("task_id", task.ID),
			slog.Any("error", wfErr),
		)
	} else if repoWorkflow != nil {
		log.Info("Loaded .pilot/workflow.yaml",
			slog.String("task_id", task.ID),
			slog.Int("max_turns", repoWorkflow.Agent.MaxTurns),
			slog.String("reasoning_effort", repoWorkflow.Agent.ReasoningEffort),
		)
		if repoWorkflow.Agent.ReasoningEffort != "" {
			selectedEffort = repoWorkflow.Agent.ReasoningEffort
		}
		workflowMaxTurns = repoWorkflow.Agent.MaxTurns
	}

	// TASK-305: fire after_create and wire before_remove now that the workflow is loaded.
	var hookEnv []string
	if repoWorkflow != nil {
		hookEnv = append(os.Environ(), "PILOT_TASK_ID="+task.ID)
		runWorkflowHook(ctx, "after_create", repoWorkflow.Hooks.AfterCreate, executionPath, hookEnv, log)
		if len(repoWorkflow.Hooks.BeforeRemove) > 0 {
			scripts := repoWorkflow.Hooks.BeforeRemove
			env := hookEnv
			s.beforeRemoveHookFn = func() {
				runWorkflowHook(context.Background(), "before_remove", scripts, executionPath, env, log)
			}
		}
	}

	// Build the prompt
	prompt := r.BuildPrompt(task, executionPath)

	// Append per-repo workflow prompt appendix if present (TASK-304).
	if repoWorkflow != nil && repoWorkflow.PromptAppendix != "" {
		prompt += "\n\n## Project Workflow\n\n" + repoWorkflow.PromptAppendix
	}

	// Inject the opt-in pipeline plan spec, if the plan stage produced one. This
	// appends AFTER BuildPrompt/.agent priming so the spec augments — not
	// replaces — the executor's normal context.
	prompt = injectPlanOutput(prompt, s.planOutput)

	// Append research context if available (GH-217)
	if researchResult != nil && len(researchResult.Findings) > 0 {
		prompt = r.appendResearchContext(prompt, researchResult)
	}

	// State for tracking progress
	state := &progressState{phase: "Starting", budgetCancel: s.cancel}

	// Initialize recorder if recording is enabled
	var recorder *replay.Recorder
	if r.enableRecording {
		var recErr error
		recorder, recErr = replay.NewRecorder(task.ID, task.ProjectPath, r.getRecordingsPath())
		if recErr != nil {
			log.Warn("Failed to create recorder, continuing without recording", slog.Any("error", recErr))
		} else {
			recorder.SetBranch(task.Branch)
			log.Debug("Recording enabled", slog.String("recording_id", recorder.GetRecordingID()))
		}
	}

	// Report start
	backendName := r.backend.Name()
	r.reportProgress(task.ID, "Starting", 0, fmt.Sprintf("Initializing %s...", backendName))

	// Clean stale pilot hooks unconditionally — even when hooks.enabled is false.
	// Prevents dead entries from accumulating after OS reboots clear temp dirs (GH-1749).
	// Clean project root first (always), then worktree path if different (GH-1884).
	projectSettingsPath := filepath.Join(task.ProjectPath, ".claude", "settings.json")
	if cleanErr := CleanStalePilotHooks(projectSettingsPath); cleanErr != nil {
		log.Warn("Failed to clean stale pilot hooks in project root", slog.Any("error", cleanErr))
	}
	if executionPath != task.ProjectPath {
		worktreeSettingsPath := filepath.Join(executionPath, ".claude", "settings.json")
		if cleanErr := CleanStalePilotHooks(worktreeSettingsPath); cleanErr != nil {
			log.Warn("Failed to clean stale pilot hooks in worktree", slog.Any("error", cleanErr))
		}
	}

	// Setup Claude Code hooks if enabled (GH-1266)
	var hookRestoreFunc func() error
	if r.config != nil && r.config.Hooks != nil && r.config.Hooks.Enabled {
		log.Debug("Setting up Claude Code hooks", slog.String("task_id", task.ID))

		// Create temporary directory for hook scripts
		scriptDir, err := os.MkdirTemp("", "pilot-hooks-")
		if err != nil {
			log.Error("Failed to create hooks script directory", slog.Any("error", err))
		} else {
			// Write embedded scripts
			if err := WriteEmbeddedScripts(scriptDir); err != nil {
				log.Error("Failed to write embedded hook scripts", slog.Any("error", err))
				if rmErr := os.RemoveAll(scriptDir); rmErr != nil {
					log.Warn("Failed to clean up hook scripts after write error", slog.Any("error", rmErr))
				}
			} else {
				// Generate Claude settings
				hookSettings := GenerateClaudeSettings(r.config.Hooks, scriptDir)

				// Merge with existing settings.json (worktree-safe path)
				settingsPath := filepath.Join(executionPath, ".claude", "settings.json")
				_, mergeErr := MergeWithExisting(settingsPath, hookSettings)
				if mergeErr != nil {
					log.Error("Failed to setup Claude hooks", slog.Any("error", mergeErr))
					// Clean up script directory
					if rmErr := os.RemoveAll(scriptDir); rmErr != nil {
						log.Warn("Failed to clean up hook scripts after merge error", slog.Any("error", rmErr))
					}
				} else {
					hookRestoreFunc = func() error {
						// Instead of blind restoreFunc() (which may write back stale entries
						// from a previous crash), use targeted cleanup (GH-1884).
						if cleanErr := CleanStalePilotHooks(settingsPath); cleanErr != nil {
							log.Warn("Failed to clean pilot hooks from settings", slog.Any("error", cleanErr))
						}
						// Clean up script directory
						if rmErr := os.RemoveAll(scriptDir); rmErr != nil {
							log.Warn("Failed to clean up hook scripts", slog.Any("error", rmErr))
						}
						return nil
					}
					log.Debug("Claude Code hooks configured",
						slog.String("settings_path", settingsPath),
						slog.String("script_dir", scriptDir))
				}
			}
		}
	}

	s.log = log
	s.selectedModel = selectedModel
	s.selectedEffort = selectedEffort
	s.git = git
	s.agentPath = agentPath
	s.researchResult = researchResult
	s.workflowMaxTurns = workflowMaxTurns
	s.repoWorkflow = repoWorkflow
	s.hookEnv = hookEnv
	s.prompt = prompt
	s.state = state
	s.recorder = recorder
	s.hookRestoreFunc = hookRestoreFunc

	return nil, nil
}
