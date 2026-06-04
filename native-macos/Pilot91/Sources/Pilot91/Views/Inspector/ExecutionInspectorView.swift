import SwiftUI

struct ExecutionInspectorView: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Panel(title: "Inspector", subtitle: "Selected Pilot work") {
                    if let task = store.selectedTask {
                        VStack(alignment: .leading, spacing: 8) {
                            Text(task.title)
                                .font(.headline)
                            StatusBadge(text: task.status)
                            LabeledContent("Issue", value: task.issueID)
                            LabeledContent("Project", value: task.projectPath)
                            if let pr = task.prURL, let url = URL(string: pr) {
                                Link("Open PR", destination: url)
                            }
                            if let issue = task.issueURL, let url = URL(string: issue) {
                                Link("Open Issue", destination: url)
                            }
                        }
                    } else {
                        CompactEmptyState(title: "Select a task", systemImage: "sidebar.right")
                    }
                }

                Panel(title: "Runtime", subtitle: "Agent session") {
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            StatusBadge(text: store.runtime.status.rawValue)
                            Spacer()
                            Text(store.runtime.connected ? "connected" : "offline")
                                .foregroundStyle(.secondary)
                        }
                        Text(store.runtime.messages.isEmpty ? "No transcript yet" : "Transcript ready")
                            .foregroundStyle(.secondary)
                        Text(store.runtime.approvals.isEmpty ? "Approval queue clear" : "Approval required")
                            .foregroundStyle(store.runtime.approvals.isEmpty ? Color.secondary : Color.orange)
                    }
                }

                Panel(title: "Git Graph", subtitle: "Current project") {
                    if let error = store.gitGraph.error {
                        Text(error)
                            .foregroundStyle(.red)
                    } else if visibleGitLines.isEmpty {
                        CompactEmptyState(title: "No git graph data", systemImage: "point.3.connected.trianglepath.dotted")
                    } else {
                        VStack(alignment: .leading, spacing: 8) {
                            ForEach(visibleGitLines) { line in
                                VStack(alignment: .leading, spacing: 2) {
                                    if let refs = compactRefs(line.refs), !refs.isEmpty {
                                        Text(refs)
                                            .font(.caption2.weight(.semibold))
                                            .foregroundStyle(.secondary)
                                            .lineLimit(1)
                                    }
                                    Text(line.message ?? "Commit")
                                        .font(.caption)
                                        .lineLimit(2)
                                        .truncationMode(.tail)
                                }
                            }
                        }
                    }
                }

                if let error = store.lastError {
                    Panel(title: "Last Error") {
                        Text(error)
                            .font(.caption)
                            .foregroundStyle(.red)
                            .textSelection(.enabled)
                    }
                }
            }
            .padding(14)
        }
        .background(.background)
    }

    private var visibleGitLines: [GitGraphLine] {
        Array(store.gitGraph.lines.filter { line in
            let message = line.message?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            return !message.isEmpty && !message.hasPrefix("checkpoint:")
        }.prefix(8))
    }

    private func compactRefs(_ refs: String?) -> String? {
        guard let refs else { return nil }
        let values = refs
            .split(separator: ",")
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .compactMap { value -> String? in
                if value.hasPrefix("HEAD -> ") {
                    return value.replacingOccurrences(of: "HEAD -> refs/heads/", with: "")
                }
                if value.hasPrefix("refs/heads/") {
                    return value.replacingOccurrences(of: "refs/heads/", with: "")
                }
                if value.hasPrefix("refs/remotes/origin/") {
                    return value.replacingOccurrences(of: "refs/remotes/origin/", with: "origin/")
                }
                return nil
            }
            .prefix(3)
        let label = values.joined(separator: " / ")
        return label.isEmpty ? nil : label
    }
}
