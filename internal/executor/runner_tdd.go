package executor

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// runTDDSequence drives the opt-in TDD role pipeline when config.TDD.Enabled is
// set. It runs ARCHITECT -> TEST-AUTHOR -> [RED gate] -> IMPLEMENTER -> [GREEN
// gate] and returns the IMPLEMENTER's BackendResult so the EXISTING finalize tail
// (executeCopyResult + executeFinalize: QA/review/PR) runs unchanged.
//
// It returns a non-nil error to ABORT the run when a gate cannot be satisfied:
//   - RED not red after a bounded TEST-AUTHOR retry  -> reasonTDDRedGateNotRed
//   - GREEN still failing after green_max_retries     -> reasonTDDGreenGateFailed
//   - GREEN passes but IMPLEMENTER never committed     -> reasonTDDImplementerNoCommit
//
// The caller (Execute) wires the returned (result, err) into the same path it
// uses for r.execBackend.Execute, so failures flow through the normal failure
// handling and successes through the normal finalize tail.
func (r *Runner) runTDDSequence(s *executeState) (*BackendResult, error) {
	task := s.task
	ctx := s.ctx
	log := s.log

	scopeToNew := true
	if r.config != nil && r.config.TDD != nil && r.config.TDD.ScopeRedToNew != nil {
		scopeToNew = *r.config.TDD.ScopeRedToNew
	}
	roleMax, greenMax := r.tddRetryBudgets()

	base := r.tddBasePrompt(s)

	// 1) ARCHITECT — read-only design. Advisory: failure is non-fatal, the design
	// just augments the later prompts when present.
	r.reportProgress(task.ID, "TDD Architect", 12, "Designing change (read-only)...")
	// Capture HEAD before the architect runs: it is design-only and must not
	// commit. A misbehaving non-claude architect that commits is reverted before
	// TEST-AUTHOR runs so its stray commits never enter the RED/GREEN diff.
	archHeadBefore := r.readOnlyHeadBefore(s)
	archRes, archErr := r.runTDDRole(s, r.architectBackend, r.tddRoleStage("architect"), buildTDDRolePrompt(base, buildArchitectAppendix()), true)
	if guard := enforceReadOnly(ctx, s.git, archHeadBefore, pilotapi.RoleArchitect, log); guard.Violated {
		s.tddArchitectReadOnlyViolation = true
	}
	if archErr != nil {
		log.Warn("TDD architect role failed; continuing without design", slog.Any("error", archErr))
	} else if archRes != nil {
		s.tddArchitectDesign = archRes.Output
	}
	// Record the architect handoff as the chain root (ParentHash == ""). The
	// design may be empty (role failed/produced nothing); the typed record still
	// anchors the lineage so test-author and implementer chain off a stable hash.
	r.recordTDDArtifact(s, pilotapi.RoleArchitect, s.tddArchitectDesign)

	// 1.5) BASELINE-GREEN precondition — the suite MUST be green BEFORE the
	// TEST-AUTHOR writes anything, so a later RED is attributable to the new tests
	// and not to a pre-existing failing suite. A RED baseline ABORTS the run.
	r.reportProgress(task.ID, "TDD Baseline", 18, "Verifying suite is green before tests...")
	if err := r.enforceTDDBaselineGreen(ctx, task.ID, s.executionPath); err != nil {
		return nil, err
	}

	// 2) TEST-AUTHOR — write FAILING tests and commit. Re-prompt once if no commit
	// landed (an empty working tree cannot drive the RED gate).
	if err := r.runTDDTestAuthor(s, base, roleMax); err != nil {
		return nil, err
	}

	// 3) RED gate — the authored tests MUST fail. Aborts (no fall-through) if not.
	r.reportProgress(task.ID, "TDD Red Gate", 40, "Verifying tests fail (RED)...")
	if err := r.enforceTDDRedGate(ctx, task.ID, s.executionPath, s.tddTestNames, scopeToNew, roleMax,
		func(ctx context.Context, feedback string) error {
			return r.rerunTDDTestAuthor(s, base, feedback)
		}); err != nil {
		return nil, err
	}
	// Record the test-author handoff once the RED gate is satisfied (single entry
	// regardless of re-author retries), chained to the architect via ParentHash.
	// Content captures the authored test names so the lineage is meaningful.
	r.recordTDDArtifact(s, pilotapi.RoleTestAuthor, tddTestAuthorArtifactContent(s.tddTestNames))

	// 3.5) TEST-FREEZE snapshot — capture the content hashes of the test files the
	// TEST-AUTHOR committed. After the IMPLEMENTER runs we verify these are
	// UNCHANGED so the implementer cannot weaken or delete the red tests to "pass".
	freeze, err := r.snapshotTDDTestFreeze(s)
	if err != nil {
		log.Warn("TDD test-freeze snapshot failed; freeze guard disabled for this run", slog.Any("error", err))
	}

	// 4) IMPLEMENTER — make the failing tests pass and commit. Capture the
	// test-author baseline commit count BEFORE the implementer runs so the
	// post-GREEN commit guard can prove the implementer landed its own commit.
	implBaseline, err := r.tddCommitCount(s)
	if err != nil {
		return nil, err
	}
	r.reportProgress(task.ID, "TDD Implementer", 50, "Implementing to pass tests...")
	implRes, err := r.runTDDImplementer(s, base, "")
	if err != nil {
		return nil, err
	}

	// 4.5) TEST-FREEZE verify — the IMPLEMENTER must not have touched the frozen
	// red tests. A modification/deletion ABORTS with reasonTDDTestsModified.
	if ferr := verifyTestFreeze(freeze, s.executionPath); ferr != nil {
		return nil, ferr
	}

	// 5) GREEN gate — same tests MUST pass; loop IMPLEMENTER with gate feedback.
	r.reportProgress(task.ID, "TDD Green Gate", 70, "Verifying tests pass (GREEN)...")
	if err := r.enforceTDDGreenGate(ctx, task.ID, s.executionPath, s.tddTestNames, scopeToNew, greenMax,
		func(ctx context.Context, feedback string) error {
			res, rerunErr := r.runTDDImplementer(s, base, feedback)
			if rerunErr != nil {
				return rerunErr
			}
			implRes = res
			return nil
		}); err != nil {
		return nil, err
	}

	// 5.5) IMPLEMENTER-COMMIT gate — the GREEN gate judges the WORKING TREE, so an
	// implementer that made tests pass without committing would yield a PR carrying
	// only the test-author commit. Require a fresh implementer commit beyond the
	// baseline; re-prompt to COMMIT (bounded by greenMax) and FAIL the run with
	// reasonTDDImplementerNoCommit rather than silently finalizing a tests-only PR.
	if err := r.enforceTDDImplementerCommit(ctx, task.ID, s.executionPath, s.tddTestNames, scopeToNew, implBaseline, greenMax,
		func(ctx context.Context) (int, error) { return r.tddCommitCount(s) },
		func(ctx context.Context, feedback string) error {
			res, rerunErr := r.runTDDImplementer(s, base, feedback)
			if rerunErr != nil {
				return rerunErr
			}
			implRes = res
			return nil
		}); err != nil {
		return nil, err
	}
	// Record the implementer handoff once the GREEN gate passes (single entry
	// regardless of green retries), chained to the test-author via ParentHash.
	r.recordTDDArtifact(s, pilotapi.RoleImplementer, tddImplementerArtifactContent(implRes))

	// Return the IMPLEMENTER result so the existing finalize tail (QA/PR) runs.
	return implRes, nil
}

// tddTestAuthorArtifactContent renders the authored test names as the
// test-author artifact's content. Empty names yield an empty string, so the
// artifact still records the role and chains correctly even when no TESTS_ADDED
// list was emitted.
func tddTestAuthorArtifactContent(names []string) string {
	return strings.Join(names, "\n")
}

// tddImplementerArtifactContent renders the implementer artifact's content from
// the final IMPLEMENTER result. A nil result yields an empty string so the
// artifact still records the role and closes the chain.
func tddImplementerArtifactContent(res *BackendResult) string {
	if res == nil {
		return ""
	}
	return res.Output
}

// tddRetryBudgets resolves the per-role and GREEN retry budgets from config,
// falling back to the package defaults.
func (r *Runner) tddRetryBudgets() (roleMax, greenMax int) {
	roleMax = defaultTDDRoleMaxRetries
	greenMax = defaultTDDGreenMaxRetries
	if r.config == nil || r.config.TDD == nil {
		return roleMax, greenMax
	}
	if r.config.TDD.RoleMaxRetries != nil {
		roleMax = *r.config.TDD.RoleMaxRetries
	}
	if r.config.TDD.GreenMaxRetries != nil {
		greenMax = *r.config.TDD.GreenMaxRetries
	}
	return roleMax, greenMax
}

// tddBasePrompt builds the shared base prompt for every TDD role: the run's
// execute prompt (BuildPrompt + plan injection, already on s.prompt) plus an
// explicit BuildGuidancePreamble so role backends that bypass BuildPrompt (e.g.
// codex-app-server) still receive .agent priming.
func (r *Runner) tddBasePrompt(s *executeState) string {
	base := s.prompt
	agentDir := s.agentPath
	if agentDir == "" {
		agentDir = filepath.Join(s.executionPath, ".agent")
	}
	if preamble := BuildGuidancePreamble(agentDir, s.task.Description); preamble != "" {
		base = preamble + "\n\n" + base
	}
	return base
}

// tddRoleStage returns the StageConfig for a TDD role so its per-stage
// model/effort override can be threaded into the role's Execute call. A nil TDD
// config (or nil role stage) yields nil, falling back to the run-level model.
func (r *Runner) tddRoleStage(role string) *StageConfig {
	if r.config == nil || r.config.TDD == nil {
		return nil
	}
	switch role {
	case "architect":
		return r.config.TDD.Architect
	case "test_author":
		return r.config.TDD.TestAuthor
	case "implementer":
		return r.config.TDD.Implementer
	case "qa":
		return r.config.TDD.QA
	default:
		return nil
	}
}

// runTDDRole invokes a single role backend with the shared ExecuteOptions wiring
// (model/effort/max-turns, recorder + progress event handler) mirrored from the
// main execute call, and returns its BackendResult. stage carries the role's
// per-stage model/effort override (nil => run-level), threaded so a role model
// override is not shadowed by the run-level selection (backends prefer opts).
//
// readOnly scopes the role's toolbox: the ARCHITECT is design-only, so it gets
// the read-only planning tools (DefaultAllowedToolsPlanning: Read/Grep/Glob) and
// CANNOT write files, mirroring the pipeline plan stage. The TEST-AUTHOR and
// IMPLEMENTER must write/commit, so they keep the normal execution toolset. This
// composes with the H3 enforceReadOnly guard (which reverts stray architect
// commits) for defense in depth.
func (r *Runner) runTDDRole(s *executeState, backend Backend, stage *StageConfig, prompt string, readOnly bool) (*BackendResult, error) {
	task := s.task
	allowedTools, mcpConfigPath := r.executionToolOptions()
	if readOnly {
		allowedTools = DefaultAllowedToolsPlanning()
	}
	effModel, effEffort := effectiveStageModelEffort(stage, s.selectedModel, s.selectedEffort)
	return backend.Execute(s.ctx, ExecuteOptions{
		Prompt:        prompt,
		ProjectPath:   s.executionPath,
		Verbose:       task.Verbose,
		Model:         effModel,
		Effort:        effEffort,
		MaxTurns:      s.workflowMaxTurns,
		AllowedTools:  allowedTools,
		MCPConfigPath: mcpConfigPath,
		EventHandler: func(event BackendEvent) {
			if s.recorder != nil {
				if recErr := s.recorder.RecordEvent(event.Raw); recErr != nil {
					s.log.Warn("Failed to record TDD event", slog.Any("error", recErr))
				}
			}
			r.processBackendEvent(task.ID, event, s.state)
		},
	})
}

// runTDDTestAuthor runs the TEST-AUTHOR role, requiring a fresh commit and a
// TESTS_ADDED list. If no commit landed it re-prompts up to roleMax times; an
// empty working tree cannot drive the RED gate.
func (r *Runner) runTDDTestAuthor(s *executeState, base string, roleMax int) error {
	if roleMax < 0 {
		roleMax = defaultTDDRoleMaxRetries
	}
	for attempt := 0; ; attempt++ {
		r.reportProgress(s.task.ID, "TDD Test Author", 25, "Writing failing tests...")
		committed, err := r.runTDDTestAuthorOnce(s, base, "")
		if err != nil {
			return err
		}
		if committed {
			if len(s.tddTestNames) == 0 {
				s.log.Warn("TDD test-author committed but emitted no TESTS_ADDED; RED/GREEN gates fall back to the whole suite",
					slog.String("task_id", s.task.ID))
			}
			return nil
		}
		if attempt >= roleMax {
			return fmt.Errorf("tdd_test_author_no_commit: TEST-AUTHOR produced no commit after %d retr%s", roleMax, plural(roleMax))
		}
		s.log.Info("TDD test-author produced no commit; re-prompting",
			slog.String("task_id", s.task.ID), slog.Int("attempt", attempt+1))
	}
}

// rerunTDDTestAuthor re-invokes the TEST-AUTHOR with the RED gate's feedback when
// the authored tests passed before any implementation. It is the rerun hook
// passed to enforceTDDRedGate.
func (r *Runner) rerunTDDTestAuthor(s *executeState, base, feedback string) error {
	r.reportProgress(s.task.ID, "TDD Test Author", 30, "Re-authoring failing tests...")
	_, err := r.runTDDTestAuthorOnce(s, base, feedback)
	return err
}

// runTDDTestAuthorOnce runs one TEST-AUTHOR invocation, parses TESTS_ADDED into
// s.tddTestNames, and reports whether a new commit landed (via CountNewCommits
// against the resolved base branch).
func (r *Runner) runTDDTestAuthorOnce(s *executeState, base, feedback string) (committed bool, err error) {
	before, err := r.tddCommitCount(s)
	if err != nil {
		return false, err
	}
	appendix := buildTestAuthorAppendix(s.tddArchitectDesign)
	if feedback != "" {
		appendix += "\n\n## RED gate feedback (fix this)\n\n" + feedback
	}
	res, err := r.runTDDRole(s, r.testAuthorBackend, r.tddRoleStage("test_author"), buildTDDRolePrompt(base, appendix), false)
	if err != nil {
		return false, err
	}
	if res != nil {
		if names := extractTestsAdded(res.Output); len(names) > 0 {
			s.tddTestNames = names
		}
	}
	after, err := r.tddCommitCount(s)
	if err != nil {
		return false, err
	}
	return after > before, nil
}

// runTDDImplementer runs one IMPLEMENTER invocation (with optional GREEN gate
// feedback) and requires a fresh commit.
func (r *Runner) runTDDImplementer(s *executeState, base, feedback string) (*BackendResult, error) {
	before, err := r.tddCommitCount(s)
	if err != nil {
		return nil, err
	}
	appendix := buildImplementerAppendix(s.tddArchitectDesign, s.tddTestNames, feedback)
	res, err := r.runTDDRole(s, r.implementerBackend, r.tddRoleStage("implementer"), buildTDDRolePrompt(base, appendix), false)
	if err != nil {
		return nil, err
	}
	after, err := r.tddCommitCount(s)
	if err != nil {
		return res, err
	}
	if after <= before {
		// Not fatal here: the GREEN gate still judges the working tree, and the
		// post-GREEN enforceTDDImplementerCommit guard re-prompts for a commit and
		// fails the run with reasonTDDImplementerNoCommit if none ever lands.
		s.log.Warn("TDD implementer produced no new commit; commit guard will require one after GREEN",
			slog.String("task_id", s.task.ID))
	}
	return res, nil
}

// tddCommitCount counts commits on the current branch relative to the resolved
// base branch (task.BaseBranch -> git default -> "main"), used to verify a role
// committed. A missing base branch yields 0 (CountNewCommits contract).
func (r *Runner) tddCommitCount(s *executeState) (int, error) {
	if s.git == nil {
		return 0, fmt.Errorf("tdd commit count: git operations not initialized")
	}
	return s.git.CountNewCommits(s.ctx, r.tddBaseBranch(s))
}

// tddBaseBranch resolves the base branch the TDD gates diff against:
// task.BaseBranch -> git default branch -> "main".
func (r *Runner) tddBaseBranch(s *executeState) string {
	if s.task.BaseBranch != "" {
		return s.task.BaseBranch
	}
	if s.git != nil {
		if def, derr := s.git.GetDefaultBranch(s.ctx); derr == nil && def != "" {
			return def
		}
	}
	return "main"
}

// snapshotTDDTestFreeze captures the content hashes of the test files committed
// by the TEST-AUTHOR (the `_test.go` files changed on this branch vs the base)
// so the post-IMPLEMENTER verify can detect any weakening/deletion. It is
// best-effort: a nil snapshot (e.g. no test-file diff, or git error already
// returned) simply disables the freeze guard for the run.
func (r *Runner) snapshotTDDTestFreeze(s *executeState) (*testFreezeSnapshot, error) {
	if s.git == nil {
		return nil, fmt.Errorf("tdd test-freeze: git operations not initialized")
	}
	return snapshotTestFiles(s.ctx, s.executionPath, r.tddBaseBranch(s))
}
