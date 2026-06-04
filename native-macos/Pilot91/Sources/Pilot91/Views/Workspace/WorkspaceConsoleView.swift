import SwiftUI
import AppKit
import class SwiftTerm.LocalProcessTerminalView
import class SwiftTerm.TerminalView
import protocol SwiftTerm.LocalProcessTerminalViewDelegate

private enum WorkspaceConsoleTab: String, CaseIterable, Identifiable {
    case agent = "Chat"
    case files = "Files"
    case terminal = "Terminal"

    var id: String { rawValue }
}

private enum WorkspaceReviewTab: String, CaseIterable, Identifiable {
    case files = "All files"
    case changes = "Changes"
    case checks = "Checks"

    var id: String { rawValue }
}

private enum WorkspaceDockTab: String, CaseIterable, Identifiable {
    case setup = "Setup"
    case run = "Run"
    case terminal = "Terminal"

    var id: String { rawValue }
}

private enum WorkspacePreviewMode: String, CaseIterable, Identifiable {
    case diff = "Diff"
    case file = "File"

    var id: String { rawValue }
}

private enum WorkspaceRunMode: String, CaseIterable, Identifiable {
    case shell = "Shell"
    case task = "Pilot task"
    case autopilot = "Autopilot"
    case architect = "Architect"

    var id: String { rawValue }
}

private func isGeneratedTaskNote(_ path: String) -> Bool {
    path.hasPrefix(".agent/tasks/task-") && path.hasSuffix(".md")
}

private func isHiddenWorkspaceTopLevelEntry(_ name: String) -> Bool {
    [".build", ".claude", ".codex", ".git", ".pilot", "dist"].contains(name) || name == ".DS_Store"
}

private func isHiddenWorkspaceChange(_ path: String) -> Bool {
    if isGeneratedTaskNote(path) {
        return true
    }
    let normalized = path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    let topLevelName = normalized.split(separator: "/", maxSplits: 1).first.map(String.init) ?? normalized
    return isHiddenWorkspaceTopLevelEntry(topLevelName)
}

private func workspaceProjectName(from root: String) -> String {
    let url = URL(fileURLWithPath: root)
    if let codexIndex = url.pathComponents.firstIndex(of: ".Codex"), codexIndex > 1 {
        return url.pathComponents[codexIndex - 1]
    }
    return url.lastPathComponent
}

private func workspaceCopyName(from root: String) -> String {
    URL(fileURLWithPath: root).lastPathComponent
}

private func workspaceTerminalPrompt(workspace: String, branch: String, modified: Bool) -> String {
    let status = modified ? " modified + -" : ""
    return "\(compactPromptComponent(workspace, limit: 16)) \(compactPromptComponent(branch, limit: 14))\(status) >"
}

private func compactPromptComponent(_ value: String, limit: Int) -> String {
    guard value.count > limit, limit > 7 else { return value }
    let headCount = max(3, (limit - 3) / 2)
    let tailCount = max(3, limit - 3 - headCount)
    return "\(value.prefix(headCount))...\(value.suffix(tailCount))"
}

struct WorkspaceConsoleView: View {
    @EnvironmentObject private var store: AppStore
    @State private var selectedTab: WorkspaceConsoleTab = .files
    @State private var prompt = ""

    var body: some View {
        VStack(spacing: 8) {
            WorkspaceHeader()

            HStack(alignment: .top, spacing: 0) {
                VStack(spacing: 12) {
                    WorkspaceTabBar(selectedTab: $selectedTab)

                    WorkspaceMainPane(selectedTab: selectedTab)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)

                    WorkspaceComposer(prompt: $prompt, selectedTab: $selectedTab)
                }
                .padding(.trailing, 12)
                .frame(maxWidth: .infinity, maxHeight: .infinity)

                Divider()

                WorkspaceReviewRail(workspaceTab: $selectedTab)
                    .frame(width: 390)
                    .padding(.leading, 12)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .preferredColorScheme(.dark)
        .task {
            await store.refreshProviderStatuses()
            await store.refreshWorkspace()
        }
    }
}

private struct WorkspaceTabBar: View {
    @EnvironmentObject private var store: AppStore
    @Binding var selectedTab: WorkspaceConsoleTab

    var body: some View {
        HStack(spacing: 0) {
            tab(.files, title: fileTitle, systemImage: "doc.text")
            tab(.agent, title: chatTitle, systemImage: "text.bubble")
            Button {
                store.agentChat.clear()
                selectedTab = .agent
            } label: {
                Image(systemName: "plus")
                    .frame(width: 28, height: 28)
            }
            .buttonStyle(.borderless)
            .help("New chat")
            Spacer()
        }
        .padding(.horizontal, 4)
    }

    private var chatTitle: String {
        store.agentChat.messages.count <= 1 ? "Untitled" : "Workspace chat"
    }

    private var fileTitle: String {
        let path = selectedFilePath ?? sourceChangedPath ?? reviewableChangedPath ?? "AGENTS.md"
        return URL(fileURLWithPath: path).lastPathComponent
    }

    private var selectedFilePath: String? {
        store.selectedWorkspaceFilePath
    }

    private var sourceChangedPath: String? {
        store.workspace.changedFiles.first { file in
            !file.path.hasSuffix("/") && !isGeneratedTaskNote(file.path) && sourceLike(file.path)
        }?.path
    }

    private var reviewableChangedPath: String? {
        store.workspace.changedFiles.first { file in
            !file.path.hasSuffix("/") && !isGeneratedTaskNote(file.path)
        }?.path
    }

    private func sourceLike(_ path: String) -> Bool {
        [".swift", ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".kt", ".rs"].contains { path.hasSuffix($0) }
    }

    private func tab(_ tab: WorkspaceConsoleTab, title: String, systemImage: String) -> some View {
        Button {
            selectedTab = tab
        } label: {
            HStack(spacing: 7) {
                Image(systemName: systemImage)
                Text(title)
                    .lineLimit(1)
            }
            .font(.subheadline.weight(selectedTab == tab ? .semibold : .regular))
            .foregroundStyle(selectedTab == tab ? Color.primary : Color.secondary)
            .padding(.horizontal, 12)
            .frame(height: 38)
            .overlay(alignment: .bottom) {
                Rectangle()
                    .fill(selectedTab == tab ? Color.primary.opacity(0.72) : Color.clear)
                    .frame(height: 2)
            }
        }
        .buttonStyle(.plain)
    }
}

private struct WorkspaceHeader: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        HStack(spacing: 10) {
            ViewThatFits(in: .horizontal) {
                fullBreadcrumb
                compactBreadcrumb
            }
            Button {
                Task { await store.refreshWorkspace() }
            } label: {
                Image(systemName: "arrow.clockwise")
                    .frame(width: 28, height: 28)
            }
            .buttonStyle(.borderless)
            .help("Refresh workspace")
            .disabled(store.isRefreshingWorkspace)
            Spacer()
            StatusBadge(text: store.accountStatus(for: store.settings.selectedAgentAccount).headline)
            PilotWorkspaceMenu()
        }
        .font(.subheadline)
        .padding(.leading, 24)
        .padding(.trailing, 8)
        .frame(minHeight: 42, alignment: .center)
    }

    private var fullBreadcrumb: some View {
        HStack(spacing: 10) {
            Text(projectName)
                .font(.headline)
                .lineLimit(1)
            Image(systemName: "chevron.right")
                .font(.caption)
                .foregroundStyle(.tertiary)
            Text(workspaceName)
                .font(.headline)
                .lineLimit(1)
            Label(branchLabel, systemImage: "point.3.connected.trianglepath.dotted")
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
        }
    }

    private var compactBreadcrumb: some View {
        HStack(spacing: 8) {
            Text(workspaceName)
                .font(.headline)
                .lineLimit(1)
            Text(store.workspace.branch.isEmpty ? "detached" : store.workspace.branch)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
        }
    }

    private var workspaceName: String {
        workspaceCopyName(from: effectivePath)
    }

    private var projectName: String {
        workspaceProjectName(from: effectivePath)
    }

    private var branchLabel: String {
        let branch = store.workspace.branch.isEmpty ? "detached" : store.workspace.branch
        return "\(branch) / \(store.workspace.baseBranch)"
    }

    private var effectivePath: String {
        store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
    }
}

private struct PilotWorkspaceMenu: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        Menu {
            Section("Workspace") {
                Button("Commands") { store.selectedSection = .commandCenter }
                Button("Task board") { store.selectedSection = .queue }
                Button("Autopilot") { store.selectedSection = .autopilot }
                Button("Architect") { store.selectedSection = .architect }
                Button("Logs") { store.selectedSection = .logs }
                Button("Agent runtime") { store.selectedSection = .runtime }
            }
            Section("Checks") {
                Button("Validate config") {
                    Task { await store.runPilotArguments("config validate", title: "pilot-config-validate") }
                }
                Button("Autopilot status") {
                    Task { await store.runPilotArguments("autopilot status --json", title: "autopilot-status") }
                }
                Button("Architect scan") {
                    Task { await store.runArchitectScan() }
                }
                Button("Pilot status") {
                    Task { await store.runPilotArguments("status", title: "pilot-status") }
                }
                Button("Pilot doctor") {
                    Task { await store.runPilotArguments("doctor", title: "pilot-doctor") }
                }
            }
            Section {
                Button("Metrics") { store.selectedSection = .metrics }
                Button("Settings") { store.selectedSection = .settings }
            }
        } label: {
            Image(systemName: "ellipsis")
                .frame(width: 28, height: 28)
        }
        .menuStyle(.button)
        .buttonStyle(.borderless)
        .help("Workspace tools")
    }
}

private struct WorkspaceMainPane: View {
    @EnvironmentObject private var store: AppStore
    var selectedTab: WorkspaceConsoleTab

    var body: some View {
        Group {
            switch selectedTab {
            case .agent:
                WorkspaceAgentTranscript()
            case .files:
                WorkspaceFilesPane()
            case .terminal:
                WorkspaceTerminalHistory()
            }
        }
        .padding(selectedTab == .files ? 0 : 14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(selectedTab == .files ? Color.clear : Color.secondary.opacity(0.06))
        .clipShape(RoundedRectangle(cornerRadius: selectedTab == .files ? 0 : 8))
    }
}

private struct WorkspaceAgentTranscript: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    if chatMessages.isEmpty {
                        WorkspaceSessionOverview()
                    } else {
                        ForEach(chatMessages) { message in
                            AgentChatMessageRow(message: message)
                        }
                    }
                    if let error = store.agentChat.lastError, !error.isEmpty {
                        WorkspaceRuntimeNote(title: "Provider Error", text: error, color: .red)
                    }
                    Color.clear.frame(height: 1).id("transcript-bottom")
                }
                .padding(.vertical, 4)
            }
            .onChange(of: transcriptScrollToken) {
                withAnimation(.easeOut(duration: 0.16)) {
                    proxy.scrollTo("transcript-bottom", anchor: .bottom)
                }
            }
        }
    }

    private var chatMessages: [AgentChatMessage] {
        store.agentChat.messages.filter { $0.role != .system }
    }

    private var transcriptScrollToken: String {
        chatMessages
            .map { "\($0.id.uuidString):\($0.body.count):\($0.isRunning)" }
            .joined(separator: "|") + ":\(store.agentChat.lastError ?? "")"
    }
}

private struct WorkspaceSessionOverview: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            WorkspaceSystemLine(title: "Workspace", value: "\(projectName) / \(workspaceName)")
            WorkspaceSystemLine(title: "Branch", value: "\(branchName) from \(store.workspace.baseBranch)")
            WorkspaceSystemLine(title: "Account", value: "\(store.settings.selectedAgentAccount.name) / \(store.settings.selectedChatModel)")
            WorkspaceSystemLine(title: "File", value: activeFilePath)
            if let lastRun = store.workspaceRuns.first {
                Divider()
                WorkspaceSystemLine(title: "Last run", value: lastRun.title)
                WorkspaceSystemLine(title: "Exit", value: lastRun.exitCode.map(String.init) ?? "running")
            }
        }
        .padding(.top, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var root: String {
        store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
    }

    private var projectName: String {
        let url = URL(fileURLWithPath: root)
        if let codexIndex = url.pathComponents.firstIndex(of: ".Codex"), codexIndex > 1 {
            return url.pathComponents[codexIndex - 1]
        }
        return url.lastPathComponent
    }

    private var workspaceName: String {
        URL(fileURLWithPath: root).lastPathComponent
    }

    private var branchName: String {
        store.workspace.branch.isEmpty ? "detached workspace" : store.workspace.branch
    }

    private var activeFilePath: String {
        store.selectedWorkspaceFilePath
            ?? store.workspace.changedFiles.first { !isHiddenWorkspaceChange($0.path) }?.path
            ?? "No file selected"
    }
}

private struct RuntimeChatMessageRow: View {
    var message: RuntimeMessage

    var body: some View {
        HStack(alignment: .top) {
            if message.role == "user" {
                Spacer(minLength: 80)
            }

            VStack(alignment: .leading, spacing: 7) {
                HStack(spacing: 7) {
                    Image(systemName: message.role == "user" ? "person.crop.circle" : "sparkles")
                        .foregroundStyle(message.role == "user" ? Color.blue : Color.green)
                    Text(message.role == "user" ? "You" : "Pilot")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                }
                Text(message.text.isEmpty ? "Working..." : message.text)
                    .font(.body)
                    .lineSpacing(3)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.horizontal, 13)
            .padding(.vertical, 11)
            .frame(maxWidth: message.role == "user" ? 620 : .infinity, alignment: .leading)
            .background(message.role == "user" ? Color.blue.opacity(0.12) : Color.secondary.opacity(0.08))
            .clipShape(RoundedRectangle(cornerRadius: 8))

            if message.role != "user" {
                Spacer(minLength: 80)
            }
        }
    }
}

private struct WorkspaceRuntimeNote: View {
    var title: String
    var text: String
    var color: Color = .secondary

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title.uppercased())
                .font(.caption.weight(.semibold))
                .foregroundStyle(color)
            Text(text)
                .font(.caption)
                .foregroundStyle(color)
                .textSelection(.enabled)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(color.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }
}

private struct AgentChatMessageRow: View {
    var message: AgentChatMessage

    var body: some View {
        HStack(alignment: .top) {
            if message.role == .user {
                Spacer(minLength: 80)
            }

            VStack(alignment: .leading, spacing: 7) {
                HStack(spacing: 7) {
                    Image(systemName: icon)
                        .foregroundStyle(iconColor)
                    Text(title)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                    if let model = message.model, message.role != .system {
                        Text(model)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                    if message.isRunning {
                        ProgressView()
                            .controlSize(.mini)
                    }
                }

                Text(message.body.isEmpty && message.isRunning ? "Thinking..." : message.body)
                    .font(.body)
                    .lineSpacing(3)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.horizontal, 13)
            .padding(.vertical, 11)
            .frame(maxWidth: message.role == .user ? 620 : .infinity, alignment: .leading)
            .background(background)
            .clipShape(RoundedRectangle(cornerRadius: 8))

            if message.role != .user {
                Spacer(minLength: 80)
            }
        }
    }

    private var title: String {
        switch message.role {
        case .user: "You"
        case .assistant: message.provider?.label ?? "Agent"
        case .system: "Session"
        }
    }

    private var icon: String {
        switch message.role {
        case .user: "person.crop.circle"
        case .assistant: message.provider?.systemImage ?? "sparkles"
        case .system: "text.bubble"
        }
    }

    private var iconColor: Color {
        switch message.role {
        case .user: .blue
        case .assistant: message.exitCode == 0 || message.exitCode == nil ? .green : .red
        case .system: .secondary
        }
    }

    private var background: Color {
        switch message.role {
        case .user: Color.blue.opacity(0.12)
        case .assistant: Color.secondary.opacity(0.08)
        case .system: Color.secondary.opacity(0.05)
        }
    }
}

private struct WorkspaceSystemLine: View {
    var title: String
    var value: String

    var body: some View {
        HStack(spacing: 8) {
            Text(title)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
            Text(value)
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
        }
    }
}

private struct WorkspaceBubble: View {
    var role: String
    var text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(role.uppercased())
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
            Text(text)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(11)
        .background(role == "user" ? Color.blue.opacity(0.10) : Color.secondary.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }
}

private struct WorkspaceRunSummary: View {
    var run: CommandRun

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Label(run.title, systemImage: run.isRunning ? "hourglass" : run.exitCode == 0 ? "checkmark.circle" : "terminal")
                Spacer()
                if let exitCode = run.exitCode {
                    Text("exit \(exitCode)")
                        .foregroundStyle(exitCode == 0 ? Color.green : Color.red)
                } else {
                    Text("running")
                        .foregroundStyle(.secondary)
                }
            }
            .font(.subheadline.weight(.medium))
            Text(run.commandLine)
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(2)
            if !run.output.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Text(run.output)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(8)
                    .textSelection(.enabled)
            } else if run.isRunning {
                Text("Waiting for output...")
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }
}

private struct WorkspaceFilesPane: View {
    @EnvironmentObject private var store: AppStore
    @State private var previewMode: WorkspacePreviewMode = .diff

    var body: some View {
        WorkspaceCodePreview(path: activePreviewPath, mode: $previewMode)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .onAppear {
                previewMode = hasWorkspaceChange(activePreviewPath) ? .diff : .file
            }
            .onChange(of: activePreviewPath) { _, path in
                previewMode = hasWorkspaceChange(path) ? .diff : .file
            }
    }

    private var activePreviewPath: String {
        selectedPreviewPath ?? sourceChangedPath ?? reviewableChangedPath ?? previewPath
    }

    private var selectedPreviewPath: String? {
        guard let path = store.selectedWorkspaceFilePath, isPreviewable(path) else { return nil }
        return path
    }

    private var sourceChangedPath: String? {
        store.workspace.changedFiles.first { file in
            !isGeneratedTaskNote(file.path) && isPreviewable(file.path) && sourceLike(file.path)
        }?.path
    }

    private var reviewableChangedPath: String? {
        store.workspace.changedFiles.first { file in
            !isGeneratedTaskNote(file.path) && isPreviewable(file.path)
        }?.path
    }

    private var previewPath: String {
        let candidates = [
            "AGENTS.md",
            "native-macos/Pilot91/Sources/Pilot91/Views/Workspace/WorkspaceConsoleView.swift",
            "native-macos/Pilot91/Sources/Pilot91/Views/Root/ConductorWindow.swift",
            "cmd/pilot/main.go"
        ]
        let root = store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
        return candidates.first { FileManager.default.fileExists(atPath: "\(root)/\($0)") } ?? candidates[0]
    }

    private func isPreviewable(_ path: String) -> Bool {
        guard !path.hasSuffix("/") else { return false }
        let root = store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
        var isDirectory: ObjCBool = false
        let exists = FileManager.default.fileExists(atPath: "\(root)/\(path)", isDirectory: &isDirectory)
        return exists && !isDirectory.boolValue
    }

    private func sourceLike(_ path: String) -> Bool {
        [".swift", ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".kt", ".rs"].contains { path.hasSuffix($0) }
    }

    private func hasWorkspaceChange(_ path: String) -> Bool {
        store.workspace.changedFiles.contains { $0.path == path }
    }
}

private struct WorkspaceCodePreview: View {
    @EnvironmentObject private var store: AppStore
    var path: String
    @Binding var mode: WorkspacePreviewMode

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Label {
                    Text(path)
                        .lineLimit(1)
                        .truncationMode(.middle)
                } icon: {
                    Image(systemName: fileIcon)
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(fileIconColor)
                }
                .font(editorSmallFont)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(Color.white.opacity(0.025))
                .overlay {
                    RoundedRectangle(cornerRadius: 4)
                        .stroke(Color.white.opacity(0.14), lineWidth: 1)
                }
                .clipShape(RoundedRectangle(cornerRadius: 4))
                Spacer()
                Picker("Preview", selection: $mode) {
                    Text("Diff").tag(WorkspacePreviewMode.diff)
                    Text("File").tag(WorkspacePreviewMode.file)
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .controlSize(.small)
                .frame(width: 112)
                .disabled(!hasWorkspaceChange)
                .help(hasWorkspaceChange ? "Switch between file diff and raw file" : "No diff for this file")
                Button {
                    copyPath()
                } label: {
                    Image(systemName: "doc.on.doc")
                }
                .font(.system(size: 13, weight: .regular))
                .buttonStyle(.borderless)
                .help("Copy path")
                Button {
                    Task { await store.runWorkspaceCommand("open \(shellQuote(absolutePath))", title: "open-file") }
                } label: {
                    Image(systemName: "arrow.up.right.square")
                }
                .font(.system(size: 13, weight: .regular))
                .buttonStyle(.borderless)
                .help("Open file")
            }

            if mode == .diff && hasWorkspaceChange {
                diffPreview
            } else {
                filePreview
            }
        }
        .task(id: "\(path):\(mode.rawValue)") {
            guard mode == .diff, hasWorkspaceChange else { return }
            await store.refreshWorkspaceDiff(for: path)
        }
        .padding(.top, 8)
        .background(Color(nsColor: workspaceCodeBackground))
    }

    private var filePreview: some View {
        CodePreviewTextView(
            text: previewText,
            mode: .file,
            theme: store.settings.codeTheme,
            accessibleColors: store.settings.accessibleColors
        )
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("File preview")
        .accessibilityValue("\(path), first \(previewLines.count) lines")
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    @ViewBuilder
    private var diffPreview: some View {
        if store.isLoadingWorkspaceDiff && !currentDiff.hasDiff {
            ProgressView("Loading diff...")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if currentDiff.hasDiff {
            CodePreviewTextView(
                text: currentDiff.body,
                mode: .diff,
                theme: store.settings.codeTheme,
                accessibleColors: store.settings.accessibleColors
            )
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Diff preview")
            .accessibilityValue("\(path), \(diffLines.count) diff lines")
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        } else {
            VStack(spacing: 8) {
                Image(systemName: "doc.text.magnifyingglass")
                    .font(.title2)
                    .foregroundStyle(.secondary)
                Text("No diff hunks for this file")
                    .font(.subheadline.weight(.medium))
                Text("The file is in git status, but Git did not return a hunk for the selected path.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private var hasWorkspaceChange: Bool {
        store.workspace.changedFiles.contains { $0.path == path }
    }

    private var currentDiff: WorkspaceDiffPreview {
        store.selectedWorkspaceDiff.path == path ? store.selectedWorkspaceDiff : WorkspaceDiffPreview()
    }

    private var diffLines: [String] {
        currentDiff.body
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map(String.init)
    }

    private func diffForeground(_ line: String) -> Color {
        if line.hasPrefix("+") && !line.hasPrefix("+++") { return diffAdditionColor }
        if line.hasPrefix("-") && !line.hasPrefix("---") { return diffDeletionColor }
        if line.hasPrefix("@@") { return diffHunkColor }
        if line.hasPrefix("diff --git") || line.hasPrefix("index ") || line.hasPrefix("new file") || line.hasPrefix("deleted file") { return .secondary }
        return .primary
    }

    private func diffBackground(_ line: String) -> Color {
        if line.hasPrefix("+") && !line.hasPrefix("+++") { return diffAdditionColor.opacity(0.09) }
        if line.hasPrefix("-") && !line.hasPrefix("---") { return diffDeletionColor.opacity(0.08) }
        if line.hasPrefix("@@") { return diffHunkColor.opacity(0.10) }
        return Color.clear
    }

    private var editorFont: Font {
        .system(size: 12, weight: .regular, design: .monospaced)
    }

    private var editorSmallFont: Font {
        .system(size: 11, weight: .regular, design: .monospaced)
    }

    private var fileIcon: String {
        path.hasSuffix(".go") ? "chevron.left.forwardslash.chevron.right" : "doc.text"
    }

    private var fileIconColor: Color {
        path.hasSuffix(".go") ? .cyan : .secondary
    }

    private func copyPath() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(path, forType: .string)
    }

    private var diffAdditionColor: Color {
        switch store.settings.accessibleColors {
        case .standard: .green
        case .redGreenSafe: .cyan
        case .blueYellowSafe: .mint
        }
    }

    private var diffDeletionColor: Color {
        switch store.settings.accessibleColors {
        case .standard: .red
        case .redGreenSafe: .orange
        case .blueYellowSafe: .purple
        }
    }

    private var diffHunkColor: Color {
        switch store.settings.accessibleColors {
        case .standard: .blue
        case .redGreenSafe: .purple
        case .blueYellowSafe: .orange
        }
    }

    private var absolutePath: String {
        let root = store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
        return "\(root)/\(path)"
    }

    private var previewLines: [String] {
        let contents = (try? String(contentsOfFile: absolutePath, encoding: .utf8)) ?? "File preview is unavailable."
        return contents
            .split(separator: "\n", omittingEmptySubsequences: false)
            .prefix(500)
            .map(String.init)
    }

    private var previewText: String {
        previewLines.joined(separator: "\n")
    }
}

private enum CodePreviewTextMode {
    case file
    case diff
}

private let workspaceCodeBackground = NSColor(red: 0.055, green: 0.050, blue: 0.050, alpha: 1)
private let workspaceCodeTextColor = NSColor(red: 0.80, green: 0.78, blue: 0.74, alpha: 1)
private let workspaceCodeLineNumberColor = NSColor(red: 0.50, green: 0.48, blue: 0.45, alpha: 1)
private let workspaceCodePunctuationColor = NSColor(red: 0.58, green: 0.56, blue: 0.52, alpha: 1)
private let workspaceCodeCommentColor = NSColor(red: 0.46, green: 0.45, blue: 0.42, alpha: 1)
private let workspaceCodeKeywordColor = NSColor(red: 0.87, green: 0.40, blue: 0.39, alpha: 1)
private let workspaceCodeFunctionColor = NSColor(red: 0.76, green: 0.43, blue: 0.82, alpha: 1)
private let workspaceCodeStringColor = NSColor(red: 0.58, green: 0.78, blue: 0.50, alpha: 1)
private let workspaceCodeTypeColor = NSColor(red: 0.86, green: 0.55, blue: 0.28, alpha: 1)
private let workspaceCodeNumberColor = NSColor(red: 0.44, green: 0.63, blue: 0.88, alpha: 1)

private struct CodePreviewTextView: NSViewRepresentable {
    var text: String
    var mode: CodePreviewTextMode
    var theme: CodeTheme
    var accessibleColors: AccessibleColorMode

    func makeNSView(context: Context) -> NSScrollView {
        let textView = NSTextView()
        textView.isEditable = false
        textView.isSelectable = true
        textView.isRichText = true
        textView.importsGraphics = false
        textView.usesFindBar = true
        textView.isAutomaticQuoteSubstitutionEnabled = false
        textView.isAutomaticDashSubstitutionEnabled = false
        textView.isAutomaticTextReplacementEnabled = false
        textView.textContainerInset = NSSize(width: 14, height: 12)
        textView.textContainer?.lineFragmentPadding = 0
        textView.isHorizontallyResizable = true
        textView.isVerticallyResizable = true
        textView.minSize = NSSize(width: 0, height: 0)
        textView.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        textView.textContainer?.containerSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        textView.textContainer?.widthTracksTextView = false
        textView.drawsBackground = true
        textView.backgroundColor = workspaceCodeBackground
        textView.textColor = workspaceCodeTextColor

        let scrollView = NSScrollView()
        scrollView.drawsBackground = true
        scrollView.backgroundColor = workspaceCodeBackground
        scrollView.hasVerticalScroller = true
        scrollView.hasHorizontalScroller = true
        scrollView.autohidesScrollers = true
        scrollView.borderType = .noBorder
        scrollView.documentView = textView
        return scrollView
    }

    func updateNSView(_ scrollView: NSScrollView, context: Context) {
        guard let textView = scrollView.documentView as? NSTextView else { return }
        scrollView.backgroundColor = workspaceCodeBackground
        textView.backgroundColor = workspaceCodeBackground
        textView.textColor = workspaceCodeTextColor
        textView.textStorage?.setAttributedString(attributedCodeText(text, mode: mode, theme: theme, accessibleColors: accessibleColors))
    }
}

private struct CodeSyntaxLine: View {
    var text: String
    var theme: CodeTheme

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 0) {
            ForEach(Array(tokens.enumerated()), id: \.offset) { _, token in
                Text(token.text)
                    .foregroundStyle(token.color)
            }
        }
        .fixedSize(horizontal: true, vertical: false)
    }

    private var tokens: [CodeTokenSpan] {
        codeSyntaxTokens(for: text, theme: theme)
    }
}

private struct DiffSyntaxLine: View {
    var text: String
    var theme: CodeTheme

    var body: some View {
        if text.hasPrefix("@@") {
            Text(text)
                .foregroundStyle(Color.blue)
                .fixedSize(horizontal: true, vertical: false)
        } else if text.hasPrefix("diff --git") || text.hasPrefix("index ") || text.hasPrefix("---") || text.hasPrefix("+++") {
            Text(text)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: true, vertical: false)
        } else if let prefix = diffPrefix {
            HStack(alignment: .firstTextBaseline, spacing: 0) {
                Text(prefix)
                    .foregroundStyle(prefix == "+" ? Color.green : Color.red)
                CodeSyntaxLine(text: String(text.dropFirst()), theme: theme)
            }
            .fixedSize(horizontal: true, vertical: false)
        } else {
            CodeSyntaxLine(text: text, theme: theme)
        }
    }

    private var diffPrefix: String? {
        if text.hasPrefix("+") { return "+" }
        if text.hasPrefix("-") { return "-" }
        return nil
    }
}

private struct CodeTokenSpan {
    var text: String
    var color: Color
}

private func attributedCodeText(_ text: String, mode: CodePreviewTextMode, theme: CodeTheme, accessibleColors: AccessibleColorMode) -> NSAttributedString {
    let result = NSMutableAttributedString()
    let lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
    let lineCount = max(lines.count, 1)
    let numberWidth = max(3, String(lineCount).count)
    let font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
    let paragraph = NSMutableParagraphStyle()
    paragraph.minimumLineHeight = 17
    paragraph.maximumLineHeight = 17

    for (index, line) in lines.enumerated() {
        let lineNumber = String(format: "%\(numberWidth)d  ", index + 1)
        result.append(NSAttributedString(
            string: lineNumber,
            attributes: [
                .font: font,
                .foregroundColor: workspaceCodeLineNumberColor,
                .paragraphStyle: paragraph
            ]
        ))

        let lineStart = result.length
        switch mode {
        case .file:
            appendSyntaxLine(line, to: result, theme: theme, font: font, paragraph: paragraph)
        case .diff:
            appendDiffLine(line, to: result, theme: theme, accessibleColors: accessibleColors, font: font, paragraph: paragraph)
        }

        let lineEnd = result.length
        if lineEnd > lineStart, let background = lineBackground(for: line, mode: mode, accessibleColors: accessibleColors) {
            result.addAttribute(.backgroundColor, value: background, range: NSRange(location: lineStart, length: lineEnd - lineStart))
        }

        if index < lines.count - 1 {
            result.append(NSAttributedString(string: "\n", attributes: [.font: font, .paragraphStyle: paragraph]))
        }
    }

    return result
}

private func appendDiffLine(_ line: String, to result: NSMutableAttributedString, theme: CodeTheme, accessibleColors: AccessibleColorMode, font: NSFont, paragraph: NSParagraphStyle) {
    if line.hasPrefix("@@") {
        result.append(NSAttributedString(string: line, attributes: codeAttributes(font: font, color: diffHunkNSColor(accessibleColors), paragraph: paragraph)))
        return
    }

    if line.hasPrefix("diff --git") || line.hasPrefix("index ") || line.hasPrefix("---") || line.hasPrefix("+++") {
        result.append(NSAttributedString(string: line, attributes: codeAttributes(font: font, color: workspaceCodeCommentColor, paragraph: paragraph)))
        return
    }

    if line.hasPrefix("+") || line.hasPrefix("-") {
        let prefix = String(line.prefix(1))
        let color = prefix == "+" ? diffAdditionNSColor(accessibleColors) : diffDeletionNSColor(accessibleColors)
        result.append(NSAttributedString(string: prefix, attributes: codeAttributes(font: font, color: color, paragraph: paragraph)))
        appendSyntaxLine(String(line.dropFirst()), to: result, theme: theme, font: font, paragraph: paragraph)
        return
    }

    appendSyntaxLine(line, to: result, theme: theme, font: font, paragraph: paragraph)
}

private func appendSyntaxLine(_ line: String, to result: NSMutableAttributedString, theme: CodeTheme, font: NSFont, paragraph: NSParagraphStyle) {
    for token in codeSyntaxNSTokens(for: line.isEmpty ? " " : line, theme: theme) {
        result.append(NSAttributedString(string: token.text, attributes: codeAttributes(font: font, color: token.color, paragraph: paragraph)))
    }
}

private func codeAttributes(font: NSFont, color: NSColor, paragraph: NSParagraphStyle) -> [NSAttributedString.Key: Any] {
    [
        .font: font,
        .foregroundColor: color,
        .paragraphStyle: paragraph
    ]
}

private func lineBackground(for line: String, mode: CodePreviewTextMode, accessibleColors: AccessibleColorMode) -> NSColor? {
    guard mode == .diff else { return nil }
    if line.hasPrefix("+") && !line.hasPrefix("+++") {
        return diffAdditionNSColor(accessibleColors).withAlphaComponent(0.055)
    }
    if line.hasPrefix("-") && !line.hasPrefix("---") {
        return diffDeletionNSColor(accessibleColors).withAlphaComponent(0.050)
    }
    if line.hasPrefix("@@") {
        return diffHunkNSColor(accessibleColors).withAlphaComponent(0.070)
    }
    return nil
}

private struct CodeNSTokenSpan {
    var text: String
    var color: NSColor
}

private func codeSyntaxNSTokens(for line: String, theme: CodeTheme) -> [CodeNSTokenSpan] {
    let chars = Array(line)
    var tokens: [CodeNSTokenSpan] = []
    var index = chars.startIndex
    var previousIdentifier = ""

    func append(_ text: String, _ color: NSColor) {
        guard !text.isEmpty else { return }
        tokens.append(CodeNSTokenSpan(text: text, color: color))
    }

    while index < chars.endIndex {
        let char = chars[index]

        if char == "/", chars.index(after: index) < chars.endIndex, chars[chars.index(after: index)] == "/" {
            append(String(chars[index...]), workspaceCodeCommentColor)
            break
        }

        if char == "#", chars[..<index].allSatisfy(\.isWhitespace) {
            append(String(chars[index...]), workspaceCodeCommentColor)
            break
        }

        if char == "\"" || char == "'" || char == "`" {
            let delimiter = char
            var cursor = chars.index(after: index)
            var escaped = false
            while cursor < chars.endIndex {
                let current = chars[cursor]
                if current == delimiter && !escaped {
                    cursor = chars.index(after: cursor)
                    break
                }
                escaped = current == "\\" && !escaped
                if current != "\\" {
                    escaped = false
                }
                cursor = chars.index(after: cursor)
            }
            append(String(chars[index..<cursor]), workspaceCodeStringColor)
            index = cursor
            continue
        }

        if char.isLetter || char == "_" {
            let start = index
            var cursor = chars.index(after: index)
            while cursor < chars.endIndex, chars[cursor].isLetter || chars[cursor].isNumber || chars[cursor] == "_" {
                cursor = chars.index(after: cursor)
            }
            let word = String(chars[start..<cursor])
            append(word, codeNSColor(for: word, previousIdentifier: previousIdentifier, line: chars, rangeEnd: cursor, theme: theme))
            previousIdentifier = word
            index = cursor
            continue
        }

        if char.isNumber {
            let start = index
            var cursor = chars.index(after: index)
            while cursor < chars.endIndex, chars[cursor].isNumber || chars[cursor] == "." {
                cursor = chars.index(after: cursor)
            }
            append(String(chars[start..<cursor]), workspaceCodeNumberColor)
            index = cursor
            continue
        }

        if "{}[]().,:;=+-*/%!<>|&".contains(char) {
            append(String(char), workspaceCodePunctuationColor)
            index = chars.index(after: index)
            continue
        }

        append(String(char), workspaceCodeTextColor)
        index = chars.index(after: index)
    }

    return tokens.isEmpty ? [CodeNSTokenSpan(text: " ", color: workspaceCodeTextColor)] : tokens
}

private func codeNSColor(for word: String, previousIdentifier: String, line: [Character], rangeEnd: Array<Character>.Index, theme _: CodeTheme) -> NSColor {
    let keywords: Set<String> = [
        "actor", "any", "as", "async", "await", "break", "case", "catch", "class", "const", "continue",
        "defer", "default", "do", "else", "enum", "extension", "fallthrough", "false", "fileprivate",
        "for", "func", "function", "guard", "if", "import", "in", "init", "interface", "internal",
        "let", "nil", "package", "private", "protocol", "public", "return", "self", "static", "struct",
        "switch", "throws", "true", "try", "type", "var", "where", "while"
    ]
    let declarationKeywords: Set<String> = ["class", "struct", "enum", "interface", "protocol", "type", "extension"]
    let functionKeywords: Set<String> = ["func", "function"]

    if keywords.contains(word) {
        return workspaceCodeKeywordColor
    }
    if declarationKeywords.contains(previousIdentifier) || word.first?.isUppercase == true {
        return workspaceCodeTypeColor
    }
    if functionKeywords.contains(previousIdentifier) || nextNonSpace(in: line, from: rangeEnd) == "(" {
        return workspaceCodeFunctionColor
    }
    return workspaceCodeTextColor
}

private func diffAdditionNSColor(_ accessibleColors: AccessibleColorMode) -> NSColor {
    switch accessibleColors {
    case .standard: return NSColor.systemGreen
    case .redGreenSafe: return NSColor.systemCyan
    case .blueYellowSafe: return NSColor.systemMint
    }
}

private func diffDeletionNSColor(_ accessibleColors: AccessibleColorMode) -> NSColor {
    switch accessibleColors {
    case .standard: return NSColor.systemRed
    case .redGreenSafe: return NSColor.systemOrange
    case .blueYellowSafe: return NSColor.systemPurple
    }
}

private func diffHunkNSColor(_ accessibleColors: AccessibleColorMode) -> NSColor {
    switch accessibleColors {
    case .standard: return NSColor.systemBlue
    case .redGreenSafe: return NSColor.systemPurple
    case .blueYellowSafe: return NSColor.systemOrange
    }
}

private func codeSyntaxTokens(for line: String, theme: CodeTheme) -> [CodeTokenSpan] {
    let chars = Array(line)
    var tokens: [CodeTokenSpan] = []
    var index = chars.startIndex
    var previousIdentifier = ""

    func append(_ text: String, _ color: Color) {
        guard !text.isEmpty else { return }
        tokens.append(CodeTokenSpan(text: text, color: color))
    }

    while index < chars.endIndex {
        let char = chars[index]

        if char == "/", chars.index(after: index) < chars.endIndex, chars[chars.index(after: index)] == "/" {
            append(String(chars[index...]), theme.comment)
            break
        }

        if char == "#", chars[..<index].allSatisfy(\.isWhitespace) {
            append(String(chars[index...]), theme.comment)
            break
        }

        if char == "\"" || char == "'" || char == "`" {
            let delimiter = char
            var cursor = chars.index(after: index)
            var escaped = false
            while cursor < chars.endIndex {
                let current = chars[cursor]
                if current == delimiter && !escaped {
                    cursor = chars.index(after: cursor)
                    break
                }
                escaped = current == "\\" && !escaped
                if current != "\\" {
                    escaped = false
                }
                cursor = chars.index(after: cursor)
            }
            append(String(chars[index..<cursor]), theme.string)
            index = cursor
            continue
        }

        if char.isLetter || char == "_" {
            let start = index
            var cursor = chars.index(after: index)
            while cursor < chars.endIndex, chars[cursor].isLetter || chars[cursor].isNumber || chars[cursor] == "_" {
                cursor = chars.index(after: cursor)
            }
            let word = String(chars[start..<cursor])
            append(word, codeColor(for: word, previousIdentifier: previousIdentifier, line: chars, rangeEnd: cursor, theme: theme))
            previousIdentifier = word
            index = cursor
            continue
        }

        if char.isNumber {
            let start = index
            var cursor = chars.index(after: index)
            while cursor < chars.endIndex, chars[cursor].isNumber || chars[cursor] == "." {
                cursor = chars.index(after: cursor)
            }
            append(String(chars[start..<cursor]), .orange)
            index = cursor
            continue
        }

        if "{}[]().,:;=+-*/%!<>|&".contains(char) {
            append(String(char), .secondary)
            index = chars.index(after: index)
            continue
        }

        append(String(char), .primary)
        index = chars.index(after: index)
    }

    return tokens.isEmpty ? [CodeTokenSpan(text: " ", color: .primary)] : tokens
}

private func codeColor(for word: String, previousIdentifier: String, line: [Character], rangeEnd: Array<Character>.Index, theme: CodeTheme) -> Color {
    let keywords: Set<String> = [
        "actor", "any", "as", "async", "await", "break", "case", "catch", "class", "const", "continue",
        "defer", "default", "do", "else", "enum", "extension", "fallthrough", "false", "fileprivate",
        "for", "func", "function", "guard", "if", "import", "in", "init", "interface", "internal",
        "let", "nil", "package", "private", "protocol", "public", "return", "self", "static", "struct",
        "switch", "throws", "true", "try", "type", "var", "where", "while"
    ]
    let declarationKeywords: Set<String> = ["class", "struct", "enum", "interface", "protocol", "type", "extension"]
    let functionKeywords: Set<String> = ["func", "function"]

    if keywords.contains(word) {
        return theme.keyword
    }
    if declarationKeywords.contains(previousIdentifier) || word.first?.isUppercase == true {
        return Color(red: 0.95, green: 0.58, blue: 0.20)
    }
    if functionKeywords.contains(previousIdentifier) || nextNonSpace(in: line, from: rangeEnd) == "(" {
        return theme.function
    }
    return .primary
}

private func nextNonSpace(in line: [Character], from index: Array<Character>.Index) -> Character? {
    var cursor = index
    while cursor < line.endIndex {
        if !line[cursor].isWhitespace {
            return line[cursor]
        }
        cursor = line.index(after: cursor)
    }
    return nil
}

private struct WorkspaceFileRow: View {
    @EnvironmentObject private var store: AppStore
    var file: WorkspaceFileChange
    var leadingIndent: CGFloat = 0
    var onSelect: (String) -> Void

    var body: some View {
        Button {
            if !file.path.hasSuffix("/") {
                onSelect(file.path)
            }
        } label: {
            HStack(spacing: 10) {
                Image(systemName: statusIcon)
                    .foregroundStyle(statusColor)
                    .frame(width: 18)
                Text(file.path)
                    .font(.system(.subheadline, design: .monospaced))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer()
            }
            .padding(.leading, leadingIndent)
            .padding(.vertical, 8)
            .background(isSelected ? Color.accentColor.opacity(0.12) : Color.clear)
            .clipShape(RoundedRectangle(cornerRadius: 6))
        }
        .buttonStyle(.plain)
        .disabled(file.path.hasSuffix("/"))
    }

    private var isSelected: Bool {
        store.selectedWorkspaceFilePath == file.path
    }

    private var statusIcon: String {
        if file.path.hasSuffix("/") {
            return "folder"
        }
        switch file.status {
        case "M": return "pencil"
        case "A", "??": return "plus"
        case "D": return "trash"
        case "R": return "arrow.triangle.swap"
        default: return "doc.text"
        }
    }

    private var statusColor: Color {
        guard store.settings.coloredSidebarDiffs || isSelected else {
            return .secondary
        }
        switch file.status {
        case "D": return deletionColor
        case "A", "??": return additionColor
        case "M", "R": return .orange
        default: return .secondary
        }
    }

    private var additionColor: Color {
        switch store.settings.accessibleColors {
        case .standard: .green
        case .redGreenSafe: .cyan
        case .blueYellowSafe: .mint
        }
    }

    private var deletionColor: Color {
        switch store.settings.accessibleColors {
        case .standard: .red
        case .redGreenSafe: .orange
        case .blueYellowSafe: .purple
        }
    }
}

private struct WorkspaceTerminalHistory: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 12) {
                if store.workspaceRuns.isEmpty {
                    CompactEmptyState(title: "No terminal runs", systemImage: "terminal")
                        .frame(minHeight: 280)
                }
                ForEach(store.workspaceRuns) { run in
                    WorkspaceRunSummary(run: run)
                    Divider()
                }
            }
        }
    }
}

private enum WorkspacePromptMode: String, CaseIterable, Identifiable {
    case chat = "Chat"
    case task = "Pilot task"

    var id: String { rawValue }
}

private struct WorkspaceSlashCommand: Identifiable {
    var id: String
    var title: String
    var detail: String
    var systemImage: String
    var prompt: String?
    var mode: WorkspacePromptMode?
    var planFirst = false
    var section: SidebarSection?
}

private struct WorkspaceComposer: View {
    @EnvironmentObject private var store: AppStore
    @Binding var prompt: String
    @Binding var selectedTab: WorkspaceConsoleTab
    @State private var mode: WorkspacePromptMode = .chat
    @State private var planMode = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            TextField(promptPlaceholder, text: $prompt, axis: .vertical)
                .textFieldStyle(.plain)
                .lineLimit(5...9)
                .padding(.horizontal, 2)

            if isShowingSlashCommands {
                WorkspaceSlashCommandPalette(suggestions: slashSuggestions) { command in
                    applySlashCommand(command)
                }
            }

            HStack(alignment: .center, spacing: 10) {
                Picker("Mode", selection: $mode) {
                    ForEach(WorkspacePromptMode.allCases) { mode in
                        Text(mode.rawValue).tag(mode)
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 172)

                if mode == .chat {
                    Text("Agent")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .fixedSize()

                    Menu {
                        ForEach(store.settings.agentAccounts) { account in
                            Button {
                                store.settings.selectedAgentAccountID = account.id
                            } label: {
                                Label(composerAccountName(account), systemImage: account.provider.systemImage)
                            }
                        }
                    } label: {
                        Label(composerAccountName(store.settings.selectedAgentAccount), systemImage: store.settings.selectedAgentAccount.provider.systemImage)
                            .lineLimit(1)
                            .frame(minWidth: 122, alignment: .leading)
                    }
                    .menuStyle(.button)
                    .help("Select agent account")

                    Menu {
                        ForEach(store.settings.selectedAgentAccount.modelOptions, id: \.self) { model in
                            Button(model) {
                                store.settings.selectedChatModel = model
                            }
                        }
                    } label: {
                        Label(composerModelName(store.settings.selectedChatModel), systemImage: "cpu")
                            .lineLimit(1)
                            .frame(minWidth: 112, alignment: .leading)
                    }
                    .menuStyle(.button)
                    .help("Select model")
                } else {
                    Text("Backend")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .fixedSize()

                    Menu {
                        ForEach(taskBackends) { backend in
                            Button {
                                Task { await store.switchBackend(backend) }
                            } label: {
                                Label(backend.label, systemImage: "terminal")
                            }
                        }
                    } label: {
                        Label(taskBackendBinding.wrappedValue.label, systemImage: "terminal")
                            .lineLimit(1)
                            .frame(minWidth: 122, alignment: .leading)
                    }
                    .menuStyle(.button)
                    .help("Select Pilot task backend")

                    Label("Local", systemImage: "tray.and.arrow.down")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Toggle(isOn: $planMode) {
                    Image(systemName: "map")
                        .frame(width: 26, height: 24)
                }
                .toggleStyle(.button)
                .help(planMode ? "Plan-first mode is active" : "Plan first before making changes")
                .accessibilityLabel("Plan first")

                if planMode {
                    Label("Plan-first", systemImage: "map")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }

                Spacer()

                Text("⌘L")
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)

                Menu {
                    Button("Review recent PRs") {
                        prompt = "Review recent PRs for this repository and summarize what should affect the current workspace."
                    }
                    Button("Improve agent instructions") {
                        prompt = "Improve the agent instructions for this repository based on the current AGENTS.md."
                    }
                    Button("Fix a TODO") {
                        prompt = "Find a small TODO in this repository and propose the safest implementation path."
                    }
                    Button("Read the docs") {
                        prompt = "Read the project docs that matter for the current workspace and summarize the next useful action."
                    }
                } label: {
                    Image(systemName: "plus")
                        .frame(width: 28, height: 28)
                }
                .menuStyle(.button)
                .buttonStyle(.borderless)
                .help("Insert prompt")

                if mode == .chat && !store.accountStatus(for: store.settings.selectedAgentAccount).connected {
                    Button {
                        Task { await store.runAccountLogin(store.settings.selectedAgentAccount) }
                    } label: {
                        Label("Login", systemImage: "key")
                    }
                }

                Button {
                    let text = submissionPrompt
                    prompt = ""
                    selectedTab = .agent
                    Task {
                        switch mode {
                        case .chat:
                            await store.runWorkspacePrompt(text)
                        case .task:
                            await store.runWorkspaceTask(text)
                        }
                    }
                } label: {
                    Image(systemName: store.agentChat.isRunning ? "hourglass" : "arrow.up")
                        .frame(width: 30, height: 30)
                }
                .keyboardShortcut(.return, modifiers: [.command])
                .controlSize(.large)
                .disabled(submissionPrompt.isEmpty || store.agentChat.isRunning)
            }
            .controlSize(.regular)
        }
        .padding(14)
        .background(Color.secondary.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private var promptPlaceholder: String {
        switch mode {
        case .chat:
            return "Ask to make changes, @mention files, reference PRs with #, run /commands"
        case .task:
            return "Run a real local Pilot task in this workspace"
        }
    }

    private var submissionPrompt: String {
        let trimmed = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, !trimmed.hasPrefix("/") else { return "" }
        guard planMode else { return trimmed }
        return """
        Plan first before editing. Read the relevant files, identify the exact classes/functions to change, list risks, then execute only the safe scoped changes if the path is clear. Keep the work in this workspace and verify with real commands.

        Task:
        \(trimmed)
        """
    }

    private var isShowingSlashCommands: Bool {
        prompt.trimmingCharacters(in: .whitespacesAndNewlines).hasPrefix("/")
    }

    private var slashSuggestions: [WorkspaceSlashCommand] {
        let rawQuery = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        let query = rawQuery.dropFirst().lowercased()
        let commands = [
            WorkspaceSlashCommand(
                id: "task",
                title: "/task",
                detail: "Run a real local Pilot task in this worktree",
                systemImage: "hammer",
                prompt: "Implement the smallest safe change for the current workspace and verify it.",
                mode: .task
            ),
            WorkspaceSlashCommand(
                id: "plan",
                title: "/plan",
                detail: "Ask the agent to plan first and keep edits scoped",
                systemImage: "map",
                prompt: "Plan the safest implementation path for the current workspace issue.",
                mode: .chat,
                planFirst: true
            ),
            WorkspaceSlashCommand(
                id: "diff",
                title: "/diff",
                detail: "Review current workspace diffs and blockers",
                systemImage: "plusminus",
                prompt: "Review the current workspace diff, identify risky changes, and propose the next verification command.",
                mode: .chat
            ),
            WorkspaceSlashCommand(
                id: "autopilot",
                title: "/autopilot",
                detail: "Run a local autopilot-safe verification task",
                systemImage: "arrow.triangle.2.circlepath",
                prompt: "Run an autopilot-safe local verification for the current workspace and report merge blockers.",
                mode: .task
            ),
            WorkspaceSlashCommand(
                id: "architect",
                title: "/architect",
                detail: "Prepare an Architect scheduled-task workflow",
                systemImage: "scope",
                prompt: "Create or verify an Architect scheduled-task workflow for this repository, then show the exact config and validation output.",
                mode: .task,
                planFirst: true
            ),
            WorkspaceSlashCommand(
                id: "logs",
                title: "/logs",
                detail: "Open live workspace and command logs",
                systemImage: "doc.text",
                section: .logs
            ),
            WorkspaceSlashCommand(
                id: "settings",
                title: "/settings",
                detail: "Open accounts, providers and project settings",
                systemImage: "gearshape",
                section: .settings
            ),
            WorkspaceSlashCommand(
                id: "commands",
                title: "/commands",
                detail: "Open the command center",
                systemImage: "terminal",
                section: .commandCenter
            )
        ]
        guard !query.isEmpty else { return commands }
        return commands.filter { command in
            command.title.dropFirst().lowercased().contains(query) || command.detail.lowercased().contains(query)
        }
    }

    private func applySlashCommand(_ command: WorkspaceSlashCommand) {
        if let section = command.section {
            prompt = ""
            store.selectedSection = section
            return
        }
        if let mode = command.mode {
            self.mode = mode
        }
        planMode = command.planFirst
        prompt = command.prompt ?? ""
    }

    private var taskBackends: [ExecutionBackend] {
        ExecutionBackend.allCases.filter { $0 != .codexAppServer }
    }

    private var taskBackendBinding: Binding<ExecutionBackend> {
        Binding(
            get: {
                store.settings.selectedBackend == .codexAppServer ? .codexExec : store.settings.selectedBackend
            },
            set: { backend in
                Task { await store.switchBackend(backend) }
            }
        )
    }

    private func composerAccountName(_ account: AgentAccount) -> String {
        switch account.id {
        case "claude": return "Claude"
        case "claude-admin": return "Claude admin"
        case "claude-doksanbir": return "Claude personal"
        case "codex": return "Codex"
        default: return account.name
        }
    }

    private func composerModelName(_ model: String) -> String {
        switch model.lowercased() {
        case "opus": return "Opus"
        case "sonnet": return "Sonnet"
        case "haiku": return "Haiku"
        default: return model
        }
    }
}

private struct WorkspaceSlashCommandPalette: View {
    var suggestions: [WorkspaceSlashCommand]
    var onSelect: (WorkspaceSlashCommand) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(suggestions) { suggestion in
                Button {
                    onSelect(suggestion)
                } label: {
                    HStack(spacing: 9) {
                        Image(systemName: suggestion.systemImage)
                            .frame(width: 18)
                            .foregroundStyle(.secondary)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(suggestion.title)
                                .font(.caption.weight(.semibold))
                            Text(suggestion.detail)
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                        }
                        Spacer()
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .padding(6)
        .background(Color.secondary.opacity(0.10))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }
}

private struct WorkspaceReviewRail: View {
    @EnvironmentObject private var store: AppStore
    @Binding var workspaceTab: WorkspaceConsoleTab
    @State private var selectedTab: WorkspaceReviewTab = .changes
    @State private var showFolderHierarchy = true
    @State private var terminalCommand = ""
    @State private var confirmCreatePR = false
    @State private var prTitleDraft = ""
    @State private var prDescriptionDraft = ""
    @State private var todos: [String] = []

    var body: some View {
        VStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 8) {
                    Picker("Review", selection: $selectedTab) {
                        Text("All files").tag(WorkspaceReviewTab.files)
                        Text("Changes").tag(WorkspaceReviewTab.changes)
                        Text("Checks").tag(WorkspaceReviewTab.checks)
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .help(reviewFiles.isEmpty ? "No reviewable changed files" : "Workspace has reviewable file changes")

                    Toggle(isOn: $showFolderHierarchy) {
                        Image(systemName: "point.3.connected.trianglepath.dotted")
                    }
                    .toggleStyle(.button)
                    .buttonStyle(.borderless)
                    .help(showFolderHierarchy ? "Show flat file list" : "Show folder hierarchy")

                    Menu {
                        Button("Refresh workspace") {
                            Task { await store.refreshAll() }
                        }
                        Button("Copy changed file list") {
                            copyChangedFileList()
                        }
                        .disabled(reviewFiles.isEmpty)
                    } label: {
                        Image(systemName: "ellipsis")
                            .frame(width: 22, height: 22)
                    }
                    .menuStyle(.button)
                    .buttonStyle(.borderless)
                }

                reviewContent
            }
            .padding(14)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background(Color.secondary.opacity(0.06))
            .clipShape(RoundedRectangle(cornerRadius: 8))

            WorkspaceDock(command: $terminalCommand)
                .frame(height: 270, alignment: .topLeading)
        }
        .confirmationDialog("Create pull request?", isPresented: $confirmCreatePR) {
            Button("Open PR creation") {
                Task { await store.createWorkspacePullRequest() }
            }
            Button("Cancel", role: .cancel) {}
        }
    }

    @ViewBuilder
    private var reviewContent: some View {
        switch selectedTab {
        case .files:
            WorkspaceFilesSummary(showHierarchy: showFolderHierarchy) { path in
                selectFile(path)
            }
        case .changes:
            if reviewFiles.isEmpty {
                VStack(spacing: 6) {
                    Image(systemName: "point.3.connected.trianglepath.dotted")
                        .font(.title)
                        .foregroundStyle(.secondary)
                    Text(store.workspace.changedFiles.isEmpty ? "No file changes yet" : "No review changes")
                        .font(.subheadline.weight(.medium))
                    Text(store.workspace.changedFiles.isEmpty ? "Changes appear here." : "Generated and runtime files are kept out of review.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, minHeight: 180)
            } else {
                WorkspaceChangedFilesList(files: reviewFiles, showHierarchy: showFolderHierarchy) { path in
                    selectFile(path)
                }
            }
        case .checks:
            checksPanel
        }
    }

    private var reviewFiles: [WorkspaceFileChange] {
        store.workspace.changedFiles.filter { !isHiddenWorkspaceChange($0.path) }
    }

    private var checksPanel: some View {
        VStack(alignment: .leading, spacing: 14) {
            WorkspaceDeliverySignals()

            Divider()

            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text(prTitle)
                        .font(.headline)
                    Text(prSubtitle)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
                Button {
                    confirmCreatePR = true
                } label: {
                    Label("Create PR", systemImage: "arrow.triangle.pull")
                }
                .disabled(reviewFiles.isEmpty)
            }

            VStack(alignment: .leading, spacing: 8) {
                TextField("PR title", text: $prTitleDraft)
                    .textFieldStyle(.roundedBorder)
                TextField("PR description", text: $prDescriptionDraft, axis: .vertical)
                    .textFieldStyle(.roundedBorder)
                    .lineLimit(2...4)
                HStack {
                    Text("Your todos")
                        .font(.subheadline.weight(.medium))
                    Spacer()
                    Button {
                        todos.append("Review workspace")
                    } label: {
                        Label("Add", systemImage: "plus")
                    }
                    .buttonStyle(.borderless)
                }
                if todos.isEmpty {
                    Text("No todos yet")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(todos.indices, id: \.self) { index in
                        HStack(spacing: 8) {
                            Image(systemName: "circle")
                                .foregroundStyle(.secondary)
                            Text(todos[index])
                                .lineLimit(1)
                            Spacer()
                        }
                        .font(.caption)
                    }
                }
            }

            if displayChecks.isEmpty {
                CompactEmptyState(title: "No checks", systemImage: "checklist")
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 10) {
                        ForEach(displayChecks) { check in
                            HStack {
                                Label(check.name, systemImage: checkSystemImage(check))
                                    .lineLimit(1)
                                Spacer()
                                Text(check.conclusion ?? check.status)
                                    .font(.caption)
                                    .foregroundStyle(checkColor(check))
                            }
                        }
                    }
                }
            }
        }
    }

    private var displayChecks: [WorkspaceCheck] {
        if !store.workspace.checks.isEmpty {
            return store.workspace.checks
        }

        return localChecks
    }

    private var localChecks: [WorkspaceCheck] {
        guard let run = store.workspaceRuns.first(where: { run in
            run.commandLine.contains(" task ") || run.commandLine.contains("'task'")
        }) else {
            return []
        }

        let combined = [run.output, run.stderr]
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .joined(separator: "\n")
        let passed = run.exitCode == 0
        var checks = [
            WorkspaceCheck(name: "Pilot task", status: passed ? "completed" : "failed", conclusion: passed ? "SUCCESS" : "FAILURE")
        ]

        if combined.contains("go build ./...") {
            checks.append(WorkspaceCheck(name: "go build ./...", status: passed ? "completed" : "failed", conclusion: passed ? "SUCCESS" : "FAILURE"))
        }
        if combined.contains("make test") {
            checks.append(WorkspaceCheck(name: "make test", status: passed ? "completed" : "failed", conclusion: passed ? "SUCCESS" : "FAILURE"))
        }
        if combined.contains("Quality Passed") || combined.contains("all_passed=true") {
            checks.append(WorkspaceCheck(name: "Quality gates", status: "completed", conclusion: "SUCCESS"))
        } else if combined.contains("Quality gate") {
            checks.append(WorkspaceCheck(name: "Quality gates", status: passed ? "completed" : "failed", conclusion: passed ? "SUCCESS" : "FAILURE"))
        }

        return checks
    }

    private func checkSystemImage(_ check: WorkspaceCheck) -> String {
        switch check.conclusion {
        case "SUCCESS":
            return "checkmark.circle"
        case "FAILURE":
            return "xmark.circle"
        default:
            return "circle.dashed"
        }
    }

    private func checkColor(_ check: WorkspaceCheck) -> Color {
        switch check.conclusion {
        case "SUCCESS":
            return .green
        case "FAILURE":
            return .red
        default:
            return .secondary
        }
    }

    private var prTitle: String {
        if let pr = store.workspace.pullRequest {
            return "PR #\(pr.number)"
        }
        return "No PR"
    }

    private var prSubtitle: String {
        if let pr = store.workspace.pullRequest {
            return [pr.state, pr.reviewDecision].compactMap { $0 }.joined(separator: " / ")
        }
        return reviewFiles.isEmpty ? "workspace clean" : "changes ready for review"
    }

    private func copyChangedFileList() {
        let value = reviewFiles.map(\.path).joined(separator: "\n")
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
    }

    private func selectFile(_ path: String) {
        store.selectWorkspaceFile(path)
        workspaceTab = .files
    }

}

private struct WorkspaceChangedFilesList: View {
    var files: [WorkspaceFileChange]
    var showHierarchy: Bool
    var onSelect: (String) -> Void

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                if showHierarchy {
                    ForEach(groupedFiles, id: \.directory) { group in
                        HStack(spacing: 8) {
                            Image(systemName: "folder")
                                .foregroundStyle(.secondary)
                                .frame(width: 18)
                            Text(group.directory)
                                .font(.caption.weight(.semibold))
                                .lineLimit(1)
                            Spacer()
                        }
                        .padding(.vertical, 7)

                        ForEach(group.files) { file in
                            WorkspaceFileRow(file: file, leadingIndent: 16, onSelect: onSelect)
                            Divider()
                        }
                    }
                } else {
                    ForEach(files) { file in
                        WorkspaceFileRow(file: file, onSelect: onSelect)
                        Divider()
                    }
                }
            }
        }
    }

    private var groupedFiles: [(directory: String, files: [WorkspaceFileChange])] {
        Dictionary(grouping: files) { file in
            let parts = file.path.split(separator: "/", omittingEmptySubsequences: true)
            guard parts.count > 1 else { return "Root" }
            return String(parts.dropLast().joined(separator: "/"))
        }
        .map { (directory: $0.key, files: $0.value.sorted { $0.path.localizedStandardCompare($1.path) == .orderedAscending }) }
        .sorted { $0.directory.localizedStandardCompare($1.directory) == .orderedAscending }
    }
}

private struct WorkspaceFilesSummary: View {
    @EnvironmentObject private var store: AppStore
    var showHierarchy: Bool
    var onSelect: (String) -> Void
    @State private var expandedDirectories: Set<String> = ["cmd", "internal", "native-macos"]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(visibleEntries) { entry in
                        fileTreeRow(entry: entry)
                        Divider()
                    }
                }
            }
        }
    }

    private var root: String {
        store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
    }

    private var visibleEntries: [WorkspaceFileTreeEntry] {
        var entries: [WorkspaceFileTreeEntry] = []
        collectEntries(in: "", depth: 0, into: &entries)
        return entries
    }

    private func collectEntries(in directory: String, depth: Int, into entries: inout [WorkspaceFileTreeEntry]) {
        guard entries.count < 260 else { return }
        for entry in children(in: directory, depth: depth) {
            entries.append(entry)
            if showHierarchy, entry.isDirectory, expandedDirectories.contains(entry.path) {
                collectEntries(in: entry.path, depth: depth + 1, into: &entries)
            }
        }
    }

    private func children(in directory: String, depth: Int) -> [WorkspaceFileTreeEntry] {
        let absoluteDirectory = directory.isEmpty ? root : "\(root)/\(directory)"
        let names = (try? FileManager.default.contentsOfDirectory(atPath: absoluteDirectory)) ?? []
        return names
            .filter { !$0.hasPrefix(".") && $0 != ".build" && $0 != "dist" }
            .filter { directory.isEmpty ? !isHiddenWorkspaceTopLevelEntry($0) : true }
            .sorted { lhs, rhs in
                let lhsPath = directory.isEmpty ? lhs : "\(directory)/\(lhs)"
                let rhsPath = directory.isEmpty ? rhs : "\(directory)/\(rhs)"
                let lhsDir = isDirectory(lhsPath)
                let rhsDir = isDirectory(rhsPath)
                if lhsDir != rhsDir { return lhsDir && !rhsDir }
                return lhs.localizedStandardCompare(rhs) == .orderedAscending
            }
            .prefix(depth == 0 ? 48 : 80)
            .map { name in
                let path = directory.isEmpty ? name : "\(directory)/\(name)"
                return WorkspaceFileTreeEntry(path: path, name: name, isDirectory: isDirectory(path), depth: depth)
            }
    }

    private func isDirectory(_ relativePath: String) -> Bool {
        var isDirectory: ObjCBool = false
        FileManager.default.fileExists(atPath: "\(root)/\(relativePath)", isDirectory: &isDirectory)
        return isDirectory.boolValue
    }

    private func fileTreeRow(entry: WorkspaceFileTreeEntry) -> some View {
        Button {
            if entry.isDirectory {
                toggleDirectory(entry.path)
            } else {
                onSelect(entry.path)
            }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: entry.isDirectory ? disclosureIcon(for: entry.path) : fileIcon(entry.name))
                    .foregroundStyle(.secondary)
                    .frame(width: 16)
                Text(entry.path)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer()
            }
            .font(entry.depth == 0 ? .subheadline : .caption)
            .padding(.leading, CGFloat(entry.depth) * 16)
            .padding(.vertical, entry.depth == 0 ? 7 : 5)
        }
        .buttonStyle(.plain)
    }

    private func disclosureIcon(for path: String) -> String {
        expandedDirectories.contains(path) ? "folder.fill" : "folder"
    }

    private func toggleDirectory(_ path: String) {
        if expandedDirectories.contains(path) {
            expandedDirectories.remove(path)
        } else {
            expandedDirectories.insert(path)
        }
    }

    private func fileIcon(_ name: String) -> String {
        if name == ".gitignore" || name.hasSuffix(".git") { return "point.3.connected.trianglepath.dotted" }
        if name.hasSuffix(".go") { return "chevron.left.forwardslash.chevron.right" }
        if name.hasSuffix(".md") { return "doc.text" }
        if name.hasSuffix(".yaml") || name.hasSuffix(".yml") || name.hasSuffix(".toml") { return "slider.horizontal.3" }
        return "doc"
    }
}

private struct WorkspaceFileTreeEntry: Identifiable {
    var path: String
    var name: String
    var isDirectory: Bool
    var depth: Int

    var id: String { path }
}

private struct WorkspaceDock: View {
    @EnvironmentObject private var store: AppStore
    @Binding var command: String
    @StateObject private var terminal = WorkspaceTerminalController()
    @State private var selectedTab: WorkspaceDockTab = .terminal
    @State private var runMode: WorkspaceRunMode = .shell
    @State private var pilotPrompt = "Inspect the current workspace, run the smallest relevant verification, and report merge blockers."
    @State private var architectSchedule = "*/30 * * * *"
    @State private var architectTimezone = TimeZone.current.identifier
    @State private var architectLens = "core"

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                Image(systemName: "chevron.down")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Picker("Workspace dock", selection: $selectedTab) {
                    ForEach(WorkspaceDockTab.allCases) { tab in
                        Text(tab.rawValue).tag(tab)
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                Spacer(minLength: 4)
                if selectedTab == .terminal {
                    Button {
                        terminal.clear()
                    } label: {
                        Image(systemName: "trash")
                            .frame(width: 22, height: 22)
                    }
                    .help("Clear terminal")

                    Button {
                        terminal.restart()
                    } label: {
                        Image(systemName: terminal.isRunning ? "arrow.clockwise" : "play.fill")
                            .frame(width: 22, height: 22)
                    }
                    .help(terminal.isRunning ? "Restart terminal" : "Start terminal")
                }
            }
            .buttonStyle(.borderless)
            .controlSize(.small)
            .frame(height: 32, alignment: .center)

            Group {
                switch selectedTab {
                case .setup:
                    WorkspaceSetupDock()
                case .run:
                    WorkspaceRunDock(
                        command: $command,
                        mode: $runMode,
                        pilotPrompt: $pilotPrompt,
                        architectSchedule: $architectSchedule,
                        architectTimezone: $architectTimezone,
                        architectLens: $architectLens
                    )
                case .terminal:
                    WorkspaceTerminalDock(terminal: terminal)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
        .padding(.top, 6)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private var workspaceName: String {
        workspaceCopyName(from: root)
    }

    private var branchName: String {
        store.workspace.branch.isEmpty ? "detached" : store.workspace.branch
    }

    private var promptLine: String {
        workspaceTerminalPrompt(
            workspace: workspaceName,
            branch: branchName,
            modified: !reviewableChangedFiles.isEmpty
        )
    }

    private var root: String {
        store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
    }

    private var reviewableChangedFiles: [WorkspaceFileChange] {
        store.workspace.changedFiles.filter { !isHiddenWorkspaceChange($0.path) }
    }
}

private struct WorkspaceSetupDock: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Label("Setup", systemImage: "wrench.and.screwdriver")
                    .font(.headline)
                Spacer()
                StatusBadge(text: setupOptions.isEmpty ? "manual" : "detected")
            }
            Text(setupSummary)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(2)
            if !setupOptions.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(setupOptions.prefix(3), id: \.command) { option in
                        HStack(spacing: 8) {
                            Image(systemName: option.systemImage)
                                .foregroundStyle(.secondary)
                                .frame(width: 18)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(option.title)
                                    .font(.caption.weight(.semibold))
                                Text(option.command)
                                    .font(.system(.caption2, design: .monospaced))
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                            }
                            Spacer()
                            Button("Run") {
                                Task { await store.runWorkspaceCommand(option.command, title: "setup") }
                            }
                        }
                    }
                }
                .padding(10)
                .background(Color.primary.opacity(0.035))
                .clipShape(RoundedRectangle(cornerRadius: 8))
            }
            HStack(spacing: 8) {
                Button {
                    Task { await store.runWorkspaceCommand(defaultSetupCommand, title: "setup") }
                } label: {
                    Label("Run setup", systemImage: "play.fill")
                }
                .disabled(defaultSetupCommand.isEmpty)
                Button {
                    Task { await store.runWorkspacePrompt("Create or improve the setup script for this workspace. Keep it project-local, idempotent, and safe to run before agent tasks.") }
                } label: {
                    Label("Ask agent", systemImage: "sparkles")
                }
                Button {
                    Task { await store.runWorkspaceCommand(createSetupScriptCommand, title: "setup-script") }
                } label: {
                    Label("Create script", systemImage: "doc.badge.plus")
                }
            }
        }
    }

    private var setupSummary: String {
        if let first = setupOptions.first {
            return "Detected \(first.title). Running setup executes a real command in this workspace."
        }
        return "No setup script was detected. Create one or ask the agent to add a project-local setup path."
    }

    private var defaultSetupCommand: String {
        setupOptions.first?.command ?? createSetupScriptCommand
    }

    private var setupOptions: [WorkspaceSetupOption] {
        var options: [WorkspaceSetupOption] = []
        if exists("script/setup.sh") {
            options.append(.init(title: "Project setup script", command: "chmod +x script/setup.sh && ./script/setup.sh", systemImage: "wrench.and.screwdriver"))
        }
        if exists("scripts/setup.sh") {
            options.append(.init(title: "Scripts setup", command: "chmod +x scripts/setup.sh && ./scripts/setup.sh", systemImage: "wrench.and.screwdriver"))
        }
        if exists("Makefile") {
            options.append(.init(title: "Make setup", command: "make setup", systemImage: "hammer"))
        }
        if exists("go.mod") {
            options.append(.init(title: "Go dependencies", command: "go mod download", systemImage: "shippingbox"))
        }
        if exists("package.json") {
            options.append(.init(title: "Node dependencies", command: packageManagerInstallCommand, systemImage: "shippingbox"))
        }
        if exists("native-macos/Pilot91/Package.swift") {
            options.append(.init(title: "Swift package resolve", command: "swift package resolve --package-path native-macos/Pilot91", systemImage: "swift"))
        }
        return options
    }

    private var packageManagerInstallCommand: String {
        if exists("pnpm-lock.yaml") { return "pnpm install" }
        if exists("yarn.lock") { return "yarn install" }
        if exists("package-lock.json") { return "npm ci" }
        return "npm install"
    }

    private var createSetupScriptCommand: String {
        "mkdir -p script && test -f script/setup.sh || printf '%s\\n' '#!/usr/bin/env bash' 'set -euo pipefail' '' > script/setup.sh; chmod +x script/setup.sh; open script/setup.sh"
    }

    private func exists(_ relativePath: String) -> Bool {
        FileManager.default.fileExists(atPath: "\(root)/\(relativePath)")
    }

    private var root: String {
        store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
    }
}

private struct WorkspaceSetupOption {
    var title: String
    var command: String
    var systemImage: String
}

private struct WorkspaceRunDock: View {
    @EnvironmentObject private var store: AppStore
    @Binding var command: String
    @Binding var mode: WorkspaceRunMode
    @Binding var pilotPrompt: String
    @Binding var architectSchedule: String
    @Binding var architectTimezone: String
    @Binding var architectLens: String

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Picker("Run mode", selection: $mode) {
                ForEach(WorkspaceRunMode.allCases) { mode in
                    Text(mode.rawValue).tag(mode)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(maxWidth: .infinity)

            switch mode {
            case .shell:
                shellRunner
            case .task:
                taskRunner(title: "Run Pilot task", localAutopilot: false)
            case .autopilot:
                taskRunner(title: store.isAutopilotTaskRunning ? "Autopilot running..." : "Run Autopilot task", localAutopilot: true)
            case .architect:
                architectRunner
            }
        }
    }

    private var shellRunner: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Run tests or a development command in this worktree.")
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack {
                TextField("go test ./...", text: $command)
                    .textFieldStyle(.roundedBorder)
                    .font(.system(.body, design: .monospaced))
                Button {
                    let value = command.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "go test ./..." : command
                    Task { await store.runWorkspaceCommand(value) }
                } label: {
                    Label("Run", systemImage: "play.fill")
                }
            }
        }
    }

    private func taskRunner(title: String, localAutopilot: Bool) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(localAutopilot ? "Runs Autopilot preflight plus a local Pilot task in this worktree." : "Runs a real local Pilot task with the selected backend.")
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(2)
            TextField("Task prompt", text: $pilotPrompt, axis: .vertical)
                .textFieldStyle(.roundedBorder)
                .lineLimit(2...4)
            HStack {
                Button {
                    let prompt = pilotPrompt
                    Task {
                        if localAutopilot {
                            await store.runAutopilotLocalTask(description: prompt)
                        } else {
                            await store.runWorkspaceTask(prompt)
                        }
                    }
                } label: {
                    Label(title, systemImage: localAutopilot ? "arrow.triangle.2.circlepath" : "hammer")
                }
                .disabled(localAutopilot && store.isAutopilotTaskRunning)

                Picker("Backend", selection: Binding(
                    get: { store.settings.selectedBackend == .codexAppServer ? .codexExec : store.settings.selectedBackend },
                    set: { backend in Task { await store.switchBackend(backend) } }
                )) {
                    ForEach(ExecutionBackend.allCases.filter { $0 != .codexAppServer }) { backend in
                        Text(backend.label).tag(backend)
                    }
                }
                .pickerStyle(.menu)
                .frame(width: 150)

                Spacer()
                StatusBadge(text: localAutopilot && store.isAutopilotTaskRunning ? "running" : store.settings.selectedBackend.label)
            }
        }
    }

    private var architectRunner: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Create the scheduler config or run a dry Architect scan without opening tickets.")
                .font(.caption)
                .foregroundStyle(.secondary)
            Grid(alignment: .leading, horizontalSpacing: 8, verticalSpacing: 6) {
                GridRow {
                    Text("Schedule").foregroundStyle(.secondary)
                    TextField("*/30 * * * *", text: $architectSchedule)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.body, design: .monospaced))
                    Text("Timezone").foregroundStyle(.secondary)
                    TextField("Europe/Istanbul", text: $architectTimezone)
                        .textFieldStyle(.roundedBorder)
                }
                GridRow {
                    Text("Lens").foregroundStyle(.secondary)
                    Picker("Lens", selection: $architectLens) {
                        ForEach(["core", "radar", "refactor", "rfc", "depdoctor", "testgap"], id: \.self) { lens in
                            Text(lens).tag(lens)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.menu)
                    EmptyView()
                    EmptyView()
                }
            }
            .font(.subheadline)
            HStack {
                Button {
                    Task {
                        await store.scheduleArchitectRadar(
                            schedule: architectSchedule,
                            timezone: architectTimezone,
                            backend: store.settings.selectedBackend
                        )
                    }
                } label: {
                    Label("Create schedule", systemImage: "calendar.badge.plus")
                }
                Button {
                    Task { await store.runArchitectScan(lens: architectLens) }
                } label: {
                    Label("Run scan", systemImage: "scope")
                }
                Spacer()
                StatusBadge(text: store.settings.selectedBackend.label)
            }
        }
    }
}

private struct WorkspaceDeliverySignals: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Text("Workspace health")
                .font(.subheadline.weight(.semibold))
            signal("Changes", value: changesValue, status: reviewableChangedFiles.isEmpty ? "clean" : "review")
            signal("Last run", value: lastRunValue, status: lastRunStatus)
            signal("Autopilot", value: autopilotValue, status: store.autopilot.activePRs.isEmpty ? "idle" : "tracking")
            signal("Architect", value: architectValue, status: architectStatus)
        }
    }

    private func signal(_ title: String, value: String, status: String) -> some View {
        HStack(spacing: 8) {
            Text(title)
                .foregroundStyle(.secondary)
                .frame(width: 82, alignment: .leading)
            Text(value)
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(maxWidth: .infinity, alignment: .leading)
            StatusBadge(text: status)
        }
        .font(.caption)
    }

    private var changesValue: String {
        guard !reviewableChangedFiles.isEmpty else { return "clean" }
        let reviewable = reviewableChangedFiles.count
        return reviewable == 1 ? "1 review file" : "\(reviewable) review files"
    }

    private var reviewableChangedFiles: [WorkspaceFileChange] {
        store.workspace.changedFiles.filter { !isHiddenWorkspaceChange($0.path) }
    }

    private var lastRunValue: String {
        guard let run = store.workspaceRuns.first else { return "none" }
        return run.exitCode.map { "\(run.title) exit \($0)" } ?? "\(run.title) running"
    }

    private var lastRunStatus: String {
        guard let run = store.workspaceRuns.first else { return "empty" }
        guard let exitCode = run.exitCode else { return "running" }
        return exitCode == 0 ? "success" : "failed"
    }

    private var autopilotValue: String {
        if store.isAutopilotTaskRunning { return "local task running" }
        if store.autopilot.activePRs.isEmpty { return "idle" }
        return "\(store.autopilot.activePRs.count) tracked PRs"
    }

    private var architectRun: CommandRun? {
        store.workspaceRuns.first { run in
            let text = "\(run.title) \(run.commandLine)".lowercased()
            return text.contains("architect")
        }
    }

    private var architectValue: String {
        guard let architectRun else { return "not scanned" }
        return architectRun.exitCode == 0 ? "last scan complete" : "scan failed"
    }

    private var architectStatus: String {
        guard let architectRun else { return "empty" }
        return architectRun.exitCode == 0 ? "success" : "failed"
    }
}

private struct WorkspaceTerminalDock: View {
    @EnvironmentObject private var store: AppStore
    @ObservedObject var terminal: WorkspaceTerminalController

    var body: some View {
        WorkspaceSwiftTermSurface(
            terminal: terminal,
            cwd: root,
            prompt: promptLine,
            isReady: terminalReady,
            fontName: terminalFontName,
            fontSize: terminalFontSize
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityLabel("Workspace terminal")
    }

    private var terminalFontName: String {
        let fontName = store.settings.terminalFont.trimmingCharacters(in: .whitespacesAndNewlines)
        return fontName.isEmpty ? store.settings.monoFont.fontName : fontName
    }

    private var terminalFontSize: CGFloat {
        min(CGFloat(store.settings.terminalFontSize), 12)
    }

    private var promptLine: String {
        workspaceTerminalPrompt(
            workspace: workspaceName,
            branch: branchName,
            modified: !reviewableChangedFiles.isEmpty
        )
    }

    private var terminalReady: Bool {
        !store.workspace.repoRoot.isEmpty && !store.workspace.branch.isEmpty
    }

    private var root: String {
        store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
    }

    private var workspaceName: String {
        workspaceCopyName(from: root)
    }

    private var branchName: String {
        store.workspace.branch.isEmpty ? "detached" : store.workspace.branch
    }

    private var reviewableChangedFiles: [WorkspaceFileChange] {
        store.workspace.changedFiles.filter { !isHiddenWorkspaceChange($0.path) }
    }
}

@MainActor
private final class WorkspaceTerminalController: ObservableObject {
    @Published var isRunning = false
    @Published private(set) var actionRevision = 0

    private(set) var action: WorkspaceTerminalAction = .none

    func clear() {
        action = .clear
        actionRevision += 1
    }

    func restart() {
        action = .restart
        actionRevision += 1
    }
}

private enum WorkspaceTerminalAction {
    case none
    case clear
    case restart
}

private struct WorkspaceSwiftTermSurface: NSViewRepresentable {
    @ObservedObject var terminal: WorkspaceTerminalController
    let cwd: String
    let prompt: String
    let isReady: Bool
    let fontName: String
    let fontSize: CGFloat

    func makeCoordinator() -> Coordinator {
        Coordinator(controller: terminal)
    }

    func makeNSView(context: Context) -> LocalProcessTerminalView {
        let terminalView = WorkspaceLocalTerminalView(frame: .zero)
        configure(terminalView)
        terminalView.processDelegate = context.coordinator
        terminalView.onReadyToStart = { [weak coordinator = context.coordinator] in
            coordinator?.startIfReady()
        }
        context.coordinator.attach(terminalView)
        context.coordinator.update(cwd: cwd, prompt: prompt, isReady: isReady)
        DispatchQueue.main.async {
            context.coordinator.startIfReady()
            terminalView.window?.makeFirstResponder(terminalView)
        }
        return terminalView
    }

    func updateNSView(_ terminalView: LocalProcessTerminalView, context: Context) {
        configure(terminalView)
        context.coordinator.update(cwd: cwd, prompt: prompt, isReady: isReady)
        if context.coordinator.actionRevision != terminal.actionRevision {
            context.coordinator.actionRevision = terminal.actionRevision
            switch terminal.action {
            case .none:
                break
            case .clear:
                context.coordinator.clear()
            case .restart:
                context.coordinator.restart()
            }
        }
    }

    private func configure(_ terminalView: LocalProcessTerminalView) {
        terminalView.font = resolvedTerminalFont(name: fontName, size: fontSize)
        terminalView.nativeForegroundColor = NSColor(red: 0.86, green: 0.88, blue: 0.86, alpha: 1)
        terminalView.nativeBackgroundColor = NSColor(red: 0.06, green: 0.055, blue: 0.055, alpha: 1)
        terminalView.layer?.backgroundColor = terminalView.nativeBackgroundColor.cgColor
        terminalView.caretColor = NSColor(red: 0.62, green: 1.0, blue: 0.68, alpha: 1)
        terminalView.getTerminal().setCursorStyle(.steadyBlock)
        terminalView.autoresizingMask = [.width, .height]
        do {
            try terminalView.setUseMetal(false)
        } catch {
            // CoreText rendering is the stable fallback for this compact dock.
        }
    }

    final class Coordinator: NSObject, LocalProcessTerminalViewDelegate {
        weak var controller: WorkspaceTerminalController?
        weak var terminalView: LocalProcessTerminalView?
        var cwd = ""
        var prompt = ""
        var actionRevision = 0
        private var pendingCwd = ""
        private var pendingPrompt = ""
        private var isReady = false
        private var hasStarted = false

        init(controller: WorkspaceTerminalController) {
            self.controller = controller
        }

        func attach(_ terminalView: LocalProcessTerminalView) {
            self.terminalView = terminalView
        }

        func update(cwd: String, prompt: String, isReady: Bool) {
            pendingCwd = cwd
            pendingPrompt = prompt
            self.isReady = isReady
            guard hasStarted else {
                startIfReady()
                return
            }
            if self.cwd != cwd {
                start(cwd: cwd, prompt: prompt)
            } else if self.prompt != prompt {
                self.prompt = prompt
            }
        }

        func startIfReady() {
            guard isReady, !hasStarted, let terminalView, terminalView.bounds.width > 80, terminalView.bounds.height > 40 else { return }
            start(cwd: pendingCwd, prompt: pendingPrompt)
        }

        func restart() {
            start(cwd: pendingCwd, prompt: pendingPrompt)
        }

        private func start(cwd: String, prompt: String) {
            guard let terminalView else { return }
            self.cwd = cwd
            self.prompt = prompt
            hasStarted = true
            terminalView.terminate()
            terminalView.getTerminal().resetToInitialState()
            terminalView.startProcess(
                executable: "/bin/zsh",
                args: ["-i"],
                environment: terminalEnvironment(prompt: prompt),
                currentDirectory: cwd
            )
            Task { @MainActor [weak controller] in
                controller?.isRunning = true
            }
        }

        func clear() {
            guard let terminalView else { return }
            let bytes = Array("\u{000C}".utf8)
            terminalView.send(source: terminalView, data: bytes[...])
        }

        func sizeChanged(source: LocalProcessTerminalView, newCols: Int, newRows: Int) {}
        func setTerminalTitle(source: LocalProcessTerminalView, title: String) {}
        func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}

        func processTerminated(source: TerminalView, exitCode: Int32?) {
            Task { @MainActor [weak controller] in
                controller?.isRunning = false
            }
        }
    }
}

private final class WorkspaceLocalTerminalView: LocalProcessTerminalView {
    var onReadyToStart: (() -> Void)?

    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        onReadyToStart?()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        DispatchQueue.main.async { [weak self] in
            self?.onReadyToStart?()
        }
    }
}

private func terminalEnvironment(prompt: String) -> [String] {
    var environment = ProcessInfo.processInfo.environment
    environment["TERM"] = "xterm-256color"
    environment["CLICOLOR"] = "1"
    environment["NO_COLOR"] = nil
    environment["PILOT_TERMINAL"] = "1"
    if let zdotdir = writePilotZshRuntime(prompt: prompt) {
        environment["ZDOTDIR"] = zdotdir.path
    }
    return environment.map { "\($0.key)=\($0.value)" }
}

private func writePilotZshRuntime(prompt: String) -> URL? {
    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("Pilot91Terminal", isDirectory: true)
        .appendingPathComponent(String(prompt.hashValue).replacingOccurrences(of: "-", with: "n"), isDirectory: true)
    let zshrc = root.appendingPathComponent(".zshrc")
    let promptValue = "%F{cyan}\(zshPromptEscaped(prompt))%f "
    let body = """
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
export TERM=xterm-256color
export CLICOLOR=1
alias ll='ls -laG'
alias la='ls -laG'
PROMPT=\(shellSingleQuoted(promptValue))
RPROMPT=''
RPS1=''
PROMPT_EOL_MARK=''
setopt interactive_comments
printf '\\033[H\\033[2J\\033[3J'

"""
    do {
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        try body.write(to: zshrc, atomically: true, encoding: .utf8)
        return root
    } catch {
        return nil
    }
}

private func zshPromptEscaped(_ value: String) -> String {
    value
        .replacingOccurrences(of: "%", with: "%%")
        .replacingOccurrences(of: "\n", with: " ")
        .replacingOccurrences(of: "\r", with: " ")
}

private func shellSingleQuoted(_ value: String) -> String {
    "'\(value.replacingOccurrences(of: "'", with: "'\\''"))'"
}

private func resolvedTerminalFont(name: String, size: CGFloat) -> NSFont {
    if let font = NSFont(name: name, size: size) {
        return font
    }
    return NSFont.monospacedSystemFont(ofSize: size, weight: .regular)
}

private extension CodeTheme {
    var nsBackground: NSColor {
        switch self {
        case .catppuccinLatte: return NSColor(red: 0.94, green: 0.93, blue: 0.88, alpha: 1)
        case .catppuccinMacchiato: return NSColor(red: 0.15, green: 0.15, blue: 0.22, alpha: 1)
        case .catppuccinMocha: return NSColor(red: 0.12, green: 0.11, blue: 0.16, alpha: 1)
        case .dracula: return NSColor(red: 0.16, green: 0.16, blue: 0.21, alpha: 1)
        case .nord: return NSColor(red: 0.18, green: 0.20, blue: 0.25, alpha: 1)
        case .tokyoNight: return NSColor(red: 0.09, green: 0.10, blue: 0.16, alpha: 1)
        case .gruvboxDark: return NSColor(red: 0.16, green: 0.13, blue: 0.10, alpha: 1)
        case .solarizedDark: return NSColor(red: 0.00, green: 0.17, blue: 0.21, alpha: 1)
        case .standard: return NSColor.textBackgroundColor
        }
    }

    var nsComment: NSColor {
        switch self {
        case .catppuccinLatte: return NSColor(red: 0.42, green: 0.44, blue: 0.54, alpha: 1)
        case .nord: return NSColor(red: 0.38, green: 0.44, blue: 0.53, alpha: 1)
        case .solarizedDark: return NSColor(red: 0.51, green: 0.58, blue: 0.59, alpha: 1)
        default: return .secondaryLabelColor
        }
    }

    var nsKeyword: NSColor {
        switch self {
        case .dracula: return NSColor(red: 1.00, green: 0.47, blue: 0.78, alpha: 1)
        case .nord: return NSColor(red: 0.50, green: 0.69, blue: 0.80, alpha: 1)
        case .tokyoNight: return NSColor(red: 0.73, green: 0.44, blue: 1.00, alpha: 1)
        case .gruvboxDark: return NSColor(red: 0.98, green: 0.29, blue: 0.20, alpha: 1)
        case .solarizedDark: return NSColor(red: 0.52, green: 0.60, blue: 0.00, alpha: 1)
        default: return NSColor.systemRed
        }
    }

    var nsFunction: NSColor {
        switch self {
        case .dracula: return NSColor(red: 0.74, green: 0.58, blue: 0.98, alpha: 1)
        case .nord: return NSColor(red: 0.53, green: 0.75, blue: 0.82, alpha: 1)
        case .tokyoNight: return NSColor(red: 0.48, green: 0.68, blue: 1.00, alpha: 1)
        case .gruvboxDark: return NSColor(red: 0.98, green: 0.74, blue: 0.18, alpha: 1)
        case .solarizedDark: return NSColor(red: 0.15, green: 0.55, blue: 0.82, alpha: 1)
        default: return NSColor.systemPurple
        }
    }

    var nsString: NSColor {
        switch self {
        case .dracula: return NSColor(red: 0.95, green: 0.98, blue: 0.55, alpha: 1)
        case .nord: return NSColor(red: 0.64, green: 0.75, blue: 0.55, alpha: 1)
        case .tokyoNight: return NSColor(red: 0.62, green: 0.84, blue: 0.52, alpha: 1)
        case .gruvboxDark: return NSColor(red: 0.72, green: 0.73, blue: 0.15, alpha: 1)
        case .solarizedDark: return NSColor(red: 0.52, green: 0.60, blue: 0.00, alpha: 1)
        default: return NSColor.systemGreen
        }
    }

    var nsType: NSColor {
        NSColor(red: 0.95, green: 0.58, blue: 0.20, alpha: 1)
    }

    var nsNumber: NSColor {
        NSColor.systemOrange
    }
}
