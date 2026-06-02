package executor

import (
	"log/slog"
	"path/filepath"
)

// executePipelinePlan runs the opt-in pipeline PLAN stage. Unlike epic planning
// (gated on complexity.IsEpic()), this fires UNCONDITIONALLY for every task when
// config.Pipeline.Plan is configured — that is the strict cost gate: lifting
// planning to every task adds an LLM round-trip, so it only happens when the
// operator explicitly opts in via the pipeline.plan stage.
//
// v1 reuses PlanEpic's existing `claude --print` subprocess approach,
// parameterized by pipeline.plan.model (falling back to the planning default).
// Routing this through the Backend interface is a deliberate future v2.
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
			prompt := buildPlanningPrompt(task, agentDir)
			model := r.resolvePlanningModel(r.config.Pipeline.Plan.Model)
			return r.runPlanningSubprocess(s.ctx, task, prompt, s.executionPath, model)
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
	r.log.Info("Pipeline plan stage produced spec",
		slog.String("task_id", task.ID),
		slog.Int("spec_bytes", len(output)),
	)
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
