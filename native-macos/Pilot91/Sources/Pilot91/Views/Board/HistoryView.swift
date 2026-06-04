import SwiftUI

private func historyProjectName(from root: String) -> String {
    let url = URL(fileURLWithPath: root)
    if let codexIndex = url.pathComponents.firstIndex(of: ".Codex"), codexIndex > 1 {
        return url.pathComponents[codexIndex - 1]
    }
    return url.lastPathComponent
}

private func historyWorkspaceName(from root: String) -> String {
    URL(fileURLWithPath: root).lastPathComponent
}

struct HistoryView: View {
    @EnvironmentObject private var store: AppStore
    @State private var filterText = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 10) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                TextField("Filter workspaces, commands, issues...", text: $filterText)
                    .textFieldStyle(.plain)
                if !filterText.isEmpty {
                    Button {
                        filterText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 9)
            .background(Color.secondary.opacity(0.08))
            .clipShape(RoundedRectangle(cornerRadius: 8))

            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    ForEach(Array(sections.enumerated()), id: \.offset) { _, section in
                        VStack(alignment: .leading, spacing: 8) {
                            Text(section.title)
                                .font(.headline)
                                .foregroundStyle(.secondary)

                            VStack(alignment: .leading, spacing: 0) {
                                ForEach(section.items) { item in
                                    WorkspaceHistoryRow(item: item) {
                                        open(item)
                                    }

                                    if item.id != section.items.last?.id {
                                        Divider()
                                    }
                                }
                            }
                        }
                    }

                    if sections.isEmpty {
                        CompactEmptyState(title: "No matching history", systemImage: "clock.arrow.circlepath")
                    }
                }
                .padding(.bottom, 18)
            }
        }
    }

    private var sections: [HistorySection] {
        let items = filteredItems
        var ordered: [HistorySection] = []
        for item in items {
            if let index = ordered.firstIndex(where: { $0.title == item.section }) {
                ordered[index].items.append(item)
            } else {
                ordered.append(HistorySection(title: item.section, items: [item]))
            }
        }
        return ordered
    }

    private var filteredItems: [HistoryDisplayItem] {
        let query = filterText.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !query.isEmpty else { return allItems }
        return allItems.filter { item in
            "\(item.repo) \(item.title) \(item.detail) \(item.time)"
                .lowercased()
                .contains(query)
        }
    }

    private var allItems: [HistoryDisplayItem] {
        let hasReviewChanges = !reviewableChangedFiles.isEmpty
        var items: [HistoryDisplayItem] = [
            HistoryDisplayItem(
                id: "current-workspace-\(effectivePath)",
                section: "Today",
                repo: repoName,
                title: workspaceTitle,
                detail: store.workspace.branch.isEmpty ? "detached" : store.workspace.branch,
                time: hasReviewChanges ? "modified" : "clean",
                systemImage: "largecircle.fill.circle",
                status: hasReviewChanges ? "review" : "clear",
                taskID: nil,
                projectPath: effectivePath
            )
        ]

        let runs = recentRuns
            .prefix(18)
            .map { run in
                HistoryDisplayItem(
                    id: "run-\(run.id.uuidString)",
                    section: dayLabel(for: run.startedAt),
                    repo: repoName,
                    title: run.title,
                    detail: run.commandLine,
                    time: run.isRunning ? "running" : run.startedAt.formatted(date: .omitted, time: .shortened),
                    systemImage: run.isRunning ? "progress.indicator" : (run.exitCode == 0 ? "checkmark.circle" : "xmark.octagon"),
                    status: run.isRunning ? "running" : (run.exitCode == 0 ? "success" : "failed"),
                    taskID: taskID(in: run.title) ?? taskID(in: run.commandLine),
                    projectPath: effectivePath
                )
            }
        items.append(contentsOf: runs)

        let daemon = store.history.prefix(18).map { entry in
            HistoryDisplayItem(
                id: "history-\(entry.id)",
                section: historySection(for: entry.completedAt),
                repo: URL(fileURLWithPath: entry.projectPath).lastPathComponent,
                title: entry.title,
                detail: "\(entry.issueID) / \(entry.status)",
                time: entry.completedAt.isEmpty ? entry.status : entry.completedAt,
                systemImage: entry.status.lowercased().contains("fail") ? "xmark.octagon" : "checkmark.circle",
                status: entry.status.lowercased().contains("fail") ? "failed" : "success",
                taskID: entry.id,
                projectPath: entry.projectPath
            )
        }
        items.append(contentsOf: daemon)
        return items
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

    private func open(_ item: HistoryDisplayItem) {
        if let taskID = item.taskID {
            store.selectedTaskID = taskID
        }
        Task {
            if let projectPath = item.projectPath, projectPath != store.settings.projectPath {
                await store.openProjectPath(projectPath)
            }
            store.selectedSection = item.taskID == nil ? .workspaceConsole : .logs
        }
    }

    private func taskID(in text: String) -> String? {
        guard let range = text.range(of: #"TASK-\d+"#, options: .regularExpression) else { return nil }
        return String(text[range])
    }

    private func dayLabel(for date: Date) -> String {
        let calendar = Calendar.current
        if calendar.isDateInToday(date) {
            return "Today"
        }
        if calendar.isDateInYesterday(date) {
            return "Yesterday"
        }
        return date.formatted(date: .abbreviated, time: .omitted)
    }

    private func historySection(for completedAt: String) -> String {
        let lowered = completedAt.lowercased()
        if lowered.contains("today") {
            return "Today"
        }
        if lowered.contains("yesterday") {
            return "Yesterday"
        }
        return completedAt.isEmpty ? "Daemon history" : "Older"
    }

    private var repoName: String {
        historyProjectName(from: effectivePath)
    }

    private var workspaceTitle: String {
        historyWorkspaceName(from: effectivePath)
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

    private var effectivePath: String {
        store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
    }
}

private struct HistorySection {
    var title: String
    var items: [HistoryDisplayItem]
}

private struct HistoryDisplayItem: Identifiable {
    var id: String
    var section: String
    var repo: String
    var title: String
    var detail: String
    var time: String
    var systemImage: String
    var status: String
    var taskID: String?
    var projectPath: String?
}

private struct WorkspaceHistoryRow: View {
    var item: HistoryDisplayItem
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: item.systemImage)
                    .foregroundStyle(iconColor)
                    .frame(width: 18)
                Text(item.repo)
                    .font(.subheadline.weight(.medium))
                    .frame(width: 120, alignment: .leading)
                    .lineLimit(1)
                Image(systemName: "chevron.right")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.title)
                        .font(.subheadline.weight(.medium))
                        .lineLimit(1)
                    Text(item.detail)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Spacer()
                StatusBadge(text: item.status)
                Text(item.time)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(width: 86, alignment: .trailing)
                    .lineLimit(1)
                HStack(spacing: 4) {
                    Text("Go to")
                    Image(systemName: "arrow.right")
                }
                .foregroundStyle(.secondary)
            }
        }
        .buttonStyle(.plain)
        .contentShape(Rectangle())
        .padding(.vertical, 9)
    }

    private var iconColor: Color {
        switch item.status.lowercased() {
        case "success", "clear": return .green
        case "running": return .blue
        case "failed": return .red
        case "review": return .orange
        default: return .secondary
        }
    }
}
