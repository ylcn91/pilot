import SwiftUI

private func taskBoardProjectName(from root: String) -> String {
    let url = URL(fileURLWithPath: root)
    if let codexIndex = url.pathComponents.firstIndex(of: ".Codex"), codexIndex > 1 {
        return url.pathComponents[codexIndex - 1]
    }
    return url.lastPathComponent
}

private func taskBoardWorkspaceName(from root: String) -> String {
    URL(fileURLWithPath: root).lastPathComponent
}

struct QueueView: View {
    @EnvironmentObject private var store: AppStore
    var compact = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if !compact {
                header
            }

            ScrollView([.horizontal, .vertical]) {
                HStack(alignment: .top, spacing: 12) {
                    ForEach(columns) { column in
                        taskColumn(column)
                    }
                }
                .padding(.bottom, 10)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
    }

    private var header: some View {
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 3) {
                Text("Task Board")
                    .font(.title2.weight(.semibold))
                Text("Tickets, active workspaces, handoffs and completed Pilot runs")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button {
                Task { await store.refreshAll() }
            } label: {
                Label("Refresh", systemImage: "arrow.clockwise")
            }
            Button {
                store.selectedSection = .workspaceConsole
            } label: {
                Label("Workspace", systemImage: "macwindow")
            }
            Button {
                store.selectedSection = .commandCenter
            } label: {
                Label("Run command", systemImage: "terminal")
            }
        }
    }

    private func taskColumn(_ column: TaskBoardColumn) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Rectangle()
                .fill(column.tint)
                .frame(height: 3)
                .clipShape(Capsule())

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
                emptyCard(title: column.emptyTitle, image: column.emptyImage)
            } else {
                LazyVStack(alignment: .leading, spacing: 10) {
                    ForEach(column.cards) { card in
                        taskCard(card)
                    }
                }
            }
            Spacer(minLength: 0)
        }
        .padding(12)
        .frame(width: compact ? 240 : 292, alignment: .topLeading)
        .frame(minHeight: compact ? 240 : 560, alignment: .topLeading)
        .background(Color.secondary.opacity(0.07))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func emptyCard(title: String, image: String) -> some View {
        VStack(spacing: 8) {
            Image(systemName: image)
                .font(.system(size: 22, weight: .medium))
                .foregroundStyle(.tertiary)
            Text(title)
                .font(.subheadline.weight(.medium))
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, minHeight: compact ? 92 : 128)
        .padding(14)
        .background(Color.primary.opacity(0.035))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func taskCard(_ card: TaskBoardCard) -> some View {
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
                    .lineLimit(3)
                    .frame(minHeight: compact ? 24 : 36, alignment: .topLeading)

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

    private var columns: [TaskBoardColumn] {
        [
            TaskBoardColumn(id: "waiting", title: "Waiting", emptyTitle: "No ticket waiting", emptyImage: "tray", cards: waitingCards),
            TaskBoardColumn(id: "working", title: "Working", emptyTitle: "No active workspace", emptyImage: "point.3.connected.trianglepath.dotted", cards: workingCards, tint: .blue),
            TaskBoardColumn(id: "review", title: "Review", emptyTitle: "No handoff pending", emptyImage: "arrow.triangle.pull", cards: reviewCards, tint: .orange),
            TaskBoardColumn(id: "done", title: "Done", emptyTitle: "No completed task", emptyImage: "checkmark.circle", cards: doneCards, tint: .green),
            TaskBoardColumn(id: "blocked", title: "Blocked", emptyTitle: "No blocked task", emptyImage: "xmark.octagon", cards: blockedCards, tint: .red)
        ]
    }

    private var waitingCards: [TaskBoardCard] {
        store.queue
            .filter { isWaiting($0.status) }
            .map { queueCard($0, status: $0.status.isEmpty ? "queued" : $0.status, image: "tray.full", tint: .orange) }
    }

    private var workingCards: [TaskBoardCard] {
        let queued = store.queue
            .filter { isWorking($0.status) }
            .map { queueCard($0, status: $0.status.isEmpty ? "running" : $0.status, image: "progress.indicator", tint: .blue) }

        let workspace = [currentWorkspaceCard]
        let running = recentRuns
            .filter(\.isRunning)
            .prefix(3)
            .map { runCard($0, status: "running", image: "terminal", tint: .blue) }

        return queued + workspace + Array(running)
    }

    private var reviewCards: [TaskBoardCard] {
        var cards = store.queue
            .filter { isReview($0.status) }
            .map { queueCard($0, status: $0.status.isEmpty ? "review" : $0.status, image: "doc.text.magnifyingglass", tint: .orange) }

        if let pr = store.workspace.pullRequest {
            cards.insert(
                TaskBoardCard(
                    id: "pr-\(pr.number)",
                    title: "#\(pr.number) \(pr.title)",
                    subtitle: pr.reviewDecision ?? pr.state,
                    detail: pr.url,
                    project: projectName,
                    status: "review",
                    systemImage: "arrow.triangle.pull",
                    tint: .orange,
                    destination: .workspaceConsole
                ),
                at: 0
            )
        }

        if !reviewableChangedFiles.isEmpty {
            cards.append(
                TaskBoardCard(
                    id: "changes-\(effectivePath)",
                    title: "Review local changes",
                    subtitle: reviewableChangedFiles.count == 1 ? "1 changed file" : "\(reviewableChangedFiles.count) changed files",
                    detail: store.workspace.diffStat.isEmpty ? store.workspace.statusSummary : store.workspace.diffStat,
                    project: projectName,
                    status: "review",
                    systemImage: "doc.text.magnifyingglass",
                    tint: .orange,
                    destination: .workspaceConsole
                )
            )
        }

        return cards
    }

    private var doneCards: [TaskBoardCard] {
        let runs = recentRuns
            .filter { ($0.exitCode ?? 1) == 0 && !$0.isRunning }
            .prefix(5)
            .map { runCard($0, status: "done", image: "checkmark.circle", tint: .green) }

        let history = store.history
            .filter { isDone($0.status) }
            .prefix(8)
            .map { historyCard($0, status: "done", image: "checkmark.circle", tint: .green) }

        return Array(runs) + Array(history)
    }

    private var blockedCards: [TaskBoardCard] {
        let runs = recentRuns
            .filter { run in (run.exitCode ?? 0) != 0 && !run.isRunning }
            .prefix(5)
            .map { runCard($0, status: "failed", image: "xmark.octagon", tint: .red) }

        let history = store.history
            .filter { isBlocked($0.status) }
            .prefix(8)
            .map { historyCard($0, status: "failed", image: "xmark.octagon", tint: .red) }

        return Array(runs) + Array(history)
    }

    private var currentWorkspaceCard: TaskBoardCard {
        let hasReviewChanges = !reviewableChangedFiles.isEmpty
        return TaskBoardCard(
            id: "workspace-\(effectivePath)",
            title: workspaceName,
            subtitle: store.workspace.branch.isEmpty ? "detached" : store.workspace.branch,
            detail: hasReviewChanges ? "\(reviewableChangedFiles.count) files changed in this workspace" : "Current workspace is ready for a task",
            project: projectName,
            status: hasReviewChanges ? "modified" : "active",
            systemImage: "largecircle.fill.circle",
            tint: hasReviewChanges ? .orange : .green,
            destination: .workspaceConsole
        )
    }

    private func queueCard(_ task: QueueTask, status: String, image: String, tint: Color) -> TaskBoardCard {
        TaskBoardCard(
            id: "queue-\(task.id)",
            title: task.title,
            subtitle: task.issueID,
            detail: task.projectPath.isEmpty ? "Pilot daemon task" : task.projectPath,
            project: task.projectPath.isEmpty ? projectName : taskBoardProjectName(from: task.projectPath),
            status: status,
            systemImage: image,
            tint: tint,
            destination: .queue
        )
    }

    private func runCard(_ run: CommandRun, status: String, image: String, tint: Color) -> TaskBoardCard {
        TaskBoardCard(
            id: "run-\(run.id.uuidString)",
            title: run.title,
            subtitle: run.startedAt.formatted(date: .omitted, time: .shortened),
            detail: run.commandLine,
            project: projectName,
            status: status,
            systemImage: image,
            tint: tint,
            destination: .logs
        )
    }

    private func historyCard(_ entry: HistoryEntry, status: String, image: String, tint: Color) -> TaskBoardCard {
        TaskBoardCard(
            id: "history-\(entry.id)",
            title: entry.title,
            subtitle: entry.issueID,
            detail: entry.completedAt.isEmpty ? entry.status : entry.completedAt,
            project: entry.projectPath.isEmpty ? projectName : taskBoardProjectName(from: entry.projectPath),
            status: status,
            systemImage: image,
            tint: tint,
            destination: .history
        )
    }

    private func open(_ card: TaskBoardCard) {
        if let destination = card.destination {
            store.selectedSection = destination
        }
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

    private var projectName: String {
        taskBoardProjectName(from: effectivePath)
    }

    private var workspaceName: String {
        taskBoardWorkspaceName(from: effectivePath)
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

    private func isWaiting(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.isEmpty || value.contains("queued") || value.contains("pending") || value.contains("wait")
    }

    private func isWorking(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.contains("running") || value.contains("active") || value.contains("progress") || value.contains("execut")
    }

    private func isReview(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.contains("review") || value.contains("pr") || value.contains("handoff")
    }

    private func isDone(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.contains("success") || value.contains("done") || value.contains("complete") || value.contains("merged")
    }

    private func isBlocked(_ status: String) -> Bool {
        let value = status.lowercased()
        return value.contains("fail") || value.contains("blocked") || value.contains("error") || value.contains("cancel")
    }
}

private struct TaskBoardColumn: Identifiable {
    var id: String
    var title: String
    var emptyTitle: String
    var emptyImage: String
    var cards: [TaskBoardCard]
    var tint: Color = .secondary
}

private struct TaskBoardCard: Identifiable {
    var id: String
    var title: String
    var subtitle: String
    var detail: String
    var project: String
    var status: String
    var systemImage: String
    var tint: Color
    var destination: SidebarSection?
}
