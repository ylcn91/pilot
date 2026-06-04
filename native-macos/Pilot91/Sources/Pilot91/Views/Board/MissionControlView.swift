import SwiftUI

private func missionProjectName(from root: String) -> String {
    let url = URL(fileURLWithPath: root)
    if let codexIndex = url.pathComponents.firstIndex(of: ".Codex"), codexIndex > 1 {
        return url.pathComponents[codexIndex - 1]
    }
    return url.lastPathComponent
}

private func missionWorkspaceName(from root: String) -> String {
    URL(fileURLWithPath: root).lastPathComponent
}

struct MissionControlView: View {
    @EnvironmentObject private var store: AppStore
    @State private var selectedProjectFilter = "all"

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            dashboardHeader
            projectTabs
            board
        }
        .task {
            await store.refreshProjectWorkspaces()
        }
    }

    private var dashboardHeader: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Dashboard")
                    .font(.title2.weight(.semibold))
                Text("Workspace work, reviews and completed runs")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            Button {
                Task { await store.createWorkspaceCopy() }
            } label: {
                Label("New workspace", systemImage: "plus.square")
            }
            .disabled(store.isCreatingWorkspace)

            Button {
                store.selectedSection = .history
            } label: {
                Label("History", systemImage: "clock.arrow.circlepath")
            }

            Button {
                Task { await store.refreshAll() }
            } label: {
                Label("Refresh", systemImage: "arrow.clockwise")
            }
        }
    }

    private var projectTabs: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                projectTab(title: "All projects", id: "all")
                ForEach(projectOptions, id: \.self) { project in
                    projectTab(title: project, id: project)
                }
            }
            .padding(4)
            .background(Color.secondary.opacity(0.06))
            .clipShape(RoundedRectangle(cornerRadius: 8))
        }
    }

    private func projectTab(title: String, id: String) -> some View {
        Button {
            selectedProjectFilter = id
        } label: {
            Text(title)
                .font(.subheadline.weight(selectedProjectFilter == id ? .semibold : .regular))
                .lineLimit(1)
                .padding(.horizontal, 12)
                .frame(height: 30)
                .background(selectedProjectFilter == id ? Color.accentColor : Color.clear)
                .foregroundStyle(selectedProjectFilter == id ? .white : .primary)
                .clipShape(RoundedRectangle(cornerRadius: 6))
        }
        .buttonStyle(.plain)
    }

    private var board: some View {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                ForEach(boardColumns) { column in
                    boardColumn(column)
                }
            }
            .padding(.bottom, 8)
        }
    }

    private func boardColumn(_ column: MissionBoardColumn) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Text(column.title)
                    .font(.headline)
                if column.cards.isEmpty {
                    Text(column.emptyTitle)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
            }

            if column.cards.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: column.emptyImage)
                        .font(.system(size: 22, weight: .medium))
                        .foregroundStyle(.tertiary)
                    Text(column.emptyTitle)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
                .frame(maxWidth: .infinity, minHeight: 128)
                .padding(14)
                .background(Color.secondary.opacity(0.055))
                .clipShape(RoundedRectangle(cornerRadius: 8))
            } else {
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(column.cards) { card in
                        boardCard(card)
                    }
                }
            }
        }
        .padding(12)
        .frame(width: 292, alignment: .topLeading)
        .background(Color.secondary.opacity(0.07))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func boardCard(_ card: MissionBoardCard) -> some View {
        Button {
            open(card)
        } label: {
            VStack(alignment: .leading, spacing: 10) {
                HStack(alignment: .top, spacing: 9) {
                    Image(systemName: card.systemImage)
                        .foregroundStyle(card.tint)
                        .frame(width: 18)

                    VStack(alignment: .leading, spacing: 3) {
                        Text(card.title)
                            .font(.subheadline.weight(.semibold))
                            .lineLimit(2)
                        Text(card.subtitle)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                    }

                    Spacer(minLength: 6)
                }

                Text(card.detail)
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .lineLimit(2)
                    .frame(minHeight: 28, alignment: .topLeading)

                HStack(spacing: 6) {
                    StatusBadge(text: card.status)
                    Spacer()
                    Text(card.project)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                }
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.primary.opacity(0.045))
            .clipShape(RoundedRectangle(cornerRadius: 8))
        }
        .buttonStyle(.plain)
    }

    private var boardColumns: [MissionBoardColumn] {
        [
            MissionBoardColumn(
                id: "backlog",
                title: "Backlog",
                emptyTitle: "No ticket waiting",
                emptyImage: "tray",
                cards: filteredCards(backlogCards)
            ),
            MissionBoardColumn(
                id: "in-progress",
                title: "In progress",
                emptyTitle: "No active workspace",
                emptyImage: "point.3.connected.trianglepath.dotted",
                cards: filteredCards(inProgressCards)
            ),
            MissionBoardColumn(
                id: "in-review",
                title: "In review",
                emptyTitle: "No PR waiting",
                emptyImage: "arrow.triangle.pull",
                cards: filteredCards(inReviewCards)
            ),
            MissionBoardColumn(
                id: "done",
                title: "Done",
                emptyTitle: "No completed work",
                emptyImage: "checkmark.circle",
                cards: filteredCards(doneCards)
            ),
            MissionBoardColumn(
                id: "canceled",
                title: "Canceled",
                emptyTitle: "No failed work",
                emptyImage: "xmark.octagon",
                cards: filteredCards(canceledCards)
            )
        ]
    }

    private var backlogCards: [MissionBoardCard] {
        store.queue
            .filter { task in isBacklog(task.status) }
            .map { taskCard($0, fallbackStatus: "queued", destination: .queue, image: "tray.full", tint: .orange) }
    }

    private var inProgressCards: [MissionBoardCard] {
        workspaceCards
            + store.queue
                .filter { task in isRunning(task.status) }
                .map { taskCard($0, fallbackStatus: "running", destination: .queue, image: "progress.indicator", tint: .blue) }
            + recentRuns
                .filter(\.isRunning)
                .prefix(4)
                .map { runCard($0, status: "running", destination: .logs, image: "terminal", tint: .blue) }
    }

    private var inReviewCards: [MissionBoardCard] {
        var cards: [MissionBoardCard] = []

        if let pullRequest = store.workspace.pullRequest {
            cards.append(
                MissionBoardCard(
                    id: "pr-\(pullRequest.number)",
                    title: "#\(pullRequest.number) \(pullRequest.title)",
                    subtitle: pullRequest.reviewDecision ?? pullRequest.state,
                    detail: pullRequest.url,
                    project: projectName,
                    status: "review",
                    systemImage: "arrow.triangle.pull",
                    tint: .orange,
                    destination: .workspaceConsole,
                    workspace: nil
                )
            )
        }

        cards += store.queue
            .filter { task in isReview(task.status) }
            .map { taskCard($0, fallbackStatus: "review", destination: .queue, image: "doc.text.magnifyingglass", tint: .orange) }

        return cards
    }

    private var doneCards: [MissionBoardCard] {
        let completedRuns = recentRuns
            .filter { ($0.exitCode ?? 1) == 0 && !$0.isRunning }
            .prefix(4)
            .map { runCard($0, status: "done", destination: .logs, image: "checkmark.circle", tint: .green) }

        let completedHistory = store.history
            .filter { entry in isDone(entry.status) }
            .prefix(8)
            .map { historyCard($0, status: "done", image: "checkmark.circle", tint: .green) }

        return Array(completedRuns) + Array(completedHistory)
    }

    private var canceledCards: [MissionBoardCard] {
        let failedRuns = recentRuns
            .filter { run in (run.exitCode ?? 0) != 0 && !run.isRunning }
            .prefix(4)
            .map { runCard($0, status: "failed", destination: .logs, image: "xmark.octagon", tint: .red) }

        let failedHistory = store.history
            .filter { entry in isCanceled(entry.status) }
            .prefix(8)
            .map { historyCard($0, status: "failed", image: "xmark.octagon", tint: .red) }

        return Array(failedRuns) + Array(failedHistory)
    }

    private var workspaceCards: [MissionBoardCard] {
        visibleWorkspaces.map { workspace in
            let isCurrent = workspace.isCurrent
            let changed = isCurrent ? reviewableChangedFiles.count : 0
            let status = isCurrent && changed > 0 ? "modified" : (isCurrent ? "current" : "active")
            let detail: String
            if isCurrent, changed > 0 {
                detail = changed == 1 ? "1 changed file ready to review" : "\(changed) changed files ready to review"
            } else if isCurrent {
                detail = store.workspace.statusSummary.isEmpty ? "Current workspace" : store.workspace.statusSummary
            } else {
                detail = "Open this workspace"
            }

            return MissionBoardCard(
                id: "workspace-\(workspace.path)",
                title: workspace.name,
                subtitle: workspace.branch.isEmpty ? "detached" : workspace.branch,
                detail: detail,
                project: missionProjectName(from: workspace.path),
                status: status,
                systemImage: isCurrent ? "largecircle.fill.circle" : "point.3.connected.trianglepath.dotted",
                tint: isCurrent ? .green : .secondary,
                destination: .workspaceConsole,
                workspace: workspace
            )
        }
    }

    private func taskCard(
        _ task: QueueTask,
        fallbackStatus: String,
        destination: SidebarSection,
        image: String,
        tint: Color
    ) -> MissionBoardCard {
        MissionBoardCard(
            id: "task-\(task.id)",
            title: task.title,
            subtitle: task.issueID,
            detail: task.projectPath.isEmpty ? "Pilot task" : task.projectPath,
            project: task.projectPath.isEmpty ? projectName : missionProjectName(from: task.projectPath),
            status: task.status.isEmpty ? fallbackStatus : task.status,
            systemImage: image,
            tint: tint,
            destination: destination,
            workspace: nil
        )
    }

    private func runCard(
        _ run: CommandRun,
        status: String,
        destination: SidebarSection,
        image: String,
        tint: Color
    ) -> MissionBoardCard {
        MissionBoardCard(
            id: "run-\(run.id.uuidString)",
            title: run.title,
            subtitle: run.startedAt.formatted(date: .omitted, time: .shortened),
            detail: run.commandLine,
            project: projectName,
            status: status,
            systemImage: image,
            tint: tint,
            destination: destination,
            workspace: nil
        )
    }

    private func historyCard(
        _ entry: HistoryEntry,
        status: String,
        image: String,
        tint: Color
    ) -> MissionBoardCard {
        MissionBoardCard(
            id: "history-\(entry.id)",
            title: entry.title,
            subtitle: entry.issueID,
            detail: entry.completedAt.isEmpty ? entry.status : entry.completedAt,
            project: entry.projectPath.isEmpty ? projectName : missionProjectName(from: entry.projectPath),
            status: status,
            systemImage: image,
            tint: tint,
            destination: .history,
            workspace: nil
        )
    }

    private func open(_ card: MissionBoardCard) {
        if let workspace = card.workspace {
            if workspace.isCurrent {
                store.selectedSection = .workspaceConsole
            } else {
                Task { await store.switchWorkspace(workspace) }
            }
            return
        }

        if let destination = card.destination {
            store.selectedSection = destination
        }
    }

    private func filteredCards(_ cards: [MissionBoardCard]) -> [MissionBoardCard] {
        guard selectedProjectFilter != "all" else { return cards }
        return cards.filter { $0.project == selectedProjectFilter }
    }

    private var projectOptions: [String] {
        var names = Set<String>()
        names.insert(projectName)
        visibleWorkspaces.forEach { names.insert(missionProjectName(from: $0.path)) }
        store.queue.forEach { task in
            if !task.projectPath.isEmpty {
                names.insert(missionProjectName(from: task.projectPath))
            }
        }
        store.history.forEach { entry in
            if !entry.projectPath.isEmpty {
                names.insert(missionProjectName(from: entry.projectPath))
            }
        }
        return names.sorted { lhs, rhs in
            if lhs == projectName { return true }
            if rhs == projectName { return false }
            return lhs.localizedCaseInsensitiveCompare(rhs) == .orderedAscending
        }
    }

    private var projectName: String {
        missionProjectName(from: effectivePath)
    }

    private var workspaceTitle: String {
        missionWorkspaceName(from: effectivePath)
    }

    private var effectivePath: String {
        store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
    }

    private var reviewableChangedFiles: [WorkspaceFileChange] {
        store.workspace.changedFiles.filter { file in
            let normalized = file.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            let topLevelName = normalized.split(separator: "/", maxSplits: 1).first.map(String.init) ?? normalized
            if file.path.hasPrefix(".agent/tasks/task-") && file.path.hasSuffix(".md") {
                return false
            }
            return ![".build", ".claude", ".codex", ".git", ".pilot", "dist"].contains(topLevelName)
        }
    }

    private var visibleWorkspaces: [WorkspaceListItem] {
        let items: [WorkspaceListItem]
        if store.projectWorkspaces.isEmpty {
            items = [currentWorkspaceItem]
        } else {
            items = store.projectWorkspaces
        }

        let filtered = items.filter { workspace in
            workspace.isCurrent || isHumanWorkspace(workspace)
        }
        if filtered.contains(where: \.isCurrent) {
            return filtered
        }
        return [currentWorkspaceItem] + filtered
    }

    private var currentWorkspaceItem: WorkspaceListItem {
        WorkspaceListItem(
            path: effectivePath,
            name: workspaceTitle,
            branch: store.workspace.branch.isEmpty ? "detached" : store.workspace.branch,
            isCurrent: true
        )
    }

    private func isHumanWorkspace(_ workspace: WorkspaceListItem) -> Bool {
        if workspace.path == canonicalProjectRoot {
            return false
        }
        if workspace.name == projectName {
            return false
        }
        if workspace.name.hasPrefix("pilot-worktree-GH-") {
            return false
        }
        if workspace.name.range(of: #"^pilot-\d{8}-\d{6}$"#, options: .regularExpression) != nil {
            return false
        }
        return true
    }

    private var canonicalProjectRoot: String {
        let url = URL(fileURLWithPath: effectivePath)
        if let codexIndex = url.pathComponents.firstIndex(of: ".Codex"), codexIndex > 1 {
            let rootComponents = Array(url.pathComponents.prefix(codexIndex))
            return NSString.path(withComponents: rootComponents)
        }
        return URL(fileURLWithPath: effectivePath).standardizedFileURL.path
    }

    private var recentRuns: [CommandRun] {
        var seen = Set<String>()
        return (store.workspaceRuns + store.commandRuns)
            .sorted { $0.startedAt > $1.startedAt }
            .filter { run in
                let bucket = Int(run.startedAt.timeIntervalSince1970 / 5)
                let key = "\(run.title)|\(run.commandLine)|\(bucket)"
                if seen.contains(key) {
                    return false
                }
                seen.insert(key)
                return true
            }
    }

    private func isBacklog(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.isEmpty
            || value.contains("queued")
            || value.contains("pending")
            || value.contains("wait")
            || value.contains("backlog")
    }

    private func isRunning(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.contains("run")
            || value.contains("active")
            || value.contains("progress")
            || value.contains("execut")
    }

    private func isReview(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.contains("review")
            || value.contains("pr")
            || value.contains("handoff")
    }

    private func isDone(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.contains("success")
            || value.contains("done")
            || value.contains("complete")
            || value.contains("merged")
    }

    private func isCanceled(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.contains("fail")
            || value.contains("cancel")
            || value.contains("error")
            || value.contains("blocked")
    }
}

private struct MissionBoardColumn: Identifiable {
    var id: String
    var title: String
    var emptyTitle: String
    var emptyImage: String
    var cards: [MissionBoardCard]
}

private struct MissionBoardCard: Identifiable {
    var id: String
    var title: String
    var subtitle: String
    var detail: String
    var project: String
    var status: String
    var systemImage: String
    var tint: Color
    var destination: SidebarSection?
    var workspace: WorkspaceListItem?
}
