# Pilot — Master Worklist

_Üretildi 2026-06-03 · kaynak: pilot-architecture.html (14-subsystem deep analysis + doğrulanmış test run + kaynak-onaylı bulgular)._

Tek doküman, fazlı. Sırayla in. **Bir bölüm = bir PR.** Hepsini tek PR'da yapma.

---

## Agent kuralları (her faz için geçerli)

- Repo: `/Volumes/doksanbir/repos/pilot`. **Kendi worktree'inde** çalış — repo root'ta `git checkout`/`switch` YOK.
- Branch'i `origin/main`'den (taze) aç. Konu başına ayrı branch + ayrı PR.
- **Scope: surgical.** Sadece o maddeyi yap. 'While we're here' temizlik, rename, refactor YOK.
- Değişiklik başına test (table-driven, repodaki stile uy). Token'lar için `internal/testutil`.
- Bitirmeden: `go build ./...` + ilgili paketlerde `go test ... -race` **yeşil**. `make test` -race ile koşar.
- Conventional Commits (İngilizce). **AI imzası/footer YOK** (Co-Authored-By, Generated with… yok).
- Madde bitince bu dosyada `[ ]` → `[x]` yap.

### Yeniden kullanılabilir prompt
```
Repo /Volumes/doksanbir/repos/pilot, kendi worktree'inde (root'ta checkout yok).
Branch <type>/<slug>. pilot-worklist.md'yi aç, SADECE şu bölümü yap: <BÖLÜM>.
Kapsam dışına çıkma. Değişiklik başına test. go build + go test <paketler> -race yeşil olmadan bitirme.
Conventional Commits, AI footer yok. Biten maddeyi [x] işaretle. Tek bölüm = tek PR.
```

### Öncelik sırası

| # | Faz | İçerik | Aciliyet |
|---|-----|--------|----------|
| 1 | Doğrulanmış bug/güvenlik | 4 madde (1 XSS) | **Şimdi** |
| 2 | Kırık test | 1 brittle test | Düşük (prod sağlam) |
| 3 | Eklenecek testler | 8 repo-wide + 92 subsystem | Yüksek (riskli yollar testsiz) |
| 4 | Refactor | 6 headline + 84 subsystem | Orta/Düşük (bug'lar hariç) |
| — | Dead-code | sil, test yazma | Ayrı ele al |

---

## DURUM (2026-06-03 oturumu · branch `fix/verified-findings`, worktree `.claude/worktrees/worklist`, `dev`'den ayrıldı)

**50 commit · `go build ./...` + `go test ./... -race` YEŞİL (44 ok, 0 FAIL/panic/race). Dev'e merge'e hazır.** Asıl kayıt: `git log dev..HEAD`.

**Yapıldı:** Faz 1 (4/4), Faz 2 (1/1), Faz 3 (~77/100: 62 yeni test + 15 zaten-kapalı), Faz 4a (correctness bug'ları + dead-code), Faz 4b (orta refactor'lar), Faz 4c (mimari: Poller/Server/Model god-struct split, teamAdapter DI, migrate+teams versiyonlama, handleMerging split, Metrics.RestoreFromRow, linear/jira typed parse, plane per-project cache, telegram dead-state).

**KALAN — başka session'a handoff:**

- **Mega-refactor (saf yapısal · 0 davranış değişimi · düşük aciliyet · AYRI PR önerilir):**
  - [ ] Runner god struct split (`runner_construct.go`) — 120 alan, 95 atama, 3 ctor, 174 test dosyası
  - [ ] executeState carrier split (`runner_execute_state.go`) — 1538 erişim noktası
  - [ ] BasePoller[T] / 7x ProcessedStore+IssueResult+dispatch dedup (`registry.go` + 7 adapter) — exported API, cross-package
  - [ ] dual pattern store birleştirme (`extractor_save.go`) — JSON vs SQLite uyumsuz şema
  - [ ] Prometheus SDK rewrite (`prometheus.go`) — byte-identical scrape çıktısı imkansız
- **PREMISE YANLIŞ — YAPMA (worklist bulgusu hatalı; bu oturumda doğrulandı):**
  - QualityOutcome/QualityGateDetail "import cycle" → **cycle YOK** (`internal/quality`, `internal/executor`'ı import etmiyor); mirror'lar redundant ama taşımanın değeri yok.
  - chat "triplicated" formatter helper'ları → 3 impl **kasıtlı farklı**; comms'a geçmek assert'leri kırıyor + davranış değiştiriyor.
  - github "duplicated issue-filter loop" (filterCandidates) → **3 gerçek davranış farkı** (recordSkip metrics, markProcessed-on-Done, pendingDeps-in-loop); birleştirmek davranışı değiştirir.
- **Yapılabilir ama yapılmadı (next session isterse):** architect ScanOptions→core+MemoryOptions split; architect IssueRef return (emit.go github import'unu muhtemelen KALDIRMAZ — önce doğrula); config.go god-dep factory registration; executor `goto` runner.go:262 helper extraction, resolveComplexity double-call, error-classification structured; chat handler_fastpath FS abstraction; shell hook dedup (pilot-stop-gate.sh / pilot-bash-guard.sh).

---

## Faz 1 — Doğrulanmış bug & güvenlik (ŞİMDİ · tek PR `fix/verified-findings`)

> Bu 4'ü bu oturumda kaynaktan satır satır doğruladım. Her düzeltmeye test ekle.

### 1. [HIGH/Security] Stored-XSS / HTML injection — alert email
- [x] **`internal/alerts/channels_email.go:99-100`**
  - **Sorun:** alert.Title ve alert.Message html.EscapeString olmadan HTML email'e yazılıyor. Ticket başlığı dış kaynaktan (GitHub/Linear/Jira/Slack) geldiği için saldırgan `<script>…` başlıklı issue ile tüm alert alıcılarına canlı HTML gönderir.
  - **Fix:** İkisini de html.EscapeString ile sarmala (2 satır).

### 2. [HIGH/Correctness] Research subagent --model flag yok sayılıyor
- [x] **`internal/executor/parallel.go:262`**
  - **Sorun:** modelFlag tek string "--model haiku" ve tek argv elemanı olarak ekleniyor: args = append([]string{modelFlag}, args...). claude CLI bunu flag+değer olarak parse etmez → her research subagent default modelde koşar (sessiz maliyet/latency).
  - **Fix:** İki ayrı arg: "--model", model. modelFlag'i []string yap.

### 3. [HIGH/Security] Crypto olmayan, çakışabilir webhook event ID
- [x] **`internal/webhooks/event.go:108-114`**
  - **Sorun:** randomString() ID'yi charset[time.Now().UnixNano()%len] + her byte arası time.Sleep(1ns) ile üretiyor. Yorum bile 'in production, use crypto/rand' diyor. ID'ler tahmin edilebilir; eşzamanlı iki çağrı çakışabilir; 16 sleep/ID gereksiz yavaş.
  - **Fix:** crypto/rand.Read ile doldur (3 satır, API değişmez).

### 4. [MEDIUM/Correctness] Budget yüzdesi /0 → NaN/Inf
- [x] **`internal/budget/enforcer.go:156,159`**
  - **Sorun:** GetStatus (TotalCost / config.DailyLimit) * 100 ve aylık eşini zero-guard'sız hesaplıyor. Limit 0.0 (kapalı/yanlış config) → NaN/Inf; alert eşiği, yüzde gösterimi ve JSON API'ye sessizce yayılır.
  - **Fix:** limit==0 ise 0 döndür (veya 'disabled' sentinel).

Doğrulama: `go test ./internal/alerts/... ./internal/executor/... ./internal/webhooks/... ./internal/budget/... -race`

---

## Faz 2 — Kırık test (opsiyonel · `test/fix-buildexecenv`)

- [x] **internal/executor · TestBuildExecEnv_Defaults**
  - **Neden:** buildExecEnv() append(os.Environ(), "PILOT_EXECUTOR=1", ...) döndürür; ANTHROPIC_BASE_URL/AUTH_TOKEN sadece backend'de set ise eklenir. Test bunların env'de OLMADIĞINI iddia eder. Suite, ANTHROPIC_BASE_URL export eden bir shell'de (örn Claude Code içinde) koşunca değer os.Environ()'dan sızar ve assertion patlar. Prod kodu doğru (parent env miras almak kasıtlı, GH-2371); TEST brittle/ortam-bağımlı.
  - **Fix:** Backend'i kurmadan önce os.Environ()'dan ANTHROPIC_* strip et, ya da sadece eklenen slice kuyruğunu assert et.

> Not: Prod bug değil. `go test ./...` temiz shell'de zaten geçer; sadece ortam sızıntısına dayanıklı hale getir.

---

## Faz 3 — Eklenecek testler (bölüm başına 1 PR · `test/gaps-<slug>`)

> SADECE test ekle, prod kodu değiştirme. Bir gap zaten kapalıysa atla → `[x]` + 'already covered'.
> ⚠️ Repo-wide'daki alert-XSS, budget-/0, parallel --model **Faz 1'de** test'le düzeltiliyor — Faz 1 yaptıysan burada atla.

### 3.0 Repo-wide (önce bunlar — en yüksek değer)

- [x] Startup orphan recovery (recoverOrphanedIssues) is untested across all 7 non-GitHub adapters — this is the exact path that runs on every daemon restart and is responsible for not re-executing in-flight tickets
- [ ] Webhook handler malformed-payload paths: extractIssueAndRepo (github), extractIssue (jira), hasPilotLabel (linear) all use unchecked type assertions that will panic on malformed JSON — zero fuzz or adversarial-payload tests exist  _(deferred → Faz 4 / dead-code / seam)_
- [ ] Cross-subsystem integration: no test exercises the adapter → executor → autopilot → release chain end-to-end; these are only covered by manual e2e runs  _(deferred → Faz 4 / dead-code / seam)_
- [ ] Concurrency/race on shared mutable state: randomString in webhooks uses time.Sleep+UnixNano (not crypto/rand, not atomic), approval.Manager fallback iterates a map in undefined order, and GlobalPatternStore.saveUnlocked uses non-atomic os.WriteFile — none have concurrent-call tests  _(deferred → Faz 4 / dead-code / seam)_
- [x] Budget Enforcer.GetStatus division-by-zero when DailyLimit or MonthlyLimit is 0.0 — a misconfigured or disabled limit produces NaN percent with no guard and no test
- [x] Parallel executor subagent model flag: '--model haiku' is prepended as one string arg instead of two, silently making claude ignore the model flag — no test catches this because executeSubagent has no unit tests at all
- [x] Alert email HTML injection: alert.Title and alert.Message are written directly into an HTML template without html.EscapeString — no test verifies that special characters are not rendered as raw tags
- [x] SQLite migration idempotency: the 260-entry flat migration slice has no version tracking and no test verifying that running migrate() twice on a live database is safe

### 3.x Executor Core  (6 gap)
> _174 test files, heavily table-driven. Core units (retry, error classification, worktree lifecycle, preflight, prompt builder, git ops, complexity, model routing, backend stream parsing, TDD gates) have focused unit tests. Integration tests in runner_integration_test.go cover state transitions, alert emission, quality gates, context cancellation. Epic, decompose, and signal paths have dedicated suites. TDD pipeline is tested at the unit level via mocked gate checkers._

- [ ] executeLintPushPR(): the duplicate lint-gate invocation (lines 26 and 85 of runner_execute_pr.go) has no unit test verifying it runs only once for direct-commit vs. exactly once for branch-push; both code paths call autoFixLint() with identical logic but no test catches the duplication.  _(deferred → Faz 4 / dead-code / seam)_
- [x] Retrier's smart-retry path inside runner.go (the inline retry loop with goto retrySucceeded at runner.go:262) has no test that exercises the full goto path: retry backend success updates backendResult and jumps to executeCopyResult.
- [x] decompose_on_kill fallback (runner.go:276–288): no test covers the path where a timeout BackendError triggers DecomposeForRetry and executeDecomposedTask.
- [ ] runner.parseStreamEvent(taskID, line, state) in runner_stream.go is dead code (zero non-test callers since processBackendEvent superseded it) — its test (runner_stream_test.go:TestParseStreamEvent) still exercises it, masking the fact it is unreachable at runtime.  _(deferred → Faz 4 / dead-code / seam)_
- [x] Budget guard + stall guard abort paths (executeBudgetGuard, executeStallGuard) have no direct tests; they are only implicitly exercised by end-to-end style tests.
- [x] session-resume fallback in ClaudeCodeBackend.Execute (--resume session_not_found retry, GH-2377) has no unit test covering the fallback leg.

### 3.x Executor Intelligence  (6 gap)
> _172 test files in internal/executor (the largest test suite in the codebase), 8 in internal/intent. Style is primarily table-driven with injected mock backends (cmdRunner func field pattern) and fake executeFunc overrides. Key intelligence files have dedicated test files: complexity_classifier_test.go (12 funcs), effort_classifier_test.go (10), model_routing_test.go (6), intent_judge_test.go (20), stagnation_test.go (4), scope_test.go (2), epic_sequential_test.go + ~12 epic_test_*.go files._

- [ ] StagnationMonitor is constructed only in tests — NewStagnationMonitor is never called in production code paths; the stall mechanism used in production is the separate watchdog.go (runStallWatchdog) which is not unit-tested independently  _(deferred → Faz 4 / dead-code / seam)_
- [x] ParallelRunner.executeSubagent has no tests; the model flag is prepended as a single string '--model haiku' rather than two separate args, which means exec.Command receives '--model haiku' as one arg — a silent bug that would cause claude to not recognize the flag
- [ ] EffortClassifier.classifyViaAPI uses http.DefaultClient directly with no injectable transport; the API path is only tested indirectly via the subprocess mock runner  _(deferred → Faz 4 / dead-code / seam)_
- [x] isSinglePackageScope / detectSameComponentFromTitles has no direct tests; only tested through epic integration tests
- [x] ModelRouter.resolveComplexity with a live EffortClassifier is not tested end-to-end (the floor escalation logic)
- [x] OutcomeTracker-based model escalation in ModelRouter.SelectModel has no test asserting the escalation path fires when failure rate exceeds threshold

### 3.x Executor Workflow & Quality  (6 gap)
> _~40 test files covering this subsystem. workflow/ package: 2 test files (workflow_test.go, hooks_test.go) with 13 table-driven and sequential unit tests covering Load edge cases, HookValue YAML deserialization, RunHook execution order/timeout/env/failure-stop/logFn. internal/quality: 5 test files with ~50 tests: runner_test.go covers RunAll (disabled/passing/failing/parallel/sequential default), runner_gate_test.go covers per-gate retry/cancel/coverage/timeout, executor_test.go covers Executor.Check/CheckWithAttempt/OnProgress, runner_feedback_test.go covers FormatErrorFeedback/ShouldRetry/parseCoverageOutput, types_test_detect.go covers DetectBuildCommand/DetectTestCommand polyglot detection. executor/ TDD: runner_tdd_test.go, runner_tdd_gates_test.go, runner_tdd_freeze_test.go, runner_tdd_verdict_test.go, runner_tdd_backends_test.go, runner_tdd_artifacts_test.go, runner_tdd_implementer_commit_test.go, runner_tdd_h4_test.go, runner_tdd_gojson_test.go with mock backends and scripted gate outcomes. hooks_test.go covers DefaultHooksConfig, GenerateClaudeSettings, JSON wire format. hooks_merge_test.go covers MergeWithExisting and CleanStalePilotHooks. Most tests are table-driven; integration tests use real git repos in temp dirs._

- [x] executeQualityGates retry loop (internal/executor/runner_execute_quality.go:81-349): the maxAutoRetries=2 circuit-breaker, backend re-invocation, and result token accumulation are not unit-tested — only the simpleQualityChecker wrapper is tested
- [x] runWorkflowHook warn-on-failure behavior (runner_quality.go:129-139): no test verifies that a failing hook does NOT abort execution
- [x] workflow.Load with before_run / after_run hooks being correctly stored in repoWorkflow and passed to runWorkflowHook in executePrepare — integration path is untested
- [ ] MergeWithExisting restoreFunc: the blind-restore path (original data written back) is tested but the GH-1884 cleanup-based hookRestoreFunc path in executePrepare is not  _(deferred → Faz 4 / dead-code / seam)_
- [ ] pilot-bash-guard.sh pattern matching: no test exercises the fallback grep/sed path when jq is unavailable  _(deferred → Faz 4 / dead-code / seam)_
- [x] TDD enforceTDDBaselineGreen for non-Go projects (exit-code fallback path) is not covered by the gate tests which use the scriptedGoTestRunner

### 3.x GitHub Adapter  (6 gap)
> _66 test files, 309 test functions. Exclusively table-driven and httptest-server-backed unit tests (no external API calls). Test helper files (_helpers_test.go) provide fake HTTP servers, stubbed stores, and mock callbacks reused across focused per-feature test files. Integration-style tests (poller_integration_*.go) compose the full Poller with an httptest.Server._

- [x] hasPendingDependencies: no test for the 'fail-safe returns true on GetIssue error' path
- [ ] ValidateSpec spec-incomplete first-strike flow (adding/checking pilot-spec-incomplete label on the issue) is not tested — the label is defined but no Poller-level integration test exercises the dispatch gate  _(deferred → Faz 4 / dead-code / seam)_
- [ ] extractIssueAndRepo in webhook.go uses unchecked type assertions (e.g. issueData['number'].(float64)) which panic on malformed payloads — no fuzz or malformed-payload test  _(deferred → Faz 4 / dead-code / seam)_
- [x] Cleaner.StartupRecover 30-minute staleness floor: no test verifies that a row created 25 minutes ago is NOT cleaned while a 35-minute-old row IS
- [x] RetryAfter capped at MaxDelay (30s): WithRetry caps Retry-After>MaxDelay to MaxDelay but this cap is only partially covered — the 'runaway Retry-After: 3600' case has no dedicated test
- [x] groupByOverlappingScope sanitization: no test for an issue body containing invisible Unicode smuggled directory paths

### 3.x Ticket Adapters  (6 gap)
> _86 test files across the 7 in-scope adapter packages (108 test funcs in linear, 67 in jira, 82 in asana, 55 in gitlab, 57 in azuredevops, 46 in plane, 41 in bitbucket). Predominantly table-driven unit tests using httptest.NewServer stubs for API clients and hand-rolled fake ProcessedStore implementations for poller dedup tests. Concurrency tests use goroutine-synchronisation via WaitForActive()._

- [x] recoverOrphanedIssues is untested in all 7 non-GitHub adapters — jira, linear, asana, gitlab, azuredevops, plane, bitbucket have no test covering startup orphan recovery
- [x] jira.WebhookHandler.Handle() for jira:issue_updated (label-added changelog path) has no end-to-end test exercising wasLabelAdded logic
- [x] linear.WebhookHandler.hasPilotLabel fallback branch (labelIds path) is exercised only by a comment, not a test
- [x] Plane cacheStateIDs / UpdateIssueState state transition is untested (no test for started or completed state UUID resolution)
- [x] processIssueAsync error path (failed label application when onIssue returns error) is untested in asana, jira, and plane pollers
- [x] AzureDevOps sequential mode (startSequential / findOldestUnprocessedWorkItem / MergeWaiter integration) has no test

### 3.x Chat Adapters & Comms  (7 gap)
> _58 test files, approximately 280 individual Test* functions across the subsystem. Tests are predominantly table-driven (especially Telegram formatter and command routing), supplemented by mock-based unit tests. comms package tests use an internal handlerMock that implements comms.Messenger. Slack has the deepest transport coverage including Socket Mode reconnect, resilience, and integration tests using httptest.NewServer and a fake WebSocket server._

- [x] TelegramMessenger.SendConfirmation sends CallbackData 'execute_task:<taskID>' but handleCallback only matches 'execute'/'cancel' — the confirmation buttons are silently dropped; no test covers this end-to-end callback path
- [x] handler_fastpath.go (tryFastAnswer, fastListTasks, fastGrepTodos, fastReadStatus) has zero test coverage — these do filesystem I/O with real paths
- [x] comms.CleanInternalSignals NAVIGATOR_STATUS block-skipping logic is not tested (only simple signal stripping is covered in formatter_signals_test.go)
- [ ] comms.Handler.handlePlanning() — the code path that stores a pending task after executor returns a plan — has no direct test  _(deferred → Faz 4 / dead-code / seam)_
- [ ] transcription.WhisperAPI.Transcribe() has no test; the HTTP client is not abstracted so it cannot be unit tested without a live API or interface injection  _(deferred → Faz 4 / dead-code / seam)_
- [x] Discord GatewayClient.Resume() path (session resume on close code 4000-4009) has no test
- [x] Slack InteractionHandler.ServeHTTP (webhook.go) signature replay-attack protection is tested but the onAction callback dispatch path has no test

### 3.x Autopilot  (6 gap)
> _83 test files vs 35 source files — exceptionally high ratio. Tests are table-driven with in-package mocks (no external test framework). Integration tests are build-tag guarded (//go:build integration) and use a real mock HTTP server. Unit tests cover: state-machine transitions, circuit breakers, CI discovery, auto-merge, approval flow, scope/size guards, guardrails gate (17 test functions including fail-open paths), feedback loop (issue body, iteration counter, review format), releaser (semver parsing, bump detection), metrics, StateStore persistence, conflict handling, learning-loop integration._

- [x] handleReleasing: race between GetTagForSHA and CreateTagForRepo when two PRs merge simultaneously is not directly tested (TASK-316 guard exists but only duplicate-tag error path is tested)
- [x] resolveMainBranchName fallback to literal 'main' when no env.Branch configured — no test for the WARN log path
- [ ] Metrics.RestoreFromRow on startup (acknowledged TODO GH-2836): token/cost counters reset to zero on restart, no test for counter continuity after restart  _(deferred → Faz 4 / dead-code / seam)_
- [x] runGuardrailsGate recover() panic path: the defer/recover in controller is untested; a panicking ruleEvaluator should be no-op'd
- [x] hasChangesRequested bot-filter and pre-PR-creation review timestamp filtering: only covered implicitly via polling-mode transition tests, no direct unit test for the filter logic
- [x] maybeCloseParentIssue native-subissue-links fallback to text search: happy path tested via controller_parentclose tests but the hasNativeLinks=false branch switching to SearchOpenSubIssues is not directly exercised

### 3.x Memory & Learning  (7 gap)
> _46 test files, ~237 test functions. Predominantly table-driven Go tests using os.MkdirTemp + real SQLite databases (no mocks for DB). Sub-files use name prefixes to logically group: extractor_test_ci*, extractor_test_selfreview*, feedback_learn*, etc. Concurrency safety is spot-checked in profile_userprofile_test.go._

- [x] PatternSync.SyncFromProject and aggregatePattern confidence formula are not tested — only ExportPatterns/ImportPatterns have a basic smoke test (cross_pattern_org_test.go)
- [x] LearningLoop.ApplyDecay does not have a test that crosses the 3-month stale threshold with real time manipulation
- [ ] OrgPatternStore.save writes directly via os.WriteFile (no atomic rename) — no crash-safety test exists  _(deferred → Faz 4 / dead-code / seam)_
- [ ] GlobalPatternStore.saveUnlocked also uses direct os.WriteFile (unlike KnowledgeGraph which is atomic) — no crash or concurrent-write test  _(deferred → Faz 4 / dead-code / seam)_
- [x] GetContextualConfidence recency decay formula is not directly unit-tested; coverage comes only indirectly via PatternQueryService tests
- [x] metering_thresholds.go CheckUsageThresholds has hardcoded $100/500-task thresholds — no test verifies the threshold boundaries behave correctly for values just below/above limits
- [x] store_executions_query.go GetExecutionsInPeriod project-filter IN-clause path has no dedicated test

### 3.x Gateway & Infra  (7 gap)
> _Well-tested overall: gateway has 24 test files (~1 500 lines), codexruntime 4 test files, health 7, tunnel 14, webhooks 4. Style is table-driven throughout. Gateway tests use httptest.NewRecorder() for HTTP handlers and real Start() for integration. codexruntime uses shell-script fixtures to simulate app-server stdio._

- [x] gateway.handleLinearWebhook with a valid Ed25519 key: no test verifies that a correctly signed request passes and an unsigned request returns 401 when linearWebhookPublicKey is non-nil
- [x] gateway.handleGithubWebhook with a configured secret: no test exercises the HMAC verification rejection path (all tests use NewServer without GithubWebhookSecret)
- [x] gateway.handleBitbucketWebhook, handleAzureDevOpsWebhook, handleGitlabWebhook, handlePlaneWebhook: no test files at all — only Linear/GitHub/Jira/Asana are covered
- [ ] gateway.runRuntimeSession end-to-end: the approval flow and multi-turn sequencing are tested as unit tests on individual functions but there is no integration test that wires a fake codex subprocess through the full WebSocket path  _(deferred → Faz 4 / dead-code / seam)_
- [ ] gateway.handleDashboardMetrics / handleDashboardQueue / handleDashboardHistory / handleDashboardLogs / handleGitGraph: no tests exercise these REST handlers  _(deferred → Faz 4 / dead-code / seam)_
- [ ] tunnel.CloudflareProvider.Start(): the URL-detection logic (parsing stderr, timeout fallback) has no test; manager_lifecycle_test.go tests only Start/Stop/IsRunning on the Manager facade with a nil provider  _(deferred → Faz 4 / dead-code / seam)_
- [x] webhooks.randomString: uses time.Sleep(time.Nanosecond) in a loop which is both slow and non-cryptographic; no test validates ID uniqueness under concurrent calls

### 3.x Dashboard & TUI  (8 gap)
> _~20 test files, ~180 test functions, all table-driven with ANSI stripping via stripANSI(). Coverage is dense on rendering (panel line-width assertions on every rendered output), model message dispatch (Update() round-trip tests for every tea.Msg type), git graph parsing/rendering/scrolling/sync, autopilot rail glyph correctness, findings panel content and style, sparkline normalization, and SQLite hydration/refresh. Desktop package has focused tests on GetServerStatus, EnsureGatewayRunning, GetGitGraph ranking logic._

- [x] renderTask() shimmer bar stagger: no test verifies the per-offset center calculation or that adjacent queued items have visually distinct positions
- [x] renderLogs(): no tests at all — log truncation, 10-line window cap, and the 100-entry ring-buffer cap are untested
- [x] renderSplash(): no tests for the lamp progression logic (litCount formula) or READY footer appearance timing
- [x] storeRefreshCmd() CostPerTask computation: the division branch when TotalTasks==0 is untested
- [x] buildAdapterChipsRow() budget-overflow path: the +N idle fallback and the chip-drop loop (lines 265-285 of tui_banner.go) have no test
- [x] desktop/App.GetHistory() deduplication logic (historyEntryBetter) and epic grouping (SubIssues field) are not exercised by any test
- [x] desktop/App.GetLogs() reversal (oldest-first) is not tested
- [x] desktop/App.EnsureGatewayRunning() concurrent double-call race (gatewayStarting flag) has no test

### 3.x Architect  (6 gap)
> _428 test functions across 44 test files (46 _test.go files total), covering nearly every file in the package. Style is exclusively table-driven and unit tests with mock injection. All external I/O (git, go list, executor backend, GitHub API) is abstracted behind interfaces (commandRunner, gateRunner, IssueCreator, IssueSearcher, backendFactory) and injected via internal constructors (newDepsCollector, newOwnershipCollector, newStaleTestsCollector, newTestAnalyzer). Three integration tests wire real collectors against temp-dir fixtures. An e2e test file (rule_suggest_e2e_test.go) exercises the full scan → suggest path without network. Both cmd/pilot and internal/architect are covered; the cmd layer has 30+ tests for flag routing, config wiring, and Linear export._

- [x] DepsCollector.Collect(): no test covers the full `go list` parse path through loadPackageGraph against a real Go module fixture — only the individual sub-analyses (cycle/layer/heavy/unused) are unit-tested with mock commandRunner output
- [x] RunRadar() error path when BuildLensScanner fails (unknown lens name) — tested indirectly but no explicit test for the BuildLensScanner-error branch
- [x] enrichRFCWithBackend() in commands_architect_rfc.go: the backend-enrichment fallback path (ok=false on error/empty response) has no dedicated test; only the write-path happy case is tested
- [ ] emitRefactorPlan() (commands_architect_refactor.go): no test for the limit=0 (no cap) case or for a linearIssueCreator returning an error mid-sequence  _(deferred → Faz 4 / dead-code / seam)_
- [x] assignOrderAndDeps() DependsOn chain: no test for a plan where multiple blast tiers produce a multi-hop DependsOn chain (only single-predecessor is exercised)
- [x] GatewayAuthRule.Eval() heuristic: tested with a few fixtures but the 'auth middleware present anywhere in file = not flagged' branch has limited negative-case coverage

### 3.x Alerts, Approval, Teams, Budget  (5 gap)
> _Extremely well-tested: alerts has 154 test functions across 17 test files (table-driven throughout, dedicated files per concern: cooldown, escalation, stuck, cost/security, dispatcher, metrics, task337 E1/E5 regression, OOM, autopilot); approval has 90 test functions across 9 files (Telegram callback/store/format/helpers, Slack, GitHub polling/webhook, rules complexity/file-pattern, manager submit/helpers); teams has 82 test functions across 10 files (store CRUD, identity resolution, role/permission matrix, audit log, service CRUD, member projects); budget has 32 test functions across 4 files (enforcer pause/resume/reset/alert, CheckBudget limit variants, TaskLimiter token/duration/context, Status helpers). Style: table-driven with subtests, mock interfaces, in-memory SQLite for store tests._

- [x] alerts.handleAutopilotMetrics dead-letter paths: no test fires an actual EventTypeAutopilotMetrics event through the running engine to assert alert generation for AlertTypeFailedQueueHigh, AlertTypeCircuitBreakerTrip, AlertTypeAPIErrorRateHigh, AlertTypePRStuckWaitingCI, or AlertTypeDeadlock — only the default-rule registration is tested
- [x] budget.Enforcer.GetStatus division-by-zero: if config.DailyLimit or config.MonthlyLimit is 0.0 (disabled / misconfigured), GetStatus computes NaN percent; no test covers this edge and there is no guard in the code
- [x] teams.Service.ResolveSlackIdentity ignores the slackUserID parameter (silently assigned to _); the store already has GetMembersBySlackUserID; no test verifies the intended fallback path when slackUserID would match but email does not
- [x] approval.Manager.SubmitApprovalRequest non-deterministic fallback handler: when preferred channel is absent and multiple handlers are registered, the fallback iterates a map (random order) — no test verifies which handler is selected, nor that retry logic is deterministic
- [x] alerts.EmailChannel.formatBody injects alert.Title and alert.Message directly into HTML without html.EscapeString; no test verifies that special characters are not rendered as raw HTML tags (XSS in alert email bodies)

### 3.x CLI, Config & Wiring  (6 gap)
> _~63 test files across the subsystem. internal/wiring (3 test files) has the most structurally significant tests: parity tests verify all 18 Runner Has* accessors match between polling and gateway harnesses for 7 config combinations. internal/config (16 test files) is table-driven with good coverage of validation bounds, deprecation warnings, project lookup, load/save round-trips. cmd/pilot (36 test files) covers flag parsing, adapter Enabled() gating per-adapter, onboarding persona routing, guardrails wiring, and adapter wiring coverage. internal/orchestrator (5 test files) covers struct construction and quality-gate happy/retry/exhaust paths. internal/pilotapi (3 test files) provides comprehensive SHA-256 hash property tests and artifact chain validation._

- [x] Orchestrator.ProcessTicket and sibling methods (ProcessGithubTicket, ProcessGitlabTicket, etc.) have no direct unit tests — the happy-path dispatch through bridge.PlanTicket, saveTaskDocument, and QueueTask is untested in isolation
- [ ] pollingRuntime phase methods (setupStores, setupRunner, startGitHubPolling, startAdapterPollers) have no unit or integration tests; only the harness exercises a subset of this logic  _(deferred → Faz 4 / dead-code / seam)_
- [ ] prepareStart / startContext in start_cmd_config.go are dead code (never called) and consequently untested — the real preparation logic lives inline in newStartCmd RunE  _(deferred → Faz 4 / dead-code / seam)_
- [x] config.Load env-var expansion (os.ExpandEnv path) is not explicitly tested; only the YAML parsing branch is covered
- [ ] buildGatewayInfra has no tests; failures in team RBAC, approval handler rehydration, or dispatcher start are exercised only in e2e tests  _(deferred → Faz 4 / dead-code / seam)_
- [ ] The Python bridge (bridge.go: PlanTicket, ScoreTasks, GenerateBrief, runPython) has no Go-level tests; Python subprocess failures produce untested error paths  _(deferred → Faz 4 / dead-code / seam)_

### 3.x Replay, Briefs, Upgrade, Logging, Budget  (10 gap)
> _52 test files across all five packages with 250+ individual Test* functions. Style is table-driven in upgrade and budget; integration-style with httptest servers and TempDir in upgrade; unit tests with mock interfaces in briefs and budget. Replay uses direct file I/O with TempDir. Logging tests write to a bytes.Buffer and parse the output. Overall coverage is high in upgrade (120 test funcs covering download, extract zip/tar.gz, rollback, codesign injection, restart smoke-test, state machine transitions) and briefs (full formatter + delivery path via mock senders). Budget and logging are well-exercised on the happy path._

- [x] replay.Analyzer.Analyze(): no test for phase detection from Navigator text patterns (detectPhaseFromText) or decision-point extraction from 'WORKFLOW CHECK' strings
- [x] replay.Player.Play(): Speed > 0 real-time delay logic and context cancellation mid-play are untested
- [ ] replay.ViewerModel (bubbletea): Update() key bindings, filter toggling, and scroll position logic in viewer_update.go are entirely untested  _(deferred → Faz 4 / dead-code / seam)_
- [x] replay.ExportHTMLReport / ExportToMarkdown: the analysis-section rendering branches (phase chart, tool usage, errors) have no dedicated tests
- [x] logging.rotatingWriter: cleanOldLogs() age-based removal and maxBackups eviction are untested; the TryLock skip-if-running path has no test
- [x] logging.Suppress(): no test verifies that subsequent slog.Info() calls are silenced after Suppress()
- [x] briefs.Scheduler.maybeCatchUp(): the missed-brief detection by iterating the cron schedule backwards is not covered by any test
- [x] upgrade.VersionChecker: the background polling goroutine and OnUpdate callback path have no test; IsHomebrew early-exit in check() is untested
- [x] budget.Enforcer.fireAlert(): alert callback is never exercised; the warning-threshold branch in CheckBudget() has no test
- [x] budget.TaskLimiter.CreateContext(): the context timeout cancellation path is untested

---

## Faz 4 — Refactor (madde başına 1 PR · davranış birebir korunur)

> Bunlar çoğunlukla kod-kalitesi (bug değil). Aciliyet düşük. Her biri ayrı PR; davranış değişmez.

### 4.0 Headline repo-wide refactor (en yüksek kaldıraç)

- [ ] **[high] 7x-duplicated adapter poll/dedup/transition loop** — `internal/adapters/registry.go:48 (shared types unused by children)`
  - Every non-GitHub adapter (linear, jira, asana, gitlab, azuredevops, plane, bitbucket) redeclares ProcessedStore interface, IssueResult struct, recoverOrphanedIssues(), and processIssueAsync() independently. The canonical types already exist in internal/adapters/registry.go but no child adapter imports them. Extracting a generic BasePoller[T any] with the shared poll/filter/dedup/dispatch loop would eliminate ~20% of all adapter LOC, ensure consistent orphan-recovery behaviour across adapters, and make the startup-recovery gap fixable in one place.
- [ ] **[high] Runner god struct (120+ fields)** — `internal/executor/runner_construct.go:21`
  - The Runner struct has absorbed every executor capability over time: 4 per-stage backends, 4 TDD role backends, 15+ optional callbacks, stagnation/drift/monitoring sub-systems, caches, flags, and test seams. This forces every test that needs even one capability to construct or mock the entire object. Splitting into composable capability structs (BackendSet, TDDConfig, ObservabilityHooks, KnowledgeContext) wired via a thin coordinator would halve per-test setup boilerplate and make individual capabilities testable in isolation.
- [ ] **[high] Non-cryptographic randomString in webhooks/event.go** — `internal/webhooks/event.go:108`
  - randomString generates event IDs using time.Now().UnixNano() % charset_len with a time.Sleep(nanosecond) between each byte. This is (a) not collision-safe under concurrent calls because two goroutines sleeping one nanosecond apart can produce the same timestamp, (b) not cryptographically random so IDs are guessable, and (c) artificially slow (16 sleeps per ID). Replacing with crypto/rand.Read is a 3-line fix with no API change.
- [ ] **[medium] Unversioned flat SQL migration slice** — `internal/memory/store_migrate.go:10`
  - The 260 raw SQL strings are executed sequentially with no schema_version table. If a migration fails mid-way the database is left in a partially-migrated state with no way to resume or roll back. Running migrate() twice on an existing database is only safe by accident (CREATE TABLE IF NOT EXISTS). Adding a migrations table tracking applied indices (or adopting golang-migrate) would make schema evolution reliable and testable.
- [ ] **[medium] Budget Enforcer division-by-zero on disabled limits** — `internal/budget/enforcer.go:156`
  - GetStatus computes (totalCost / config.DailyLimit) * 100 with no zero-guard. When DailyLimit or MonthlyLimit is 0.0 (budget disabled or misconfigured), the result is NaN or Inf, which propagates silently into alert threshold comparisons, percentage displays, and JSON API responses. A single if limit == 0 { return 100 } guard (or explicit 'disabled' sentinel) fixes all callers at once.
- [ ] **[medium] HTML injection in alert email body** — `internal/alerts/channels_email.go:99`
  - alert.Title and alert.Message are interpolated directly into an HTML template via fmt.Sprintf without html.EscapeString. A ticket title containing <script> or an attacker-controlled message field renders as live HTML in the recipient's email client. Because Pilot processes external ticket sources (GitHub, Linear, Jira, Slack), an attacker can control the title field and craft a stored-XSS payload delivered to all alert recipients. Fix is two html.EscapeString calls.

### 4.x Executor Core  (6)
- [ ] **[high] Runner is a god struct with 50+ fields** — `internal/executor/runner_construct.go:21`
  - The Runner struct holds every optional capability (decomposer, intentJudge, retrier, worktreeManager, knowledgeGraph, driftDetector, metricsRecorder, 8 backend pointers, 30+ callbacks/flags). This makes construction fragile, tests require dozens of SetX() calls to reach a valid state, and any new feature adds another field. Splitting into clearly-scoped sub-structs (e.g. RunnerCore, RunnerOptions, RunnerHooks) would isolate concerns.
- [ ] **[medium] Duplicate pre-push lint gate invocation** — `internal/executor/runner_execute_pr.go:26,85`
  - autoFixLint() is called twice inside executeLintPushPR() with identical guard and side-effect logic — once before the DirectCommit branch and once inside the CreatePR branch. The first call at line 26 only matters when DirectCommit is true, but lint runs unconditionally before the if-DirectCommit check, meaning any PR path runs lint twice. Should be one call placed inside each branch.
- [ ] **[medium] goto in runner.go smart-retry path** — `internal/executor/runner.go:262`
  - The smart-retry success path uses goto retrySucceeded to jump forward over several variable assignments and into executeCopyResult(). This is the only goto in the codebase and obscures control flow. Extracting the retry attempt into a helper that returns (backendResult, err) and letting the caller fall through normally would remove the label and make the retry path testable in isolation.
- [ ] **[medium] executeState carrier struct couples all phases** — `internal/executor/runner_execute_state.go:17`
  - executeState is passed by pointer through every phase method, holding 40+ fields. Each phase deposits into fields that later phases consume. This creates invisible temporal coupling (phase N must run before phase M or fields are zero-valued) and makes it impossible to unit-test a single phase without constructing the entire state. Per-phase explicit return values or smaller scoped structs would make dependencies explicit.
- [ ] **[low] runner.parseStreamEvent is dead code** — `internal/executor/runner_stream.go:11`
  - Runner.parseStreamEvent(taskID, line string, state) has zero non-test callers: all live paths use processBackendEvent() (which receives a pre-parsed BackendEvent from the backend). The method and its test exist only as a legacy artifact. Removing it reduces confusion about which parsing path is active.
- [ ] **[low] Error classification relies solely on substring matching of stderr** — `internal/executor/backend_claudecode_errors.go:63`
  - classifyClaudeCodeError() checks ToLower(stderr) for strings like 'hit your limit', 'api error', 'authentication'. New CC versions or locale-variant messages silently fall through to ErrorTypeUnknown, losing the retry benefit. A structured error field in CC's stream-JSON exit event (if available) or a versioned pattern table would be more resilient.

### 4.x Executor Intelligence  (6)
- [ ] **[high] Runner god struct with 50+ fields** — `internal/executor/runner_construct.go:21`
  - Runner has over 50 fields spanning backends, callbacks, injectable fns for testing, feature flags, and lifecycle tracking. Construction is implicit via setter methods; there is no single place to audit what is wired. This makes it hard to reason about which capabilities are active for a given deployment and causes silent no-ops (e.g., StagnationMonitor is never wired despite being tested).
- [ ] **[high] StagnationMonitor dead code in production** — `internal/executor/stagnation.go`
  - NewStagnationMonitor is called only in tests. Production stall detection uses runStallWatchdog (watchdog.go). The StagnationMonitor API (RecordState, Level, ShouldCommitPartial, Reset) is fully implemented but never invoked during actual execution, making it dead code that misleads future developers.
- [ ] **[high] model flag construction bug in ParallelRunner.executeSubagent** — `internal/executor/parallel.go:247-262`
  - modelFlag is constructed as a single string '--model haiku' and then prepended via append([]string{modelFlag}, args...). exec.Command treats this as one argument, not two, so the model flag is silently ignored and all research subagents run with the default model regardless of configuration.
- [ ] **[medium] Duplicated JSON parsing strip logic in EffortClassifier and ComplexityClassifier** — `internal/executor/effort_classifier.go:339 / internal/executor/complexity_classifier.go:184`
  - parseEffortResponse and parseClassificationResponse share identical markdown fence stripping (TrimPrefix ```json, ```) and JSON unmarshal+switch logic. The only difference is the response struct and the return type. This 40-line pattern should be a generic helper.
- [ ] **[medium] http.DefaultClient in EffortClassifier.classifyViaAPI** — `internal/executor/effort_classifier.go:249`
  - The API path uses http.DefaultClient with no injectable transport, making it impossible to test the classifyViaAPI path with a mock HTTP server. The subprocess path has a clean cmdRunner injection seam but the direct API path does not, creating an asymmetric testing surface.
- [ ] **[low] resolveComplexity called twice per routing decision** — `internal/executor/model_routing.go:107 and 149`
  - ModelRouter.SelectModel and ModelRouter.SelectTimeout each call resolveComplexity independently. resolveComplexity calls both DetectComplexity (regex scan) and EffortClassifier.Classify (potentially an LLM call). When both SelectModel and SelectTimeout are called for the same task (as they are in runner_execute_prepare.go), the LLM is queried twice unless the cache kicks in for EffortClassifier. The cache is per task.ID, which helps, but the double call is still confusing and wastes the first result.

### 4.x Executor Workflow & Quality  (6)
- [ ] **[high] executeQualityGates is a 350-line monolith with three nested early-return paths, webhook/alert emission duplicated across all three failure exits, and an inner retry loop that re-assembles backend options inline** — `internal/executor/runner_execute_quality.go:17`
  - The failure-path boilerplate (emitAlertEvent + dispatchWebhook + recorder.Finish + return result) is copy-pasted three times (lines ~88-121, ~207-282, ~312-340). Extracting a failQualityGates(s, err, phase string) helper would cut ~120 lines and make the retry loop readable.
- [ ] **[medium] Dual QualityOutcome/QualityGateDetail mirror types exist purely to avoid an import cycle between internal/executor and internal/quality** — `internal/executor/quality.go:1`
  - QualityOutcome mirrors quality.ExecutionOutcome; QualityGateDetail mirrors quality.GateReportItem. The cycle could be broken by moving the shared types to a thin internal/pilotapi sub-package (pilotapi already hosts HandoffArtifact), eliminating the duplicates and the simpleQualityChecker adapter.
- [ ] **[medium] hookRestoreFunc is discarded (MergeWithExisting restore function is captured but immediately replaced by a CleanStalePilotHooks closure in executePrepare)** — `internal/executor/runner_execute_prepare.go:279`
  - MergeWithExisting returns a restore function (line 279) but it is never stored — the real cleanup closure defined inline at line 287 calls CleanStalePilotHooks instead. The returned restoreFunc goes unused (assigned to `_` implicitly), which is subtle and means a future caller re-using MergeWithExisting would need to know not to use the return value.
- [ ] **[low] TDD role retry logic duplicated between runTDDTestAuthor and runTDDImplementer loops** — `internal/executor/runner_tdd.go:267`
  - runTDDTestAuthor has its own bounded for-loop (lines 271-289) mirroring the retry structure of enforceTDDGreenGate. The commit-check pattern (before/after count) is also duplicated in runTDDTestAuthorOnce and runTDDImplementer. A shared runRoleUntilCommit helper would consolidate this.
- [ ] **[low] pilot-stop-gate.sh duplicates the project-type detection already in quality.DetectTestCommand / DetectBuildCommand in Go** — `internal/executor/hookscripts/pilot-stop-gate.sh:1`
  - The shell script hand-rolls the same Go/Node/Python/Rust/Makefile detection logic that already exists in quality.DetectBuildCommand and DetectTestCommand. A future project type added to the Go functions would silently be missing from the shell hook.
- [ ] **[low] GenerateClaudeSettings has an implicit boolean-inversion bug for RunTestsOnStop nil check** — `internal/executor/hooks_settings.go:20`
  - Line 20: `if config.RunTestsOnStop == nil || *config.RunTestsOnStop` installs the Stop hook when the pointer is nil OR when it is true. But DefaultHooksConfig() sets RunTestsOnStop to false (GH-2432), so nil would mean 'no override present' and the Stop hook should NOT be installed by default. The nil branch adds the hook contrary to the documented default.

### 4.x GitHub Adapter  (6)
- [ ] **[high] Duplicated issue-filter loop between checkForNewIssues and findOldestUnprocessedIssue** — `internal/adapters/github/poller_parallel.go and internal/adapters/github/poller_fetch.go`
  - Both functions contain nearly identical sequential label-check blocks (in-progress, blocked, needs-clarification, superseded, failed-retry, retry-ready, grace period, taskChecker, hasMergedWork, hasOpenPRAwaitingMerge, execChecker). Any new skip rule must be added in two places and has already diverged: parallel path records skip metrics via recordSkip(), sequential path does not. This should be a shared filterCandidates() helper.
- [ ] **[high] God struct: Poller has 30+ fields covering orthogonal concerns** — `internal/adapters/github/poller.go:92`
  - Poller mixes execution-mode config, rate-limit retry scheduler, persistent store, board sync, metrics, pre-flight judge, retry counters, and concurrency primitives into a single struct. Extracting a DispatchPolicy (retry strategy, preflight, exec checker) and a BoardConfig sub-struct would make each concern independently testable.
- [ ] **[high] Unchecked type assertions in WebhookHandler.extractIssueAndRepo and handlePRReview** — `internal/adapters/github/webhook.go:196`
  - issueData['number'].(float64), issueData['title'].(string), labelMap['name'].(string), prData['number'].(float64) etc. all panic on any malformed webhook payload. GitHub delivers malformed bodies on retries and edge cases. Should use safe comma-ok assertions and return errors.
- [ ] **[medium] Error type detection via string prefix slicing instead of typed errors** — `internal/adapters/github/client.go:175-189`
  - isNotFoundError and isUnprocessableError compare errStr[:21] against a magic prefix string. This is fragile: it couples error detection to the exact format string in doRequest, breaks under wrapping (fmt.Errorf('%w')), and is impossible to match with errors.As. A typed APIError struct with StatusCode would fix this.
- [ ] **[medium] retryReadyCount in-memory map is a now-stale mirror with misleading field name** — `internal/adapters/github/poller.go:143`
  - After GH-2432 the retry source-of-truth moved to GitHub labels (pilot-retry-1/2/exhausted). The in-memory retryReadyCount is kept 'in sync' as a mirror, but it resets on restart (defeating the purpose) and the field comment says 'Legacy in-memory counter in sync so existing tests/state observers remain consistent'. The field should be removed and all reads replaced with label inspection.
- [ ] **[low] Duplicate comment in startSequential for the OnPRCreated gate** — `internal/adapters/github/poller_dispatch.go:208-210`
  - The comment 'Gate: PRNumber > 0 implies executor surfaced a valid PR URL...' appears verbatim twice within five lines (lines 208 and 210), and the same comment block is copy-pasted into poller_parallel.go:326-328. The duplication suggests the blocks were copy-pasted and the deduplication into a helper was deferred.

### 4.x Ticket Adapters  (6)
- [ ] **[high] ProcessedStore interface duplicated 7 times** — `internal/adapters/linear/poller.go:25, internal/adapters/jira/poller.go:31, internal/adapters/asana/poller.go:32, internal/adapters/gitlab/poller.go:36, internal/adapters/azuredevops/poller.go:36, internal/adapters/plane/poller.go:33, internal/adapters/bitbucket/poller.go:36`
  - Identical four-method interface (Mark/Unmark/IsProcessed/Load) defined in every adapter package. It already exists as adapters.ProcessedStore in registry.go but the adapters ignore it and define their own copies. Any future method addition requires 7 synchronised edits.
- [ ] **[high] IssueResult/WorkItemResult/TaskResult struct duplicated 7 times** — `internal/adapters/linear/poller.go:15, internal/adapters/jira/poller.go:21, internal/adapters/asana/poller.go:22, internal/adapters/gitlab/poller.go:26, internal/adapters/azuredevops/poller.go:26, internal/adapters/plane/poller.go:23, internal/adapters/bitbucket/poller.go:26`
  - All six share the same five fields (Success, PRNumber, PRURL, HeadSHA, BranchName, Error). adapters.IssueResult already exists in registry.go but is not used by the adapters, creating a drift risk whenever autopilot wiring needs a new field.
- [ ] **[medium] parallel dispatch boilerplate copy-pasted across 7 pollers** — `internal/adapters/linear/poller_processing.go:118, internal/adapters/jira/poller_lifecycle.go:148, internal/adapters/asana/poller_tasks.go:81, internal/adapters/gitlab/poller_dispatch.go:63, internal/adapters/azuredevops/poller_parallel.go, internal/adapters/plane/poller_dispatch.go:97, internal/adapters/bitbucket/poller_run.go`
  - The semaphore-acquire → stopping-check-under-wgMu → activeWg.Add(1) → go processXxxAsync pattern is copy-pasted identically in every adapter. A shared parallel dispatcher type (accepting a work function) would remove ~50 lines of identical locking logic from 7 files.
- [ ] **[medium] linear.WebhookHandler.hasPilotLabel operates on raw map[string]interface{}** — `internal/adapters/linear/webhook.go:188`
  - The method receives an untyped map from the webhook payload, type-asserting fields manually. If the Linear API returns the newer label connection shape (as was already a bug in GraphQL — see commit 6bcaef7) the assertion silently falls back to the 'return len(labelIDs) > 0' branch which approves any-labelled issue, not just pilot-labelled ones. Using the typed Issue struct from the GraphQL response (as the poller path does) would eliminate this ambiguity.
- [ ] **[medium] Jira webhook.go extractIssue is a 130-line hand-rolled JSON walker** — `internal/adapters/jira/webhook.go:162`
  - The function manually type-asserts every field from a map[string]interface{} (summary, description, labels, issuetype, status, priority, project) instead of json.Unmarshal into the typed Issue struct already defined in types.go. This doubles maintenance and is the source of the ADF description dual-type check at lines 194-197.
- [ ] **[low] Plane label UUID cache only resolves from the first project that has matching labels** — `internal/adapters/plane/poller_cache.go:42`
  - cacheLabelIDs breaks out of the project loop as soon as pilotLabelID is found. In a multi-project workspace where labels live in different projects, the inProgressLabelID / doneLabelID may be from project A while work items dispatched from project B never get state labels applied. The intent is likely to resolve per-project but the current logic uses a single shared UUID for all projects.

### 4.x Chat Adapters & Comms  (6)
- [ ] **[high] TelegramMessenger.SendConfirmation callback data mismatch** — `internal/adapters/telegram/messenger.go:49-50`
  - SendConfirmation sends 'execute_task:<taskID>' and 'cancel_task:<taskID>' as CallbackData, but handleCallback in handler_messages.go:104,119 only matches the exact strings 'execute' and 'cancel'. Any confirmation button rendered via the comms.Messenger interface is silently ignored — the task can only be confirmed via text 'yes'/'no'. The legacy CommandHandler still uses plain 'execute'/'cancel' for a different code path, creating a latent dual-path inconsistency.
- [ ] **[medium] Triplicated CleanInternalSignals and ChunkContent utilities** — `internal/adapters/telegram/formatter_text.go, internal/adapters/slack/formatter.go, internal/adapters/discord/formatter.go`
  - CleanInternalSignals(), ChunkContent(), makeProgressBar(), and truncate helpers are each independently re-implemented in all three adapter formatter files. comms/util.go already exports CleanInternalSignals, ChunkContent, TruncateText, and GenerateProgressBar. The adapter copies diverge in edge cases (e.g. discord/formatter.go's CleanInternalSignals lacks the NAVIGATOR_STATUS block-skip logic present in comms/util.go). All three adapters should import from comms instead.
- [ ] **[medium] handler_fastpath.go tight coupling to filesystem paths** — `internal/adapters/telegram/handler_fastpath.go:44-356`
  - tryFastAnswer(), fastListTasks(), fastGrepTodos(), and fastReadStatus() directly call os.ReadDir, os.ReadFile, os.Open, and filepath.Walk using h.projectPath as the root. There is no injectable filesystem abstraction, making these functions untestable without real project layout on disk. They also duplicate task-listing and TODO-grep logic that belongs in the executor or memory layer.
- [ ] **[medium] Telegram Handler holds redundant task state alongside comms.Handler** — `internal/adapters/telegram/handler.go:66-84`
  - telegram.Handler declares its own PendingTask and RunningTask structs (lines 47-64) and the Handler struct still has a commsHandler field that owns the canonical state. The local structs are vestigial from before the comms.Handler refactor (GH-2143) but remain in the file, creating confusion about where authoritative state lives. The telegram-local task maps are never written to after the migration — only commsHandler.pendingTasks/runningTasks are used.
- [ ] **[low] commandHandler wired via 8 separate setter methods instead of a config struct** — `internal/comms/commands.go:28-76`
  - CommandHandler exposes eight individual SetXxxFunc() methods (SetRunCommandFunc, SetStatusQueryFunc, SetActiveProjectFunc, etc.) instead of accepting a config struct at construction time. This makes it easy to forget wiring a callback and produces nil-pointer silent failures (e.g. if runCommandFunc is nil, /run silently falls through to a usage message). A single CommandHandlerConfig struct would make missing fields visible at compile time.
- [ ] **[low] Discord GatewayClient.handleHello starts heartbeat goroutine without tracking it** — `internal/adapters/discord/transport.go:75-123`
  - heartbeatLoop() is launched as a bare go g.heartbeatLoop() in handleHello() without a WaitGroup or reference that would let Close() wait for it to exit. On a rapid connect/close cycle the old heartbeat goroutine can outlive the connection and attempt WriteJSON on a closed conn, causing a data race on g.conn under the mutex.

### 4.x Autopilot  (6)
- [ ] **[medium] Label manipulation duplicated across merge paths** — `internal/autopilot/controller_state_merge.go:74 and internal/autopilot/controller_external.go:19`
  - handleMerging and checkExternalMergeOrClose both execute the same sequence: AddLabels(pilot-done), RemoveLabel(pilot-in-progress), RemoveLabel(pilot-failed), UpdateIssueState(closed), AddComment(merge completion). This is ~25 lines duplicated with slight differences. A shared applyMergeLabels helper would eliminate the drift risk.
- [ ] **[medium] handleMerging is a 147-line god function** — `internal/autopilot/controller_state_merge.go:12`
  - The function handles merge attempt, conflict detection, attempt cap escalation, issue label cleanup, monitor state sync, self-heal, pattern reinforcement, board sync, branch deletion, and notification — 9 distinct responsibilities. Extracting postMergeIssueCleanup and postMergeNotifications would make each path testable in isolation.
- [ ] **[medium] Metrics in-memory counters not restored from SQLite on restart** — `internal/autopilot/metrics.go:44`
  - TokensConsumed, ExecutionCostUSD, and ExecutionsByResult are persisted per snapshot but the TODO(GH-2836) comment confirms RestoreFromRow is never called. Prometheus rate() queries tolerate resets, but cost dashboards lose history on every daemon restart. The hook point exists (metrics_persister.go) but is unwired.
- [ ] **[low] Duplicate version-fetch logic in Releaser** — `internal/autopilot/releaser.go:165 and releaser.go:287`
  - GetCurrentVersion and GetCurrentVersionForRepo are identical in body except for the owner/repo parameter. GetCurrentVersion is only called from tests; production always uses GetCurrentVersionForRepo. GetCurrentVersion should delegate to GetCurrentVersionForRepo(r.owner, r.repo) rather than duplicating 30 lines.
- [ ] **[low] ShouldWaitForCI always returns true** — `internal/autopilot/auto_merger.go:178`
  - ShouldWaitForCI(env Environment) takes an Environment parameter it ignores and unconditionally returns true. The parameter was meaningful when dev could skip CI; now it's dead code that misleads the reader. Remove the parameter and inline the constant.
- [ ] **[low] projectBoardSyncer and approvalPersister are unexported interface stubs tightly coupled to concrete types in controller_config.go** — `internal/autopilot/controller_config.go:13`
  - WithProjectBoardSync takes *github.ProjectBoardSync (concrete) and assigns to the projectBoardSyncer interface, but the ControllerOption signature leaks the concrete type to callers. All other optional components (Notifier, TaskMonitor, ExecutionHealer) use their interfaces throughout — WithProjectBoardSync should accept projectBoardSyncer for consistency and testability.

### 4.x Memory & Learning  (6)
- [ ] **[high] Dual pattern stores: GlobalPatternStore (JSON) and Store/CrossPattern (SQLite) hold overlapping data** — `internal/memory/extractor_save.go:91-108`
  - SaveExtractedPatterns dual-writes CI anti-patterns to both GlobalPatternStore (global_patterns.json) and SQLite cross_patterns. The two stores diverge over time, have different confidence update semantics, and callers must know which to query. PatternQueryService only reads SQLite; GetForProject only reads JSON. This split makes it impossible to have a single authoritative read path.
- [ ] **[high] migrate() uses a flat slice of raw SQL strings with no versioning** — `internal/memory/store_migrate.go:10-260`
  - All 30+ migrations run on every startup guarded only by 'duplicate column' error swallowing. There is no migration-version table, no rollback path, and the list will grow unboundedly. A failed mid-list migration leaves the schema partially applied with no way to tell how far it got.
- [ ] **[medium] GlobalPatternStore.saveUnlocked and OrgPatternStore.save use direct os.WriteFile (not atomic)** — `internal/memory/patterns.go:94 / internal/memory/sync.go:122`
  - KnowledgeGraph uses a temp-file+fsync+rename pattern to avoid corrupt JSON on crash. GlobalPatternStore and OrgPatternStore write directly to the target path, risking truncated/empty files if the process dies mid-write. With crash-recovery tests only covering KnowledgeGraph, data loss is silently possible for global_patterns.json and org_patterns.json.
- [ ] **[medium] findSimilarPattern uses O(n) title-equality scan on every Save** — `internal/memory/extractor_save.go:115-124`
  - On every call to SaveExtractedPatterns, findSimilarPattern iterates all patterns of the same type (GetByType returns an unsorted slice). As the GlobalPatternStore grows this becomes an O(n) linear scan per extracted pattern per execution. A map keyed on lowercased title would reduce this to O(1).
- [ ] **[low] LearningLoop.GetTopPerformingPatterns uses a bubble-sort loop instead of sort.Slice** — `internal/memory/feedback_performance.go:74-82`
  - The inner sort is implemented as a manual O(n²) bubble sort. sort.Slice (already imported elsewhere in the package) would be clearer and faster, especially as performance data grows to hundreds of patterns.
- [ ] **[low] CheckUsageThresholds has hardcoded $100 / 500-task limits with no config path** — `internal/memory/metering_thresholds.go:36-43`
  - The comment says 'would be configurable in production' but the literals are hardcoded. Any production budget enforcement that reads this function will silently ignore operator-configured limits. The UsageThreshold struct is defined but never populated or read.

### 4.x Gateway & Infra  (6)
- [ ] **[high] randomString in webhooks/event.go uses time.Sleep in a loop** — `internal/webhooks/event.go:108`
  - randomString calls time.Sleep(time.Nanosecond) for each byte to vary UnixNano, producing weak entropy and measurable latency. Under concurrent calls the nanosecond granularity can repeat. Should use crypto/rand.
- [ ] **[medium] Webhook handler duplication — 8 nearly-identical handler functions** — `internal/gateway/server_webhooks.go`
  - Every handler repeats the same io.ReadAll, JSON unmarshal, router.HandleWebhook pattern with only header names varying. A generic handleWebhook(source, sigHeader, verifyFn) adapter would reduce ~300 lines to ~50 and make future adapters trivial to add.
- [ ] **[medium] Server god struct with 17 fields and no sub-grouping** — `internal/gateway/server.go:70`
  - Server accumulates unrelated concerns: auth, sessions, router, Prometheus, alerts, autopilot, architect, dashboard store, log stream, git graph, codex runtime approvals, liveness. Each concern has its own mu-protected field with a Set* method, making the constructor and buildHandler harder to follow.
- [ ] **[medium] cloudflare.go URL detection parses stderr with brittle string contains** — `internal/tunnel/cloudflare.go:150`
  - The goroutine scanning cloudflared stderr looks for Contains(line, "Connection") && Contains(line, "registered") and also any line containing "error" or "failed" — this will false-positive on log messages that mention those words in context. A structured regex or the cloudflared --json flag would be more reliable.
- [ ] **[low] Prometheus metrics written with hand-rolled string formatting instead of SDK** — `internal/gateway/prometheus.go`
  - The manual formatLabels / escapeLabel / writeHistogram functions re-implement label escaping and bucket counting that the prometheus/client_golang library handles correctly and more efficiently. The hand-rolled escapeLabel uses string concatenation in a rune loop (O(n²)).
- [ ] **[low] runtimeSessionController has two overlapping close mechanisms (close/finish)** — `internal/gateway/codexruntime_ws_controller.go:93`
  - There are two separate Once-protected close operations (close() closes the stop channel; finish() closes the done channel) plus a closed bool field. The distinction is subtle, callers must call both in the right order, and the closed bool is checked separately from channel state — consolidating into a single lifecycle state machine would reduce potential for races.

### 4.x Dashboard & TUI  (6)
- [ ] **[medium] Duplicated panel rendering code: renderPanel vs renderOrangePanel (and the 4 orange build* helpers)** — `internal/dashboard/tui_panels.go:82-139`
  - renderOrangePanel, buildOrangeTopBorder, buildOrangeEmptyLine, buildOrangeContentLine, buildOrangeBottomBorder are exact copies of the default variants with only the style variable changed. A single renderPanel(title, content string, tw int, sty lipgloss.Style) signature would eliminate ~60 lines and keep color logic in one place.
- [ ] **[medium] renderGraphPanel in gitgraph_panel.go duplicates the panel border logic from tui_panels.go** — `internal/dashboard/gitgraph_panel.go:185-226`
  - renderGraphPanel rebuilds top border, empty line, content line, and bottom border from scratch (with focus-aware color) instead of reusing the primitives from tui_panels.go. Extracting a borderSet struct (the 5 style-parameterized build* functions) and passing it to both would unify the two implementations.
- [ ] **[medium] hydrateFromStore and storeRefreshCmd are near-identical execution sequences** — `internal/dashboard/tui_store.go:14-98 vs 147-210`
  - Both functions call the same three store queries (GetRecentExecutions, GetLifetimeTokens, GetLifetimeTaskCounts), apply identical mapping loops, and compute CostPerTask the same way. The only difference is hydrateFromStore writes to m directly while storeRefreshCmd populates a storeRefreshMsg. Extracting a loadStoreSnapshot(store) function returning the struct would remove ~60 lines of duplication.
- [ ] **[medium] Model struct is a god-object with 30+ fields spanning unrelated concerns** — `internal/dashboard/tui_types.go:170-234`
  - The Model struct conflates git graph state (gitGraphMode, gitGraphState, gitGraphScroll, gitGraphFocus, projectPath, defaultProjectPath, gitProjectName), upgrade state (updateInfo, upgradeState, upgradeProgress, upgradeMessage, upgradeError, upgradeCh), splash state (splashActive, splashFrame, splashStart, configPath), metrics (metricsCard, sparklineTick, shimmerTick), and banner metadata (bannerAdapters, activeAdapters, envName, modelStack, startTime). Sub-structs for each concern would improve readability and reduce the risk of Update() branches touching unrelated state.
- [ ] **[low] truncateString vs truncateVisual: two byte-level truncation functions with subtly different semantics** — `internal/dashboard/tui_autopilot.go:209-215 vs internal/dashboard/tui_panels.go:158-189`
  - truncateString (autopilot) truncates by byte length (len), while truncateVisual (panels) truncates by lipgloss visual width. The autopilot PR title truncation will mis-count multi-byte Unicode chars. All callers should use truncateVisual.
- [ ] **[low] desktop/app_metrics.go issueURL() is hardcoded to the ylcn91/pilot repo** — `desktop/app_helpers.go:19-24`
  - issueURL() constructs a GitHub URL with a hardcoded 'ylcn91/pilot' path, so the desktop app generates wrong issue links for any other fork or project. The repo path should come from config (config.Load) the same way gatewayURL is resolved in startup().

### 4.x Architect  (6)
- [ ] **[medium] ScanOptions struct carries 7 fields spanning two very different concerns (core scan config vs. memory/knowledge wiring)** — `internal/architect/scan_default.go:12`
  - Fields QualityRunner, MinCoverage, Signals are core scan knobs while FailureSource, FailureQuery, KnowledgeSource, SuggestRules, ProjectID are only used by the memory-backed and rule-suggestion paths. This single struct is passed through BuildLensScanner, coreCollectors, refactorCollectors, radarCollectors, and every lens Collectors factory. Splitting into ScanOptions + MemoryOptions (or using functional options) would keep the core lens's constructor surface clean.
- [ ] **[medium] IssueCreator / SubIssueCreator interface redeclaration leaks the github.Issue type into the emit boundary** — `internal/architect/emit.go:19 and :61`
  - IssueCreator.CreatePilotIssue() returns *github.Issue, which causes internal/architect to import internal/adapters/github solely for this return type. linearIssueCreator.CreatePilotIssue() constructs a stub *github.Issue containing only the Linear identifier. If IssueCreator returned a minimal struct (e.g. IssueRef{ID string, URL string}) instead of *github.Issue, the github import could be dropped from the architect package, improving layer hygiene.
- [ ] **[low] ChurnCollector and BugHistoryCollector are near-identical structs with identical Collect() bodies** — `internal/architect/collect_memory.go:26 and internal/architect/collect_bughistory.go:34`
  - Both structs hold (source failureSource, query memory.MetricsQuery, limit int, projectID string, threshold int), both Collect() bodies call source.GetFailureReasons(), filter by count >= threshold, and produce Signals with the same shape — differing only in Kind and weight/risk formulas. A shared genericChurnCollector with injected naming and risk/weight functions would eliminate the duplication without adding abstraction cost.
- [ ] **[low] commands_architect_rfc.go:enrichRFCWithBackend() re-calls SynthesizeFindings() and ProjectToEpic() a second time unnecessarily** — `cmd/pilot/commands_architect_rfc.go:99`
  - runArchitectRFC() calls SynthesizeFindings(signals) and ProjectToEpic() at line 35-36, then enrichRFCWithBackend() independently re-calls both at lines 115-116. The results are identical for the same inputs but the work is done twice. The plan and synthesized findings computed before the backend call should be passed in.
- [ ] **[low] Lens.Collectors factory signature discards projectPath for all lenses except none** — `internal/architect/lens.go:33`
  - Every lens's Collectors factory receives projectPath as its first argument, but no registered lens (core, depdoctor, refactor, rfc, radar, testgap) uses it — all pass `_ string`. The DepsCollector builds the graph inside Collect() using the projectPath passed to Collect(), not the one given to the factory. The factory signature could drop the argument, reducing noise at every lens registration site.
- [ ] **[low] RuleSuggester.Suggest() accumulates triggers into ruleEdge.triggers as a slice but never deduplicates until draftSignal()** — `internal/architect/rule_suggest.go:99`
  - The triggers slice in each ruleEdge can contain duplicate strings when the same Signal content matches both the pitfall and the violation patterns (e.g. a pitfall memory that also reads as a violation). dedupStrings() is called inside draftSignal() at render time, but the Weight (len(e.triggers)) is set before deduplication, so the weight over-counts duplicate triggers. The dedup should happen at accumulation time or the weight should use len(deduplicated triggers).

### 4.x Alerts, Approval, Teams, Budget  (6)
- [ ] **[high] Division-by-zero in Enforcer.GetStatus when limits are zero** — `internal/budget/enforcer.go:156-159`
  - Lines 156-159 compute DailyPercent = (cost / e.config.DailyLimit) * 100 and MonthlyPercent = (cost / e.config.MonthlyLimit) * 100 with no guard. If either limit is 0.0 (disabled budget, misconfigured YAML), both values become NaN or +Inf, which propagates silently into CheckResult and threshold comparisons.
- [ ] **[medium] Unsanitized alert title/message written into HTML email body** — `internal/alerts/channels_email.go:99-100`
  - alert.Title and alert.Message are interpolated directly into an HTML string via fmt.Sprintf without html.EscapeString. An attacker who controls alert metadata (e.g. via a crafted task title) can inject arbitrary HTML/script tags into the alert email.
- [ ] **[medium] Non-deterministic fallback handler selection in SubmitApprovalRequest** — `internal/approval/manager.go:247-256`
  - When PreferredChannel is unset or not registered and multiple handlers exist, the fallback iterates a map (Go randomises iteration order), so the selected handler is random per process run. This can silently route approvals to the wrong channel in multi-handler deployments.
- [ ] **[medium] ResolveSlackIdentity silently ignores the slackUserID argument** — `internal/teams/service_identity.go:73-75`
  - The slack_user_id column and GetMembersBySlackUserID store method already exist (GH-783), but ResolveSlackIdentity discards slackUserID with _ = slackUserID and only resolves by email. This means Slack identity resolution silently degrades to email-only for all callers.
- [ ] **[low] Flat ALTER TABLE migration list is not idempotent by design** — `internal/teams/store.go:67-80`
  - New columns are added via bare ALTER TABLE statements. The error is caught with a string-match on 'duplicate column', which is SQLite-specific and will silently fail to apply a migration if the error message changes. A proper version-numbered migration table would be safer.
- [ ] **[low] handleTaskFailed iterates all rules in O(rules) per event with three nested type-switches** — `internal/alerts/engine_handlers.go:83-133`
  - handleTaskFailed, handleCostUpdate, and handleSecurityEvent each iterate the full rules slice and switch on rule.Type. With many rules this is O(n) per event per handler. Pre-indexing rules by AlertType at engine construction (map[AlertType][]AlertRule) would reduce per-event work and eliminate the repetitive pattern across all five handler functions.

### 4.x CLI, Config & Wiring  (6)
- [ ] **[high] Dead code: prepareStart / startContext** — `cmd/pilot/start_cmd_config.go:38`
  - prepareStart replicates the entire config-load, flag-validation, logging-init, path-resolution, and mode-selection sequence that newStartCmd RunE already performs inline. Neither prepareStart nor startContext is called anywhere. The 185-line file is pure dead code that will diverge from the real path on the next change.
- [ ] **[high] Duplicated ProcessXxxTicket methods in Orchestrator** — `internal/orchestrator/orchestrator_tickets.go:18`
  - ProcessTicket, ProcessGithubTicket, ProcessGitlabTicket, ProcessJiraTicket, ProcessAsanaTicket, and ProcessPlaneTicket share an identical body (normalize to TicketData → bridge.PlanTicket → saveTaskDocument → QueueTask). Only the input struct and field names differ. A single func processGenericTicket(ctx, ticket *TicketData, projectPath, branch string) would remove ~180 lines of copy-paste.
- [ ] **[medium] config.go imports all adapter packages for DefaultConfig()** — `internal/config/config.go:12-31`
  - config imports 11 adapter packages (asana, azuredevops, bitbucket, discord, github, gitlab, jira, linear, plane, slack, telegram) plus autopilot, budget, executor, gateway, logging, quality, tunnel, webhooks purely to call their DefaultConfig() constructors. This makes config a de-facto god dependency — any change to any adapter package rebuilds config. Moving adapter default construction to the adapter packages themselves and using interface/factory registration would break this coupling.
- [ ] **[medium] Global teamAdapter mutable shared state** — `cmd/pilot/main.go:17`
  - teamAdapter *teams.ServiceAdapter is a package-level global mutated in at least three places: start_polling_stores.go:87, start_cmd_gateway.go:119, and read in handlers.go:161, start_cmd.go:242/256, start_polling_telegram.go:65, start_polling_adapters.go:60. This is unsafefor concurrent access and makes the wiring hard to test in isolation. The adapter should be passed as a dependency through pollingRuntime and gatewayInfra fields.
- [ ] **[medium] Python subprocess bridge with no fallback or healthcheck** — `internal/orchestrator/bridge.go:21`
  - NewBridge calls exec.LookPath for python3/python at startup and stores the path. bridge.PlanTicket launches a fresh subprocess per ticket with no timeout (context is passed but the subprocess has no deadline), no retry, and no circuit-breaker. A slow or missing Python installation silently hangs the task queue workers rather than returning a fast, surfaceable error.
- [ ] **[low] buildGatewayInfra is a 341-line God function** — `cmd/pilot/start_cmd_gateway.go:56`
  - buildGatewayInfra constructs the runner, quality gates, teams RBAC, knowledge store, log store, approval manager, Telegram/Slack/GitHub approval handlers, autopilot controller with board sync and guardrails, autopilot state store with crash recovery, and alerts engine — all in one function with nested if-chains. Extracting each concern (approval setup, autopilot setup, alerts setup) into focused builder functions would match the pattern already established by buildGatewayAlertsEngine.

### 4.x Replay, Briefs, Upgrade, Logging, Budget  (6)
- [ ] **[high] Duplicate HTML export implementations** — `internal/replay/player_export.go vs internal/replay/export_html.go`
  - ExportToHTML() in player_export.go and ExportHTMLReport() in export_html.go both emit full standalone HTML documents with embedded CSS and event rendering. They serve different call sites but share no code, so any styling or escaping fix must be applied twice. The CSS strings in export_styles.go are only referenced by one of them.
- [ ] **[medium] Recorder.parseEvent() silently drops all but the last content block** — `internal/replay/recorder_parse.go:49`
  - The loop over message.content blocks unconditionally overwrites parsed.ToolName and parsed.Text on each iteration, so only the last block survives. A message with both a text block and a tool_use block loses the text silently, causing the file-operation tracker to miss files in multi-block turns.
- [ ] **[medium] Scheduler.maybeCatchUp() hardcoded to 'telegram' channel** — `internal/briefs/scheduler.go:213`
  - store.GetLastBriefSent('telegram') is used as the universal 'was a brief sent?' signal regardless of configured channels. If Slack or email is the only channel, a spurious catch-up fires on every restart. The channel to query should be derived from config or the store query should check any channel.
- [ ] **[medium] DeliveryService.deliverSlack block conversion uses unchecked type assertions** — `internal/briefs/delivery.go:134`
  - Converting the []map[string]any returned by SlackFormatter.SlackBlocks() to slack.Block relies on direct type assertions (b['type'].(string)) without ok-checks, causing a panic on any unexpected block shape. The SlackFormatter should return typed slack.Block values directly to eliminate the unsafe conversion.
- [ ] **[low] truncate() and formatDuration() duplicated across packages** — `internal/replay/player_format.go:148 vs internal/replay/export.go:62 vs internal/briefs/formatter.go`
  - A formatDuration(ms int64) exists in briefs/formatter.go and a near-identical formatDuration(d time.Duration) in replay/export.go; truncate() is defined in player_format.go and also inlined in player_analyzer.go. An internal/text package already exists in this repo and is the right home for these utilities.
- [ ] **[low] Enforcer.GetStatus() acquires two separate mutex sections for read then write** — `internal/budget/enforcer.go:120`
  - GetStatus() takes RLock to read paused/pauseReason/blockedTasks, releases it, then takes a full Lock at line 166 to write lastStatus. Between the two sections the paused flag can change, making the returned Status inconsistent. Both sections should be merged under a single Lock.

---

## Ek — Dead-code / tekilleştirme (test DEĞİL — sil/birleştir)

- [ ] `internal/executor/runner_stream.go` — runner.parseStreamEvent dead code (processBackendEvent sonrası non-test çağrısı yok); testi unreachable olduğunu maskeliyor. Sil, test yazma.
- [ ] `internal/executor/runner_execute_pr.go:26,85` — executeLintPushPR duplicate lint-gate çağrısı; test değil, tekilleştir.

---

**Toplam:** Faz1 4 bug · Faz2 1 test · Faz3 8+92 test gap · Faz4 6+84 refactor.
