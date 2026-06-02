package executor

import (
	"log/slog"
	"path/filepath"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// executePipelinePlan runs the opt-in pipeline PLAN stage. Unlike epic planning
// (gated on complexity.IsEpic()), this fires UNCONDITIONALLY for every task when
// config.Pipeline.Plan is configured — that is the strict cost gate: lifting
// planning to every task adds an LLM round-trip, so it only happens when the
// operator explicitly opts in via the pipeline.plan stage.
//
// The spec is produced by the CONFIGURED plan backend (r.planBackend, resolved
// by resolveStageBackends from config.Pipeline.Plan) via Backend.Execute, so any
// backend can plan (claude→codex AND codex→claude) — the goal-critical any-to-any
// path. This is distinct from PlanEpic, which keeps its dedicated `claude --print`
// subprocess for epic decomposition.
//
// On success it stores the raw spec on s.planOutput; executePrepare injects it
// into the execute prompt as an "## Implementation Plan" section. Planning
// failure is non-fatal: it is logged and execution proceeds without a spec.
func (r *Runner) executePipelinePlan(s *executeState) {
	if r.config == nil || r.config.Pipeline == nil || r.config.Pipeline.Plan == nil {
		return
	}

	task := s.task
	r.reportProgress(task.ID, "Planning", 10, "Running pipeline plan stage...")

	planFn := r.planPipelineFn
	if planFn == nil {
		planFn = func() (string, error) {
			agentDir := filepath.Join(s.executionPath, ".agent")
			prompt := buildPipelinePlanPrompt(task, agentDir)
			// AllowedTools is honored by claude-code (read-only enforcement);
			// other backends ignore it, so the prompt itself forbids edits/commits.
			result, err := r.planBackend.Execute(s.ctx, ExecuteOptions{
				Prompt:       prompt,
				ProjectPath:  s.executionPath,
				Model:        r.config.Pipeline.Plan.Model,
				Effort:       r.config.Pipeline.Plan.Effort,
				AllowedTools: DefaultAllowedToolsPlanning(),
			})
			if err != nil {
				return "", err
			}
			return result.Output, nil
		}
	}

	output, err := planFn()
	if err != nil {
		r.log.Warn("Pipeline plan stage failed, executing without a spec",
			slog.String("task_id", task.ID),
			slog.Any("error", err),
		)
		return
	}

	s.planOutput = output

	// Build the typed, traceable record alongside the prose injection. The prose
	// "## Implementation Plan" section (injectPlanOutput) is what backends
	// consume; this artifact is the versioned, content-addressable audit record.
	// The plan stage is the chain root, so its ParentHash is empty.
	s.planArtifact = pilotapi.NewHandoffArtifact(pilotapi.RolePlan, task.ID, output, "")

	r.log.Info("Pipeline plan stage produced spec",
		slog.String("task_id", task.ID),
		slog.Int("spec_bytes", len(output)),
	)
	r.log.Debug("Pipeline plan handoff artifact",
		slog.String("task_id", task.ID),
		slog.String("role", s.planArtifact.Role),
		slog.String("trace_hash", s.planArtifact.TraceHash),
	)
}

// buildPipelinePlanPrompt builds the prompt for the opt-in pipeline plan stage.
// It reuses buildPlanningPrompt's task framing and project priming, but prepends
// an explicit read-only contract: the spec backend MUST design only and must not
// modify files or commit. AllowedTools enforces this for claude-code; every other
// backend ignores tool restrictions, so the instruction is the only guardrail
// there — that is why it lives in the prompt text, not just in ExecuteOptions.
func buildPipelinePlanPrompt(task *Task, agentDir string) string {
	const readOnlyContract = "IMPORTANT: Produce an implementation plan / spec ONLY. " +
		"Do NOT write or modify any files, do NOT run commands that change state, " +
		"and do NOT commit. Output the plan as text only.\n\n"
	return readOnlyContract + buildPlanningPrompt(task, agentDir)
}

// injectPlanOutput appends the pipeline plan spec to the execute prompt as an
// "## Implementation Plan" section. An empty spec leaves the prompt untouched,
// preserving today's behavior when no plan stage is configured.
func injectPlanOutput(prompt, planOutput string) string {
	if planOutput == "" {
		return prompt
	}
	return prompt + "\n\n## Implementation Plan\n\n" + planOutput
}
