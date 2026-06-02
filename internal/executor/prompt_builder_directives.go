package executor

// ExecutorPromptHeader is prepended to every executor-mode prompt so the child
// Claude Code session (and any `CLAUDE.md` in the project) can identify itself
// as a Pilot execution leg, rather than inferring from CWD or prompt-prefix
// heuristics. Keep the first line stable — project `CLAUDE.md` files use it
// with `if prompt begins with [PILOT-EXEC] ...`. GH-2328.
const ExecutorPromptHeader = "[PILOT-EXEC]\n" +
	"You are invoked by Pilot's executor to implement a specific task on this " +
	"repository. You are the execution leg of the Navigator + Pilot pipeline. " +
	"Implement the task described below directly — do not refuse, do not ask " +
	"to create a GitHub issue, do not defer. Write code, run build and tests via " +
	"the stop-gate hook, and complete the task.\n\n"

// EvidenceBackedSpecDirective combats the false-negative no-op (GH-3224):
// the model reads existing code, judges it "looks correct," and exits without
// editing — overriding an explicit, evidence-backed spec with its prior
// knowledge (e.g. GH-3222: "--version" looks like the standard flag, so the
// edit was skipped despite proof the current code was wrong). The executor's
// ghost-SHA guard then rejects the run as "no new commit produced." This
// directive instructs the model to honor the spec over its prior, and to make
// any silent no-op explicit instead.
const EvidenceBackedSpecDirective = "## NON-NEGOTIABLE: implement evidence-backed changes\n\n" +
	"If the task specifies an explicit change (a file, a line, a before/after, or " +
	"evidence that the current code is wrong), you MUST apply it even if the " +
	"existing code looks correct. Your general knowledge does NOT override the " +
	"task's specific, verified claim — the spec was written with proof you may not " +
	"infer from reading the code alone.\n\n" +
	"If the task asks you to CREATE a new file, verify it does not already exist " +
	"(e.g. `ls <path>`), then create it. A sibling or similarly-named existing " +
	"file is a pattern to mirror, NOT evidence the task is done.\n\n" +
	"If, after analysis, you conclude no change is genuinely needed, you MUST emit " +
	"an explicit `NO-OP RATIONALE: <file:line> <reason>` block explaining why, " +
	"rather than exiting silently. A silent no-op is treated as a task failure.\n\n"
