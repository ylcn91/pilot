import Combine
import AppKit
import Foundation

struct WorkspaceDiffPreview: Equatable {
    var path: String = ""
    var body: String = ""
    var commandLine: String = ""
    var exitCode: Int32?
    var updatedAt: Date?

    var hasDiff: Bool {
        !body.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

@MainActor
final class AppStore: ObservableObject {
    @Published var settings = AppSettings()
    @Published var selectedSection: SidebarSection = .workspaceConsole
    @Published var selectedTaskID: QueueTask.ID?
    @Published var metrics = DashboardMetrics()
    @Published var queue: [QueueTask] = []
    @Published var history: [HistoryEntry] = []
    @Published var logs: [LogEntry] = []
    @Published var autopilot = AutopilotStatus()
    @Published var findings: [Finding] = []
    @Published var gitGraph = GitGraphData()
    @Published var serverRunning = false
    @Published var serverStatus: ServerStatus?
    @Published var lastError: String?
    @Published var commandRuns: [CommandRun] = []
    @Published var workspace = WorkspaceSnapshot()
    @Published var projectWorkspaces: [WorkspaceListItem] = []
    @Published var selectedWorkspaceFilePath: String?
    @Published var selectedWorkspaceDiff = WorkspaceDiffPreview()
    @Published var workspaceRuns: [CommandRun] = []
    @Published var isRefreshingWorkspace = false
    @Published var isLoadingWorkspaceDiff = false
    @Published var isCreatingWorkspace = false
    @Published var isAutopilotTaskRunning = false
    @Published var isRefreshing = false
    @Published var runtime = RuntimeStore()
    @Published var agentChat = AgentChatSession()
    @Published var providerStatuses: [ProviderStatus] = AgentChatProvider.allCases.map { ProviderStatus.placeholder(for: $0) }

    private var settingsCancellable: AnyCancellable?
    private var agentChatCancellable: AnyCancellable?

    init() {
        settingsCancellable = settings.objectWillChange.sink { [weak self] _ in
            self?.objectWillChange.send()
        }
        agentChatCancellable = agentChat.objectWillChange.sink { [weak self] _ in
            self?.objectWillChange.send()
        }
        providerStatuses = Self.initialProviderStatuses()
    }

    var selectedTask: QueueTask? {
        guard let selectedTaskID else { return nil }
        return queue.first { $0.id == selectedTaskID }
    }

    func refreshAll() async {
        isRefreshing = true
        defer { isRefreshing = false }
        let client = gatewayClient()

        var errors: [String] = []

        do { serverRunning = try await client.health() } catch {
            serverRunning = false
            errors.append(error.localizedDescription)
        }
        do { serverStatus = try await client.status() } catch { errors.append(error.localizedDescription) }
        do { metrics = try await client.metrics() } catch { errors.append(error.localizedDescription) }
        do { queue = try await client.queue() } catch { errors.append(error.localizedDescription) }
        do { history = try await client.history(limit: 20) } catch { errors.append(error.localizedDescription) }
        do { logs = try await client.logs(limit: 80) } catch { errors.append(error.localizedDescription) }
        do { autopilot = try await client.autopilot() } catch { errors.append(error.localizedDescription) }
        do { findings = try await client.architectFindings() } catch { errors.append(error.localizedDescription) }
        do { gitGraph = try await client.gitGraph(limit: 40) } catch { errors.append(error.localizedDescription) }
        refreshRecentRecordings()
        await refreshProviderStatuses()
        await refreshWorkspace()
        await refreshProjectWorkspaces()

        if errors.isEmpty {
            lastError = nil
        } else {
            lastError = errors.first
        }
    }

    func providerStatus(for provider: AgentChatProvider) -> ProviderStatus {
        providerStatuses.first { $0.provider == provider } ?? ProviderStatus.placeholder(for: provider)
    }

    func accountStatus(for account: AgentAccount) -> ProviderStatus {
        let providerStatus = providerStatus(for: account.provider)
        let explicitExecutable = expandAccountPath(account.executablePath)
        let executablePath = explicitExecutable.isEmpty ? providerStatus.executablePath : explicitExecutable
        let configuredPath = account.configPath.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            ? providerStatus.configPath
            : account.configPath
        let expandedConfigPath = expandAccountPath(configuredPath)
        let configExists = !expandedConfigPath.isEmpty && FileManager.default.fileExists(atPath: expandedConfigPath)
        let defaultConfigPath = expandAccountPath(AgentAccount.defaultConfigPath(for: account.provider))
        let hasAccountSpecificContext = expandedConfigPath != defaultConfigPath
            || !account.environment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            || !account.profileName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty

        let connected: Bool
        switch account.provider {
        case .claudeCode:
            connected = hasAccountSpecificContext ? configExists : (providerStatus.connected || configExists)
        case .codex:
            let authPath = codexAuthPath(forConfigPath: expandedConfigPath)
            let accountAuthExists = authPath.map { FileManager.default.fileExists(atPath: $0) } ?? false
            connected = hasAccountSpecificContext ? accountAuthExists : (providerStatus.connected || accountAuthExists)
        }

        let headline: String
        if connected {
            headline = "Connected"
        } else if hasAccountSpecificContext && !configExists {
            headline = "Config missing"
        } else {
            headline = "CLI not authenticated"
        }

        return ProviderStatus(
            provider: account.provider,
            connected: connected,
            headline: headline,
            authMethod: account.authMethod,
            executablePath: executablePath,
            configPath: configuredPath,
            loginCommand: account.loginCommand,
            detailRows: [
                ProviderDetailRow(label: "Account", value: account.name),
                ProviderDetailRow(label: "Provider", value: account.provider.label),
                ProviderDetailRow(label: "Plan", value: account.plan),
                ProviderDetailRow(label: "Scope", value: account.scope),
                ProviderDetailRow(label: "Org", value: account.organization),
                ProviderDetailRow(label: "Default model", value: account.defaultModel),
                ProviderDetailRow(label: "Executable", value: executablePath.isEmpty ? "auto" : executablePath),
                ProviderDetailRow(label: "Config", value: configuredPath.isEmpty ? "not set" : configuredPath),
                ProviderDetailRow(label: "Config status", value: configExists ? "exists" : "missing"),
                ProviderDetailRow(label: "Profile", value: account.profileName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "default" : account.profileName),
                ProviderDetailRow(label: "Environment", value: account.environment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "inherit" : "custom"),
                ProviderDetailRow(label: "Login", value: account.loginCommand),
                ProviderDetailRow(label: "Detected provider", value: providerStatus.headline)
            ]
        )
    }

    func refreshProviderStatuses() async {
        let shell = ShellCommand(workingDirectory: settings.projectPath)
        let claudePath = cleanOutput((await shell.run(Self.detectClaudeExecutableCommand, title: "claude-path")).output)
        let claudeConfig = cleanOutput((await shell.run("for p in \"$HOME/.claude/settings.json\" \"$HOME/.claude.json\"; do [ -f \"$p\" ] && echo \"$p\" && break; done", title: "claude-config")).output)
        let codexPath = cleanOutput((await shell.run(Self.detectCodexExecutableCommand, title: "codex-path")).output)
        let codexConfig = cleanOutput((await shell.run("for p in \"$HOME/.codex/config.toml\" \"$HOME/.codex/settings.json\"; do [ -f \"$p\" ] && echo \"$p\" && break; done", title: "codex-config")).output)
        let codexLogin = cleanOutput((await shell.run("\(codexPath.isEmpty ? "codex" : shellQuote(codexPath)) login status 2>&1 || true", title: "codex-login")).output)

        providerStatuses = [
            claudeStatus(executablePath: claudePath, configPath: claudeConfig),
            codexStatus(executablePath: codexPath, configPath: codexConfig, loginStatus: codexLogin)
        ]
    }

    func runProviderLogin(_ provider: AgentChatProvider) async {
        let status = providerStatus(for: provider)
        let command: String
        switch provider {
        case .claudeCode:
            command = status.executablePath.isEmpty ? "claude /login" : "\(shellQuote(status.executablePath)) /login"
        case .codex:
            command = status.executablePath.isEmpty ? "codex login" : "\(shellQuote(status.executablePath)) login"
        }
        let run = await ShellCommand(workingDirectory: settings.projectPath).run(command, title: "\(provider.label) login")
        commandRuns.insert(run, at: 0)
        await refreshProviderStatuses()
    }

    func runAccountLogin(_ account: AgentAccount) async {
        let configured = account.loginCommand.trimmingCharacters(in: .whitespacesAndNewlines)
        let baseCommand = configured.isEmpty || configured == AgentAccount.defaultLoginCommand(for: account.provider)
            ? resolvedLoginCommand(for: account)
            : configured
        let command = accountLoginEnvironmentPrefix(account) + baseCommand
        let run = await ShellCommand(workingDirectory: settings.projectPath).run(command, title: "\(account.name) login")
        commandRuns.insert(run, at: 0)
        workspaceRuns.insert(run, at: 0)
        await refreshProviderStatuses()
    }

    func openProviderConfig(_ provider: AgentChatProvider) async {
        let status = providerStatus(for: provider)
        let path = status.configPath.replacingOccurrences(of: "~", with: FileManager.default.homeDirectoryForCurrentUser.path)
        guard !path.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        let run = await ShellCommand(workingDirectory: settings.projectPath).run("open \(shellQuote(path))", title: "\(provider.label) config")
        commandRuns.insert(run, at: 0)
    }

    func openAccountConfig(_ account: AgentAccount) async {
        let rawPath = account.configPath.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            ? AgentAccount.defaultConfigPath(for: account.provider)
            : account.configPath
        let path = rawPath.replacingOccurrences(of: "~", with: FileManager.default.homeDirectoryForCurrentUser.path)
        guard !path.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        let run = await ShellCommand(workingDirectory: settings.projectPath).run("open \(shellQuote(path))", title: "\(account.name) config")
        commandRuns.insert(run, at: 0)
        workspaceRuns.insert(run, at: 0)
    }

    func connectRuntime() {
        runtime.connect(gatewayURL: settings.gatewayURL)
    }

    func runCommand(_ command: PilotCommandDefinition, rawArguments: String, prompt: String) async {
        let cli = PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath)
        let run: CommandRun
        switch command.inputStyle {
        case .prompt:
            let promptArgument = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
            let fullArguments = ([command.name] + splitShellLike(rawArguments) + (promptArgument.isEmpty ? [] : [promptArgument]))
            run = await cli.run(arguments: fullArguments)
        case .arguments, .none:
            run = await cli.runRaw("\(command.name) \(rawArguments)", title: command.name)
        }
        commandRuns.insert(run, at: 0)
    }

    @discardableResult
    func switchBackend(_ backend: ExecutionBackend) async -> CommandRun? {
        let args = backendConfigSetArguments(backend)
        guard !args.isEmpty else {
            settings.selectedBackend = backend
            return nil
        }
        let cli = PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath)
        let run = await cli.run(arguments: args)
        commandRuns.insert(run, at: 0)
        workspaceRuns.insert(run, at: 0)
        if run.exitCode == 0 {
            settings.selectedBackend = backend
        }
        return run
    }

    private func backendConfigSetArguments(_ backend: ExecutionBackend) -> [String] {
        var args = ["config", "set", "executor.type", backend.rawValue]
        switch backend {
        case .claudeCode:
            args += ["executor.default_model", backendDefaultModel(for: .claudeCode)]
            if let command = backendExecutable(for: .claudeCode), !command.isEmpty {
                args += ["executor.claude_code.command", command]
            }
        case .codexExec:
            args += [
                "executor.default_model", backendDefaultModel(for: .codex),
                "executor.codex_exec.bypass_approvals_and_sandbox", "true",
            ]
            if let command = backendExecutable(for: .codex), !command.isEmpty {
                args += ["executor.codex_exec.command", command]
            }
        case .opencode:
            break
        case .codexAppServer:
            return []
        }
        return args
    }

    private func backendDefaultModel(for provider: AgentChatProvider) -> String {
        if settings.selectedAgentAccount.provider == provider, !settings.selectedAgentAccount.defaultModel.isEmpty {
            return settings.selectedAgentAccount.defaultModel
        }
        return settings.agentAccounts.first { $0.provider == provider && !$0.defaultModel.isEmpty }?.defaultModel
            ?? provider.defaultModel
    }

    private func backendExecutable(for provider: AgentChatProvider) -> String? {
        if settings.selectedAgentAccount.provider == provider {
            let explicit = expandAccountPath(settings.selectedAgentAccount.executablePath)
            if !explicit.isEmpty {
                return explicit
            }
        }
        return providerStatus(for: provider).executablePath.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func ensureTaskBackendConfigured() async -> ExecutionBackend? {
        let backend = settings.selectedBackend == .codexAppServer ? .codexExec : settings.selectedBackend
        if settings.selectedBackend == .codexAppServer {
            settings.selectedBackend = backend
        }
        guard let run = await switchBackend(backend) else {
            return backend
        }
        guard run.exitCode == 0 else {
            let output = [run.output, run.stderr]
                .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
                .filter { !$0.isEmpty }
                .joined(separator: "\n")
            lastError = output.isEmpty ? "Backend configuration failed for \(backend.label)." : output
            return nil
        }
        return backend
    }

    func runTask(description: String, local: Bool = false) async {
        guard await ensureTaskBackendConfigured() != nil else { return }
        let args = ["task", "--project", settings.projectPath, "--verbose"] + (local ? ["--local"] : []) + [description]
        let run = await PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath).run(arguments: args)
        commandRuns.insert(run, at: 0)
        await refreshAll()
    }

    func runAutopilotLocalTask(description: String) async {
        let taskDescription = description.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            ? "Run a local autopilot-safe workspace verification task. Inspect current changes, run the smallest relevant checks, and report risks without opening a PR."
            : description
        isAutopilotTaskRunning = true
        defer { isAutopilotTaskRunning = false }

        guard await ensureTaskBackendConfigured() != nil else { return }
        let cli = PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath)

        let statusRun = await cli.run(arguments: ["autopilot", "status", "--json"])
        commandRuns.insert(statusRun, at: 0)
        workspaceRuns.insert(statusRun, at: 0)

        if statusRun.output.contains("autopilot not enabled") || statusRun.output.contains("\"enabled\": false") {
            let enableRun = await cli.run(arguments: ["autopilot", "enable", "--env", "dev", "--json"])
            commandRuns.insert(enableRun, at: 0)
            workspaceRuns.insert(enableRun, at: 0)

            let refreshedStatusRun = await cli.run(arguments: ["autopilot", "status", "--json"])
            commandRuns.insert(refreshedStatusRun, at: 0)
            workspaceRuns.insert(refreshedStatusRun, at: 0)
        }

        let taskArgs = ["task", "--project", settings.projectPath, "--verbose", "--local", "--skip-self-review", taskDescription]
        let commandLine = ([settings.pilotCommand] + taskArgs.map(shellQuote)).joined(separator: " ")
        let runID = beginTrackedWorkspaceRun(title: "autopilot-task", commandLine: commandLine)
        let taskRun = await cli.run(
            arguments: taskArgs,
            onOutput: { [weak self] output in
                Task { @MainActor in
                    self?.updateTrackedWorkspaceRun(id: runID, output: output)
                }
            }
        )
        finishTrackedWorkspaceRun(id: runID, with: taskRun)
        await refreshAll()
    }

    func runArchitectScan(lens: String = "", json: Bool = true) async {
        var args = ["architect", "--dry-run"]
        if json {
            args.append("--json")
        }
        let trimmedLens = lens.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmedLens.isEmpty {
            args += ["--lens", trimmedLens]
        }
        let run = await PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath).run(arguments: args)
        commandRuns.insert(run, at: 0)
        workspaceRuns.insert(run, at: 0)
        await refreshAll()
    }

    func scheduleArchitectRadar(schedule: String, timezone: String, backend: ExecutionBackend? = nil) async {
        var args = [
            "config", "set",
            "architect.enabled", "true",
            "architect.schedule", schedule,
            "architect.timezone", timezone
        ]
        if let backend, backend != .codexAppServer {
            args += ["architect.backend.type", backend.rawValue]
        }
        let cli = PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath)
        let saveRun = await cli.run(arguments: args)
        commandRuns.insert(saveRun, at: 0)
        workspaceRuns.insert(saveRun, at: 0)

        let validateRun = await cli.run(arguments: ["config", "validate"])
        commandRuns.insert(validateRun, at: 0)
        workspaceRuns.insert(validateRun, at: 0)

        let showRun = await cli.run(arguments: ["config", "show", "--json"])
        commandRuns.insert(showRun, at: 0)
        workspaceRuns.insert(showRun, at: 0)
        await refreshAll()
    }

    func runCodexChat(prompt: String) async {
        let args = ["chat", "--cwd", settings.projectPath, "--sandbox", settings.sandbox.rawValue]
            + (settings.selectedModel.isEmpty ? [] : ["--model", settings.selectedModel])
            + [prompt]
        let run = await PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath).run(arguments: args)
        commandRuns.insert(run, at: 0)
    }

    func runPilotArguments(_ rawArguments: String, title: String) async {
        let run = await PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath).runRaw(rawArguments, title: title)
        commandRuns.insert(run, at: 0)
    }

    func startDaemon() async {
        let logPath = "/tmp/pilot91-daemon.log"
        let raw = "start --project \(shellQuote(settings.projectPath)) > \(shellQuote(logPath)) 2>&1 & echo $!"
        let run = await PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath).runRaw(raw, title: "start")
        commandRuns.insert(run, at: 0)
        try? await Task.sleep(nanoseconds: 1_000_000_000)
        await refreshAll()
    }

    func stopDaemon() async {
        let run = await PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath).run(arguments: ["stop"])
        commandRuns.insert(run, at: 0)
        await refreshAll()
    }

    func runDoctor() async {
        let run = await PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath).run(arguments: ["doctor"])
        commandRuns.insert(run, at: 0)
    }

    func runStatus() async {
        let run = await PilotCLI(pilotCommand: settings.pilotCommand, workingDirectory: settings.projectPath).run(arguments: ["status"])
        commandRuns.insert(run, at: 0)
        await refreshAll()
    }

    func refreshWorkspace() async {
        isRefreshingWorkspace = true
        defer { isRefreshingWorkspace = false }

        let shell = ShellCommand(workingDirectory: settings.projectPath)
        let rootRun = await shell.run("git rev-parse --show-toplevel 2>/dev/null || pwd", title: "repo-root")
        let branchRun = await shell.run("git branch --show-current 2>/dev/null || true", title: "branch")
        let baseRun = await shell.run("git symbolic-ref refs/remotes/origin/HEAD --short 2>/dev/null | sed 's#^origin/##' || true", title: "base-branch")
        let statusRun = await shell.run("git status --short 2>/dev/null || true", title: "status")
        let diffRun = await shell.run("git diff --shortstat HEAD 2>/dev/null || true", title: "diff-stat")
        let numstatRun = await shell.run("(git diff --numstat 2>/dev/null; git diff --cached --numstat 2>/dev/null) || true", title: "numstat")
        let prRun = await shell.run("command -v gh >/dev/null && gh pr view --json number,title,url,state,reviewDecision,statusCheckRollup 2>/dev/null || true", title: "pr")

        let files = parseWorkspaceFiles(status: statusRun.output, numstat: numstatRun.output)
        let pr = parseWorkspacePullRequest(prRun.output)
        workspace = WorkspaceSnapshot(
            repoRoot: cleanOutput(rootRun.output),
            branch: cleanOutput(branchRun.output),
            baseBranch: cleanOutput(baseRun.output).isEmpty ? "dev" : cleanOutput(baseRun.output),
            diffStat: cleanOutput(diffRun.output),
            statusSummary: workspaceStatusSummary(files),
            changedFiles: files,
            pullRequest: pr?.pullRequest,
            checks: pr?.checks ?? [],
            lastRefreshedAt: Date()
        )
        if let selectedWorkspaceFilePath {
            let root = workspace.repoRoot.isEmpty ? settings.projectPath : workspace.repoRoot
            var isDirectory: ObjCBool = false
            let exists = FileManager.default.fileExists(atPath: "\(root)/\(selectedWorkspaceFilePath)", isDirectory: &isDirectory)
            if !exists || isDirectory.boolValue {
                self.selectedWorkspaceFilePath = nil
            }
        }
        if selectedWorkspaceFilePath == nil {
            focusPrimaryWorkspaceChange(force: true)
        }
        if let selectedWorkspaceFilePath {
            await refreshWorkspaceDiff(for: selectedWorkspaceFilePath)
        } else {
            selectedWorkspaceDiff = WorkspaceDiffPreview()
        }
        await refreshProjectWorkspaces()
    }

    func selectWorkspaceFile(_ relativePath: String) {
        selectedWorkspaceFilePath = relativePath
        Task {
            await refreshWorkspaceDiff(for: relativePath)
        }
    }

    func refreshWorkspaceDiff(for relativePath: String) async {
        let path = relativePath.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !path.isEmpty, !path.hasSuffix("/") else {
            selectedWorkspaceDiff = WorkspaceDiffPreview()
            return
        }

        isLoadingWorkspaceDiff = true
        defer { isLoadingWorkspaceDiff = false }

        let root = workspace.repoRoot.isEmpty ? settings.projectPath : workspace.repoRoot
        let quotedPath = shellQuote(path)
        let command = """
        if git ls-files --error-unmatch \(quotedPath) >/dev/null 2>&1; then
          git diff --color=never --no-ext-diff HEAD -- \(quotedPath)
        else
          git diff --color=never --no-ext-diff --no-index -- /dev/null \(quotedPath) 2>/dev/null || true
        fi
        """
        let run = await ShellCommand(workingDirectory: root).run(command, title: "diff \(path)")

        guard selectedWorkspaceFilePath == path else { return }
        selectedWorkspaceDiff = WorkspaceDiffPreview(
            path: path,
            body: cleanOutput(run.output),
            commandLine: run.commandLine,
            exitCode: run.exitCode,
            updatedAt: Date()
        )
    }

    func refreshProjectWorkspaces() async {
        let sourceRoot = canonicalRepositoryRoot(from: settings.projectPath)
        let run = await ShellCommand(workingDirectory: sourceRoot).run("git worktree list --porcelain 2>/dev/null || true", title: "worktrees")
        projectWorkspaces = parseWorkspaceList(output: run.output, currentPath: settings.projectPath)
    }

    func createWorkspaceCopy() async {
        guard !isCreatingWorkspace else { return }
        isCreatingWorkspace = true
        defer { isCreatingWorkspace = false }

        let currentRoot = settings.projectPath
        let sourceRoot = canonicalRepositoryRoot(from: currentRoot)
        let projectName = workspaceProjectName(from: currentRoot)
        let workspaceName = uniqueWorkspaceName(projectName: projectName)
        let worktreesRoot = "\(sourceRoot)/.Codex/worktrees"
        let workspacePath = "\(worktreesRoot)/\(workspaceName)"
        let branchName = "ylcn91/\(workspaceName)"
        let shell = ShellCommand(workingDirectory: sourceRoot)
        let baseRun = await shell.run("git symbolic-ref refs/remotes/origin/HEAD --short 2>/dev/null | sed 's#^origin/##' || git branch --show-current", title: "workspace-base")
        let base = cleanOutput(baseRun.output).isEmpty ? "HEAD" : cleanOutput(baseRun.output)
        let command = [
            "mkdir -p \(shellQuote(worktreesRoot))",
            "git worktree add -b \(shellQuote(branchName)) \(shellQuote(workspacePath)) \(shellQuote(base))"
        ].joined(separator: " && ")
        let run = await shell.run(command, title: "new-workspace")
        commandRuns.insert(run, at: 0)
        workspaceRuns.insert(run, at: 0)
        if run.exitCode == 0 {
            settings.projectPath = workspacePath
            selectedWorkspaceFilePath = nil
            agentChat.clear()
            selectedSection = .workspaceConsole
            await refreshWorkspace()
            await refreshProjectWorkspaces()
        }
    }

    func openProjectFolder() async {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Open"
        panel.message = "Choose a project or repository folder for Pilot 91."

        guard panel.runModal() == .OK, let url = panel.url else { return }
        await openProjectPath(url.standardizedFileURL.path)
    }

    func openProjectPath(_ path: String) async {
        let cleanPath = standardizedPath(path)
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: cleanPath, isDirectory: &isDirectory), isDirectory.boolValue else { return }

        settings.projectPath = cleanPath
        selectedWorkspaceFilePath = nil
        selectedWorkspaceDiff = WorkspaceDiffPreview()
        agentChat.clear()
        selectedSection = .workspaceConsole
        await refreshWorkspace()
        await refreshProjectWorkspaces()
    }

    func switchWorkspace(_ workspace: WorkspaceListItem) async {
        guard FileManager.default.fileExists(atPath: workspace.path) else { return }
        settings.projectPath = workspace.path
        selectedWorkspaceFilePath = nil
        agentChat.clear()
        selectedSection = .workspaceConsole
        await refreshWorkspace()
        await refreshProjectWorkspaces()
    }

    func runWorkspaceCommand(_ command: String, title: String? = nil) async {
        let trimmed = command.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        let runID = beginTrackedWorkspaceRun(title: title ?? trimmed, commandLine: trimmed)
        let run = await ShellCommand(workingDirectory: settings.projectPath).run(
            trimmed,
            title: title ?? trimmed,
            onOutput: { [weak self] output in
                Task { @MainActor in
                    self?.updateTrackedWorkspaceRun(id: runID, output: output)
                    self?.focusWorkspaceFileMentioned(in: output)
                }
            }
        )
        focusWorkspaceFileMentioned(in: [run.output, run.stderr].joined(separator: "\n"))
        finishTrackedWorkspaceRun(id: runID, with: run)
        await refreshWorkspace()
        focusPrimaryWorkspaceChange(force: false)
    }

    func runWorkspacePrompt(_ prompt: String) async {
        let trimmed = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        await agentChat.send(
            prompt: trimmed,
            account: settings.selectedAgentAccount,
            model: settings.selectedChatModel,
            cwd: settings.projectPath,
            sandbox: settings.sandbox,
            pilotCommand: settings.pilotCommand,
            onOutput: { [weak self] output in
                self?.focusWorkspaceFileMentioned(in: output)
            }
        )
        await refreshWorkspace()
        focusPrimaryWorkspaceChange(force: false)
    }

    func runWorkspaceTask(_ prompt: String) async {
        let trimmed = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, !agentChat.isRunning else { return }
        guard let backend = await ensureTaskBackendConfigured() else { return }

        agentChat.beginRun(
            prompt: trimmed,
            provider: settings.selectedAgentAccount.provider,
            model: backend.label
        )

        let args = [
            "task",
            "--project",
            settings.projectPath,
            "--verbose",
            "--local",
            "--skip-self-review",
            trimmed
        ]
        let commandLine = ([settings.pilotCommand] + args.map(shellQuote)).joined(separator: " ")
        let runID = beginTrackedWorkspaceRun(title: "task", commandLine: commandLine)
        let run = await ShellCommand(workingDirectory: settings.projectPath).run(
            commandLine,
            title: "task",
            onOutput: { [weak self] output in
                Task { @MainActor in
                    self?.updateTrackedWorkspaceRun(id: runID, output: output)
                    self?.focusWorkspaceFileMentioned(in: output)
                    self?.agentChat.updateRunningAssistant(body: self?.workspaceTaskLiveSummary(output, backend: backend) ?? "Task is running.")
                }
            }
        )
        focusWorkspaceFileMentioned(in: [run.output, run.stderr].joined(separator: "\n"))
        finishTrackedWorkspaceRun(id: runID, with: run)

        await refreshAll()
        focusPrimaryWorkspaceChange(force: false)
        agentChat.finishRun(body: workspaceTaskChatSummary(run, backend: backend), exitCode: run.exitCode)
    }

    private func beginTrackedWorkspaceRun(title: String, commandLine: String) -> UUID {
        let run = CommandRun(
            title: title,
            commandLine: commandLine,
            output: "",
            exitCode: nil,
            startedAt: Date(),
            finishedAt: nil
        )
        workspaceRuns.insert(run, at: 0)
        commandRuns.insert(run, at: 0)
        return run.id
    }

    private func updateTrackedWorkspaceRun(id: UUID, output: String) {
        updateTrackedRun(id: id, in: &workspaceRuns) { run in
            run.output = output
        }
        updateTrackedRun(id: id, in: &commandRuns) { run in
            run.output = output
        }
    }

    private func finishTrackedWorkspaceRun(id: UUID, with run: CommandRun) {
        var finished = run
        finished.id = id
        updateTrackedRun(id: id, in: &workspaceRuns) { item in
            item = finished
        }
        updateTrackedRun(id: id, in: &commandRuns) { item in
            item = finished
        }
    }

    private func updateTrackedRun(id: UUID, in runs: inout [CommandRun], mutate: (inout CommandRun) -> Void) {
        guard let index = runs.firstIndex(where: { $0.id == id }) else { return }
        mutate(&runs[index])
    }

    private func refreshRecentRecordings(limit: Int = 12) {
        let recordingsPath = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".pilot/recordings")
        guard let entries = try? FileManager.default.contentsOfDirectory(
            at: recordingsPath,
            includingPropertiesForKeys: [.isDirectoryKey],
            options: [.skipsHiddenFiles]
        ) else { return }

        let currentProject = standardizedPath(settings.projectPath)
        let recordings = entries.compactMap { directory -> PilotExecutionRecording? in
            guard (try? directory.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) == true else { return nil }
            let metadataURL = directory.appendingPathComponent("metadata.json")
            guard let data = try? Data(contentsOf: metadataURL),
                  let recording = try? JSONDecoder.pilotRecording.decode(PilotExecutionRecording.self, from: data),
                  standardizedPath(recording.projectPath) == currentProject else {
                return nil
            }
            return recording
        }
        .sorted { $0.startTime > $1.startTime }
        .prefix(limit)
        .map(commandRun)

        mergeRecordedRuns(recordings)
    }

    private func mergeRecordedRuns(_ runs: [CommandRun]) {
        for run in runs.reversed() {
            guard !commandRuns.contains(where: { $0.commandLine == run.commandLine }) else { continue }
            commandRuns.insert(run, at: 0)
        }
        for run in runs.reversed() {
            guard !workspaceRuns.contains(where: { $0.commandLine == run.commandLine }) else { continue }
            workspaceRuns.insert(run, at: 0)
        }
    }

    private func commandRun(from recording: PilotExecutionRecording) -> CommandRun {
        let model = recording.metadata?.modelName ?? "unknown model"
        let tokens = recording.tokenUsage?.totalTokens ?? 0
        let cost = recording.tokenUsage?.estimatedCostUSD ?? 0
        let status = recording.status.lowercased()
        let exitCode: Int32 = status == "completed" ? 0 : 1
        let qualityLine = status == "completed" ? "Quality Passed" : "Quality Failed"
        return CommandRun(
            title: "recording \(recording.taskID)",
            commandLine: "pilot task --recording \(recording.id)",
            output: """
            Recording ID: \(recording.id)
            Task ID: \(recording.taskID)
            Status: \(recording.status)
            Model: \(model)
            \(qualityLine)
            tokens_in=\(recording.tokenUsage?.inputTokens ?? 0) tokens_out=\(recording.tokenUsage?.outputTokens ?? 0) cost_usd=\(cost)
            Total tokens: \(tokens)
            """,
            exitCode: exitCode,
            startedAt: recording.startTime,
            finishedAt: recording.endTime
        )
    }

    private func standardizedPath(_ path: String) -> String {
        URL(fileURLWithPath: path).standardizedFileURL.path
    }

    func focusPrimaryWorkspaceChange(force: Bool) {
        if !force, let selectedWorkspaceFilePath, isWorkspacePreviewable(selectedWorkspaceFilePath) {
            return
        }
        if let path = primaryWorkspaceChangePath() {
            selectedWorkspaceFilePath = path
        }
    }

    private func focusWorkspaceFileMentioned(in output: String) {
        guard let path = workspaceFileMentioned(in: output) else { return }
        selectedWorkspaceFilePath = path
    }

    func createWorkspacePullRequest(title: String = "", body: String = "") async {
        await runWorkspaceCommand(buildCreatePRCommand(title: title, body: body), title: "create-pr")
    }

    private func gatewayClient() -> PilotGatewayClient {
        PilotGatewayClient(baseURL: URL(string: settings.gatewayURL)!, token: settings.authToken)
    }

    private static func initialProviderStatuses() -> [ProviderStatus] {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let claudeConfig = firstExisting([
            "\(home)/.claude/settings.json",
            "\(home)/.claude.json"
        ])
        let claudeExecutable = firstExecutable([
            "\(home)/.local/bin/claude",
            "\(home)/Library/Application Support/com.conductor.app/bin/claude",
            "\(home)/Library/Application Support/Claude/claude-code/2.1.160/claude.app/Contents/MacOS/claude",
            "\(home)/Library/Application Support/Claude/claude-code-vm/2.1.156/claude",
            "\(home)/Applications/Claude Code URL Handler.app/Contents/MacOS/claude",
            "/Applications/Claude.app/Contents/MacOS/Claude"
        ])
        let codexConfig = firstExisting([
            "\(home)/.codex/config.toml",
            "\(home)/.codex/settings.json"
        ])
        let codexAuth = firstExisting([
            "\(home)/.codex/auth.json"
        ])
        let codexExecutable = FileManager.default.isExecutableFile(atPath: "/Applications/Codex.app/Contents/Resources/codex")
            ? "/Applications/Codex.app/Contents/Resources/codex"
            : ""

        let claudeConnected = !claudeConfig.isEmpty
        let codexConnected = !codexAuth.isEmpty

        return [
            ProviderStatus(
                provider: .claudeCode,
                connected: claudeConnected,
                headline: claudeConnected ? "Connected" : "CLI not authenticated",
                authMethod: .cli,
                executablePath: claudeExecutable,
                configPath: claudeConfig.isEmpty ? "~/.claude/settings.json" : claudeConfig,
                loginCommand: "claude /login",
                detailRows: [
                    ProviderDetailRow(label: "Provider", value: "Claude Code"),
                    ProviderDetailRow(label: "Method", value: "CLI"),
                    ProviderDetailRow(label: "Executable", value: claudeExecutable.isEmpty ? "auto" : claudeExecutable),
                    ProviderDetailRow(label: "Config", value: claudeConfig.isEmpty ? "~/.claude/settings.json" : claudeConfig)
                ]
            ),
            ProviderStatus(
                provider: .codex,
                connected: codexConnected,
                headline: codexConnected ? "Connected" : "CLI not authenticated",
                authMethod: .cli,
                executablePath: codexExecutable,
                configPath: codexConfig.isEmpty ? "~/.codex/config.toml" : codexConfig,
                loginCommand: "codex login",
                detailRows: [
                    ProviderDetailRow(label: "Provider", value: "Codex"),
                    ProviderDetailRow(label: "Method", value: "CLI"),
                    ProviderDetailRow(label: "Executable", value: codexExecutable.isEmpty ? "not found" : codexExecutable),
                    ProviderDetailRow(label: "Config", value: codexConfig.isEmpty ? "~/.codex/config.toml" : codexConfig)
                ]
            )
        ]
    }

    private static func firstExisting(_ paths: [String]) -> String {
        paths.first { FileManager.default.fileExists(atPath: $0) } ?? ""
    }

    private static func firstExecutable(_ paths: [String]) -> String {
        paths.first { path in
            var isDirectory: ObjCBool = false
            guard FileManager.default.fileExists(atPath: path, isDirectory: &isDirectory), !isDirectory.boolValue else {
                return false
            }
            return FileManager.default.isExecutableFile(atPath: path)
        } ?? ""
    }

    private static let detectClaudeExecutableCommand = """
    claude_bin="$(command -v claude || command -v claude-code || true)"
    if [ -n "$claude_bin" ] && { [ ! -f "$claude_bin" ] || [ ! -x "$claude_bin" ]; }; then
      claude_bin=""
    fi
    if [ -z "$claude_bin" ] && [ -f "$HOME/.local/bin/claude" ] && [ -x "$HOME/.local/bin/claude" ]; then
      claude_bin="$HOME/.local/bin/claude"
    fi
    if [ -z "$claude_bin" ]; then
      claude_bin="$HOME/Library/Application Support/com.conductor.app/bin/claude"
    fi
    if [ ! -f "$claude_bin" ] || [ ! -x "$claude_bin" ]; then
      claude_bin="$(find "$HOME/Library/Application Support/Claude/claude-code" "$HOME/Library/Application Support/Claude/claude-code-vm" "$HOME/Library/Application Support/com.conductor.app/agent-binaries/claude" "$HOME/Library/Developer/Xcode/CodingAssistant/Agents/claude" -type f -name claude -perm -111 2>/dev/null | sort -r | head -n 1)"
    fi
    if { [ ! -f "$claude_bin" ] || [ ! -x "$claude_bin" ]; } && [ -f /Applications/Claude.app/Contents/MacOS/Claude ] && [ -x /Applications/Claude.app/Contents/MacOS/Claude ]; then
      claude_bin="/Applications/Claude.app/Contents/MacOS/Claude"
    fi
    if [ -n "$claude_bin" ]; then printf "%s" "$claude_bin"; fi
    """

    private static let detectCodexExecutableCommand = """
    codex_bin="$(command -v codex || true)"
    if [ -n "$codex_bin" ] && { [ ! -f "$codex_bin" ] || [ ! -x "$codex_bin" ]; }; then
      codex_bin=""
    fi
    if [ -z "$codex_bin" ] && [ -f /Applications/Codex.app/Contents/Resources/codex ] && [ -x /Applications/Codex.app/Contents/Resources/codex ]; then
      codex_bin="/Applications/Codex.app/Contents/Resources/codex"
    fi
    if [ ! -f "$codex_bin" ] || [ ! -x "$codex_bin" ]; then
      codex_bin="$HOME/Library/Application Support/com.conductor.app/bin/codex"
    fi
    if [ ! -f "$codex_bin" ] || [ ! -x "$codex_bin" ]; then
      codex_bin="$(find "$HOME/Library/Application Support/com.conductor.app/agent-binaries/codex" "$HOME/Library/Developer/Xcode/CodingAssistant/Agents/codex" -type f -name codex -perm -111 2>/dev/null | sort -r | head -n 1)"
    fi
    if [ -n "$codex_bin" ]; then printf "%s" "$codex_bin"; fi
    """

    private func resolvedLoginCommand(for account: AgentAccount) -> String {
        let explicit = expandAccountPath(account.executablePath)
        if !explicit.isEmpty {
            switch account.provider {
            case .claudeCode: return "\(shellQuote(explicit)) /login"
            case .codex: return "\(shellQuote(explicit)) login"
            }
        }
        let status = providerStatus(for: account.provider)
        if !status.executablePath.isEmpty {
            switch account.provider {
            case .claudeCode: return "\(shellQuote(status.executablePath)) /login"
            case .codex: return "\(shellQuote(status.executablePath)) login"
            }
        }
        return AgentAccount.defaultLoginCommand(for: account.provider)
    }

    private func accountLoginEnvironmentPrefix(_ account: AgentAccount) -> String {
        let assignments = account.environment
            .split(whereSeparator: \.isNewline)
            .compactMap { rawLine -> String? in
                var line = rawLine.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !line.isEmpty, !line.hasPrefix("#") else { return nil }
                if line.hasPrefix("export ") {
                    line.removeFirst("export ".count)
                    line = line.trimmingCharacters(in: .whitespacesAndNewlines)
                }
                guard let separator = line.firstIndex(of: "=") else { return nil }
                let key = String(line[..<separator]).trimmingCharacters(in: .whitespacesAndNewlines)
                let value = expandAccountPath(String(line[line.index(after: separator)...]))
                guard isValidEnvironmentKey(key) else { return nil }
                return "\(key)=\(shellQuote(value))"
            }
        return assignments.isEmpty ? "" : "env \(assignments.joined(separator: " ")) "
    }

    private func isValidEnvironmentKey(_ key: String) -> Bool {
        guard let first = key.unicodeScalars.first else { return false }
        guard first == "_" || CharacterSet.letters.contains(first) else { return false }
        return key.unicodeScalars.allSatisfy { scalar in
            scalar == "_" || CharacterSet.alphanumerics.contains(scalar)
        }
    }

    private func expandAccountPath(_ value: String) -> String {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.hasPrefix("~/") {
            return FileManager.default.homeDirectoryForCurrentUser.path + String(trimmed.dropFirst())
        }
        return trimmed
    }

    private func codexAuthPath(forConfigPath path: String) -> String? {
        guard !path.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        let directory = URL(fileURLWithPath: path).deletingLastPathComponent().path
        return "\(directory)/auth.json"
    }

    private func claudeStatus(executablePath: String, configPath: String) -> ProviderStatus {
        let configured = !configPath.isEmpty
        let connected = !executablePath.isEmpty || configured
        return ProviderStatus(
            provider: .claudeCode,
            connected: connected,
            headline: connected ? "Connected" : "CLI not authenticated",
            authMethod: .cli,
            executablePath: executablePath,
            configPath: configPath.isEmpty ? "~/.claude/settings.json" : configPath,
            loginCommand: "claude /login",
            detailRows: [
                ProviderDetailRow(label: "Provider", value: "Claude Code"),
                ProviderDetailRow(label: "Method", value: "CLI"),
                ProviderDetailRow(label: "Executable", value: executablePath.isEmpty ? "not found in shell PATH" : executablePath),
                ProviderDetailRow(label: "Config", value: configPath.isEmpty ? "~/.claude/settings.json" : configPath)
            ]
        )
    }

    private func codexStatus(executablePath: String, configPath: String, loginStatus: String) -> ProviderStatus {
        let connected = loginStatus.lowercased().contains("logged in")
        return ProviderStatus(
            provider: .codex,
            connected: connected,
            headline: connected ? "Connected" : "CLI not authenticated",
            authMethod: .cli,
            executablePath: executablePath,
            configPath: configPath.isEmpty ? "~/.codex/config.toml" : configPath,
            loginCommand: "codex login",
            detailRows: [
                ProviderDetailRow(label: "Provider", value: "Codex"),
                ProviderDetailRow(label: "Method", value: "CLI"),
                ProviderDetailRow(label: "Executable", value: executablePath.isEmpty ? "not found" : executablePath),
                ProviderDetailRow(label: "Status", value: loginStatus.isEmpty ? "not checked" : loginStatus),
                ProviderDetailRow(label: "Config", value: configPath.isEmpty ? "~/.codex/config.toml" : configPath)
            ]
        )
    }

    private func primaryWorkspaceChangePath() -> String? {
        let candidates = workspace.changedFiles
            .map(\.path)
            .filter { isWorkspacePreviewable($0) && !isGeneratedWorkspaceChange($0) }

        return candidates.first(where: sourceLikeWorkspacePath) ?? candidates.first
    }

    private func isWorkspacePreviewable(_ path: String) -> Bool {
        guard !path.hasSuffix("/") else { return false }
        let root = workspace.repoRoot.isEmpty ? settings.projectPath : workspace.repoRoot
        var isDirectory: ObjCBool = false
        let exists = FileManager.default.fileExists(atPath: "\(root)/\(path)", isDirectory: &isDirectory)
        return exists && !isDirectory.boolValue
    }

    private func isGeneratedWorkspaceChange(_ path: String) -> Bool {
        if path.hasPrefix(".agent/tasks/task-") && path.hasSuffix(".md") {
            return true
        }
        let normalized = path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        let topLevel = normalized.split(separator: "/", maxSplits: 1).first.map(String.init) ?? normalized
        return [".build", ".claude", ".codex", ".git", ".pilot", "dist"].contains(topLevel)
    }

    private func sourceLikeWorkspacePath(_ path: String) -> Bool {
        [".swift", ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".kt", ".rs"].contains { path.hasSuffix($0) }
    }

    private func workspaceFileMentioned(in output: String) -> String? {
        let candidates = workspacePathCandidates(in: output)
            .compactMap(normalizedWorkspaceRelativePath)
            .filter { isWorkspacePreviewable($0) && !isGeneratedWorkspaceChange($0) }
        return candidates.first(where: sourceLikeWorkspacePath) ?? candidates.first
    }

    private func workspacePathCandidates(in output: String) -> [String] {
        let separators = CharacterSet.whitespacesAndNewlines
            .union(CharacterSet(charactersIn: "\"'`<>[]{}()"))
        var seen = Set<String>()
        var candidates: [String] = []

        for rawToken in output.components(separatedBy: separators) {
            let token = rawToken.trimmingCharacters(in: CharacterSet(charactersIn: ".,;"))
            guard token.contains("/") || token.contains(".") else { continue }
            guard !seen.contains(token) else { continue }
            seen.insert(token)
            candidates.append(token)
        }
        return candidates
    }

    private func normalizedWorkspaceRelativePath(_ rawPath: String) -> String? {
        var path = rawPath
            .replacingOccurrences(of: "\\/", with: "/")
            .trimmingCharacters(in: CharacterSet(charactersIn: " \n\t\r\"'`.,;"))
        guard !path.isEmpty else { return nil }

        path = stripLineSuffix(from: path)
        if path.hasPrefix("./") {
            path.removeFirst(2)
        }
        if path.hasPrefix("a/") || path.hasPrefix("b/") {
            path.removeFirst(2)
        }

        let root = standardizedPath(workspace.repoRoot.isEmpty ? settings.projectPath : workspace.repoRoot)
        let standardized = standardizedPath(path)
        if standardized.hasPrefix(root + "/") {
            return String(standardized.dropFirst(root.count + 1))
        }
        if path.hasPrefix("/") {
            return nil
        }
        return path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    }

    private func stripLineSuffix(from rawPath: String) -> String {
        var path = rawPath
        while let suffix = path.split(separator: ":", omittingEmptySubsequences: false).last,
              !suffix.isEmpty,
              suffix.allSatisfy(\.isNumber),
              let range = path.range(of: ":\(suffix)", options: .backwards) {
            path.removeSubrange(range)
        }
        return path
    }

    private func workspaceTaskChatSummary(_ run: CommandRun, backend: ExecutionBackend) -> String {
        let combined = [run.output, run.stderr]
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .joined(separator: "\n")
        let lines = combined.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        let taskID = firstValue(after: "Task ID:", in: lines) ?? "local task"
        let highlights = workspaceTaskHighlights(from: lines)
        let hasFinding = highlights.contains { line in
            line.contains("FAIL") || line.contains("panic:") || line.contains("operation not permitted")
        }
        let failedByLog = combined.contains("Backend execution failed")
            || combined.contains("phase=Failed")
            || combined.contains("\"is_error\":true")
            || combined.contains("model_not_found")
            || combined.contains("Task failed:")
        let status = run.exitCode == 0 && !failedByLog ? (hasFinding ? "completed with findings" : "completed") : "failed"

        var summary = [
            "Pilot task \(status)",
            "Task: \(taskID)",
            "Backend: \(backend.label)",
            "Mode: local"
        ]
        if let selectedWorkspaceFilePath {
            summary.append("Focused file: \(selectedWorkspaceFilePath)")
        }
        if !highlights.isEmpty {
            summary.append("")
            summary.append("Highlights:")
            summary.append(contentsOf: highlights.map { "- \($0)" })
        }
        summary.append("")
        summary.append("Raw log is available in Terminal.")
        return summary.joined(separator: "\n")
    }

    private func workspaceTaskLiveSummary(_ output: String, backend: ExecutionBackend) -> String {
        let trimmed = output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            return "Task is running.\nBackend: \(backend.label)\nRaw log is streaming in Terminal."
        }
        let lines = trimmed.split(separator: "\n", omittingEmptySubsequences: false).suffix(24).map(String.init)
        var header = ["Task is running.", "Backend: \(backend.label)", "Raw log is streaming in Terminal."]
        if let selectedWorkspaceFilePath {
            header.append("Focused file: \(selectedWorkspaceFilePath)")
        }
        return (header + [""] + lines).joined(separator: "\n")
    }

    private func workspaceTaskHighlights(from lines: [String]) -> [String] {
        var highlights: [String] = []
        for line in lines {
            let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { continue }

            for parsed in workspaceTaskStructuredHighlights(from: trimmed) {
                if !highlights.contains(parsed) {
                    highlights.append(parsed)
                }
            }

            if trimmed.contains("panic:") || trimmed.contains("operation not permitted") {
                if !highlights.contains(trimmed) {
                    highlights.append(trimmed)
                }
            } else if trimmed.contains("Quality Passed") {
                let highlight = "Quality passed: All quality gates passed"
                if !highlights.contains(highlight) {
                    highlights.append(highlight)
                }
            } else if trimmed.contains("Quality gate checks completed") {
                let passed = workspaceTaskFieldValue(after: "all_passed=", in: trimmed) ?? "unknown"
                let total = workspaceTaskFieldValue(after: "total_time=", in: trimmed) ?? "unknown"
                let highlight = "Quality gates: all_passed=\(passed), total_time=\(total)"
                if !highlights.contains(highlight) {
                    highlights.append(highlight)
                }
            } else if trimmed.contains("Backend execution failed") || trimmed.contains("model_not_found") || trimmed.contains("phase=Failed") {
                if !highlights.contains(trimmed) {
                    highlights.append(trimmed)
                }
            } else if trimmed.contains("Task completed") && trimmed.contains("cost_usd=") {
                let cost = workspaceTaskFieldValue(after: "cost_usd=", in: trimmed) ?? "unknown"
                let tokensIn = workspaceTaskFieldValue(after: "tokens_in=", in: trimmed) ?? "unknown"
                let tokensOut = workspaceTaskFieldValue(after: "tokens_out=", in: trimmed) ?? "unknown"
                let highlight = "Task cost: cost_usd=\(cost), tokens_in=\(tokensIn), tokens_out=\(tokensOut)"
                if !highlights.contains(highlight) {
                    highlights.append(highlight)
                }
            }
            if highlights.count >= 6 {
                break
            }
        }
        return highlights
    }

    private func workspaceTaskStructuredHighlights(from line: String) -> [String] {
        guard line.hasPrefix("{"),
              let data = line.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let item = json["item"] as? [String: Any],
              let itemType = item["type"] as? String,
              itemType == "command_execution",
              let command = item["command"] as? String,
              command.contains("go test")
        else {
            return []
        }

        var highlights = ["Command: \(workspaceTaskCleanCommand(command))"]
        if let output = item["aggregated_output"] as? String {
            let outputLine = output
                .split(separator: "\n")
                .map { String($0).trimmingCharacters(in: .whitespacesAndNewlines) }
                .first { !$0.isEmpty && !$0.hasPrefix("---") }
            if let outputLine {
                highlights.append("Output: \(outputLine)")
            }
        }
        if let exitCode = item["exit_code"] as? Int {
            highlights.append("Exit code: \(exitCode)")
        }
        return highlights
    }

    private func workspaceTaskCleanCommand(_ command: String) -> String {
        guard let goTestLine = command
            .split(separator: "\n")
            .map({ String($0).trimmingCharacters(in: .whitespacesAndNewlines) })
            .first(where: { $0.contains("go test") })
        else {
            return command.replacingOccurrences(of: "\n", with: " ")
        }

        if let redirectRange = goTestLine.range(of: " >") {
            return String(goTestLine[..<redirectRange.lowerBound])
        }
        return goTestLine
    }

    private func workspaceTaskFieldValue(after marker: String, in line: String) -> String? {
        guard let range = line.range(of: marker) else { return nil }
        let rest = line[range.upperBound...]
        return rest
            .split(whereSeparator: { $0 == " " || $0 == "\n" || $0 == "\t" })
            .first
            .map(String.init)
    }

    private func firstValue(after marker: String, in lines: [String]) -> String? {
        for line in lines {
            guard let range = line.range(of: marker) else { continue }
            let value = line[range.upperBound...].trimmingCharacters(in: .whitespacesAndNewlines)
            if !value.isEmpty {
                return value
            }
        }
        return nil
    }
}

func splitShellLike(_ value: String) -> [String] {
    value
        .split(whereSeparator: { $0 == " " || $0 == "\n" || $0 == "\t" })
        .map(String.init)
}

private func cleanOutput(_ value: String) -> String {
    value.trimmingCharacters(in: .whitespacesAndNewlines)
}

/// Builds the `gh pr create` command for the workspace console.
///
/// When the user supplies a title and/or description, they are forwarded via
/// `--title` / `--body` so the form input actually reaches GitHub. With no
/// input we fall back to `--web`, letting GitHub prefill the PR from commits.
func buildCreatePRCommand(title: String, body: String) -> String {
    let trimmedTitle = title.trimmingCharacters(in: .whitespacesAndNewlines)
    let trimmedBody = body.trimmingCharacters(in: .whitespacesAndNewlines)

    guard !trimmedTitle.isEmpty || !trimmedBody.isEmpty else {
        return "gh pr create --web"
    }

    var parts = ["gh", "pr", "create"]
    if trimmedTitle.isEmpty {
        // gh requires a title; reuse the body's first line so the call is non-interactive.
        let fallbackTitle = trimmedBody.split(separator: "\n", maxSplits: 1).first.map(String.init) ?? trimmedBody
        parts += ["--title", shellQuote(fallbackTitle)]
    } else {
        parts += ["--title", shellQuote(trimmedTitle)]
    }
    parts += ["--body", shellQuote(trimmedBody)]
    return parts.joined(separator: " ")
}

private func workspaceStatusSummary(_ files: [WorkspaceFileChange]) -> String {
    if files.isEmpty {
        return "clean"
    }
    return "modified"
}

private func canonicalRepositoryRoot(from path: String) -> String {
    let url = URL(fileURLWithPath: path)
    let components = url.pathComponents
    if let codexIndex = components.firstIndex(of: ".Codex"), codexIndex > 1 {
        return NSString.path(withComponents: Array(components.prefix(codexIndex)))
    }
    return path
}

private func workspaceProjectName(from path: String) -> String {
    URL(fileURLWithPath: canonicalRepositoryRoot(from: path)).lastPathComponent
}

private func uniqueWorkspaceName(projectName: String) -> String {
    let formatter = DateFormatter()
    formatter.dateFormat = "yyyyMMdd-HHmmss"
    return "\(projectName)-\(formatter.string(from: Date()))"
}

private func parseWorkspaceList(output: String, currentPath: String) -> [WorkspaceListItem] {
    var items: [WorkspaceListItem] = []
    var path = ""
    var branch = ""

    func flush() {
        guard !path.isEmpty else { return }
        let normalizedPath = URL(fileURLWithPath: path).standardizedFileURL.path
        let normalizedCurrent = URL(fileURLWithPath: currentPath).standardizedFileURL.path
        items.append(WorkspaceListItem(
            path: normalizedPath,
            name: URL(fileURLWithPath: normalizedPath).lastPathComponent,
            branch: branch.replacingOccurrences(of: "refs/heads/", with: ""),
            isCurrent: normalizedPath == normalizedCurrent
        ))
        path = ""
        branch = ""
    }

    for rawLine in output.split(separator: "\n", omittingEmptySubsequences: false) {
        let line = String(rawLine)
        if line.isEmpty {
            flush()
        } else if line.hasPrefix("worktree ") {
            path = String(line.dropFirst("worktree ".count))
        } else if line.hasPrefix("branch ") {
            branch = String(line.dropFirst("branch ".count))
        } else if line == "detached" {
            branch = "detached"
        }
    }
    flush()

    return items.sorted { lhs, rhs in
        if lhs.isCurrent != rhs.isCurrent { return lhs.isCurrent && !rhs.isCurrent }
        return lhs.name.localizedStandardCompare(rhs.name) == .orderedAscending
    }
}

private func parseWorkspaceFiles(status: String, numstat: String) -> [WorkspaceFileChange] {
    var stats: [String: (additions: Int, deletions: Int)] = [:]
    for line in numstat.split(separator: "\n") {
        let parts = line.split(separator: "\t", omittingEmptySubsequences: false)
        guard parts.count >= 3 else { continue }
        let additions = Int(parts[0]) ?? 0
        let deletions = Int(parts[1]) ?? 0
        let path = String(parts[2])
        stats[path] = (additions, deletions)
    }

    var files: [WorkspaceFileChange] = []
    var seen = Set<String>()
    for rawLine in status.split(separator: "\n") {
        let line = String(rawLine)
        guard line.count >= 4 else { continue }
        let statusCode = String(line.prefix(2)).trimmingCharacters(in: .whitespaces)
        let rawPath = String(line.dropFirst(3))
        let path = rawPath.components(separatedBy: " -> ").last ?? rawPath
        let fileStats = stats[path] ?? (0, 0)
        files.append(WorkspaceFileChange(
            path: path,
            status: statusCode.isEmpty ? "?" : statusCode,
            additions: fileStats.additions,
            deletions: fileStats.deletions
        ))
        seen.insert(path)
    }

    for (path, fileStats) in stats where !seen.contains(path) {
        files.append(WorkspaceFileChange(path: path, status: "M", additions: fileStats.additions, deletions: fileStats.deletions))
    }

    return files.sorted { $0.path.localizedStandardCompare($1.path) == .orderedAscending }
}

private struct ParsedWorkspacePR {
    var pullRequest: WorkspacePullRequest
    var checks: [WorkspaceCheck]
}

private struct PilotExecutionRecording: Decodable {
    var id: String
    var taskID: String
    var projectPath: String
    var startTime: Date
    var endTime: Date
    var status: String
    var metadata: PilotExecutionMetadata?
    var tokenUsage: PilotExecutionTokenUsage?

    enum CodingKeys: String, CodingKey {
        case id
        case taskID = "task_id"
        case projectPath = "project_path"
        case startTime = "start_time"
        case endTime = "end_time"
        case status
        case metadata
        case tokenUsage = "token_usage"
    }
}

private struct PilotExecutionMetadata: Decodable {
    var modelName: String?

    enum CodingKeys: String, CodingKey {
        case modelName = "model_name"
    }
}

private struct PilotExecutionTokenUsage: Decodable {
    var inputTokens: Int64
    var outputTokens: Int64
    var totalTokens: Int64
    var estimatedCostUSD: Double

    enum CodingKeys: String, CodingKey {
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case totalTokens = "total_tokens"
        case estimatedCostUSD = "estimated_cost_usd"
    }
}

private extension JSONDecoder {
    static var pilotRecording: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let value = try container.decode(String.self)
            if let date = pilotRecordingDateFormatterWithFractional.date(from: value)
                ?? pilotRecordingDateFormatter.date(from: value) {
                return date
            }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Invalid recording date: \(value)")
        }
        return decoder
    }
}

private let pilotRecordingDateFormatterWithFractional: ISO8601DateFormatter = {
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return formatter
}()

private let pilotRecordingDateFormatter: ISO8601DateFormatter = {
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime]
    return formatter
}()

private func parseWorkspacePullRequest(_ output: String) -> ParsedWorkspacePR? {
    let trimmed = cleanOutput(output)
    guard !trimmed.isEmpty, let data = trimmed.data(using: .utf8) else { return nil }
    guard let ghPR = try? JSONDecoder().decode(GitHubPullRequest.self, from: data), let number = ghPR.number else {
        return nil
    }
    let pullRequest = WorkspacePullRequest(
        number: number,
        title: ghPR.title ?? "Pull request",
        url: ghPR.url ?? "",
        state: ghPR.state ?? "unknown",
        reviewDecision: ghPR.reviewDecision
    )
    let checks = (ghPR.statusCheckRollup ?? []).compactMap { check -> WorkspaceCheck? in
        guard let name = check.name, !name.isEmpty else { return nil }
        return WorkspaceCheck(name: name, status: check.status ?? "unknown", conclusion: check.conclusion)
    }
    return ParsedWorkspacePR(pullRequest: pullRequest, checks: checks)
}

private struct GitHubPullRequest: Decodable {
    var number: Int?
    var title: String?
    var url: String?
    var state: String?
    var reviewDecision: String?
    var statusCheckRollup: [GitHubCheck]?
}

private struct GitHubCheck: Decodable {
    var name: String?
    var status: String?
    var conclusion: String?
}
