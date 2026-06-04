import SwiftUI

struct LogsView: View {
    @EnvironmentObject private var store: AppStore
    var compact = false

    var body: some View {
        Panel(title: "Logs", subtitle: "Gateway and execution log stream") {
            if allEvents.isEmpty {
                emptyLogs
            } else {
                ScrollView {
                    Text(renderedEvents)
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .topLeading)
                        .padding(.vertical, 4)
                }
                .frame(minHeight: compact ? 180 : 500)
            }
        }
    }

    private var allEvents: [LogTimelineEvent] {
        let gateway = store.logs.reversed().map {
            LogTimelineEvent(
                timestamp: $0.ts,
                kind: $0.level,
                title: $0.message,
                detail: "",
                exitCode: nil,
                date: nil
            )
        }
        let command = store.commandRuns.map { run in
            LogTimelineEvent(
                timestamp: Self.formatter.string(from: run.finishedAt ?? run.startedAt),
                kind: run.title,
                title: run.commandLine,
                detail: run.output.trimmingCharacters(in: .whitespacesAndNewlines),
                exitCode: run.exitCode,
                date: run.finishedAt ?? run.startedAt
            )
        }
        let workspace = store.workspaceRuns.map { run in
            LogTimelineEvent(
                timestamp: Self.formatter.string(from: run.finishedAt ?? run.startedAt),
                kind: "workspace",
                title: run.commandLine,
                detail: run.output.trimmingCharacters(in: .whitespacesAndNewlines),
                exitCode: run.exitCode,
                date: run.finishedAt ?? run.startedAt
            )
        }
        return (command + workspace + gateway)
            .sorted { lhs, rhs in
                switch (lhs.date, rhs.date) {
                case let (l?, r?):
                    return l > r
                case (_?, nil):
                    return true
                case (nil, _?):
                    return false
                case (nil, nil):
                    return lhs.timestamp > rhs.timestamp
                }
            }
    }

    private var visibleEvents: [LogTimelineEvent] {
        let limit = compact ? 8 : 120
        return allEvents.enumerated().compactMap { index, event in
            index < limit ? event : nil
        }
    }

    private var renderedEvents: String {
        visibleEvents.map { event in
            let exit = event.exitCode.map { " exit \($0)" } ?? ""
            let detailText = Self.compactDetail(event.detail)
            let detail = detailText.isEmpty ? "" : "\n    \(detailText.replacingOccurrences(of: "\n", with: "\n    "))"
            return "[\(event.timestamp)] \(event.kind.uppercased())\(exit) \(event.title)\(detail)"
        }
        .joined(separator: "\n\n")
    }

    private static func compactDetail(_ detail: String) -> String {
        let trimmed = detail.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return "" }

        let lines = trimmed.components(separatedBy: .newlines)
        let maxLines = 80
        guard lines.count > maxLines else { return trimmed }

        let head = Array(lines.prefix(28))
        let tail = Array(lines.suffix(36))
        let omitted = lines.count - head.count - tail.count
        return (head + ["... \(omitted) lines omitted; latest output preserved below ..."] + tail).joined(separator: "\n")
    }

    private static let formatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "HH:mm:ss"
        return formatter
    }()

    private var emptyLogs: some View {
        VStack(alignment: .leading, spacing: 14) {
            if compact {
                CompactEmptyState(title: "No log entries", systemImage: "doc.text")
                    .frame(minHeight: 120)
            } else {
                HStack(spacing: 18) {
                    signal("Errors", value: "clear", status: "clear")
                    signal("Warnings", value: "clear", status: "clear")
                    signal("Gateway", value: store.serverRunning ? "online" : "offline", status: store.serverRunning ? "connected" : "offline")
                    signal("Stream", value: "ready", status: "live")
                }
                Divider()
                VStack(spacing: 8) {
                    logRow("Latest event", value: latestEventTitle, status: latestEventTitle == "No event yet" ? "empty" : "live")
                    logRow("Execution stream", value: store.commandRuns.isEmpty ? "No task output attached" : store.commandRuns[0].commandLine, status: store.commandRuns.isEmpty ? "empty" : "attached")
                    logRow("Next action", value: store.serverRunning ? "Run a task" : "Check daemon", status: "pending")
                }
                Divider()
                HStack {
                    Button {
                        Task { await store.runStatus() }
                    } label: {
                        Label("Check daemon", systemImage: "list.bullet.rectangle")
                    }
                    Button {
                        store.selectedSection = .missionControl
                    } label: {
                        Label("Open Mission Control", systemImage: "rectangle.3.group")
                    }
                }
            }
        }
    }

    private func signal(_ title: String, value: String, status: String) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.system(size: 24, weight: .semibold, design: .rounded))
            StatusBadge(text: status)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func logRow(_ title: String, value: String, status: String) -> some View {
        HStack {
            Text(title)
                .foregroundStyle(.secondary)
            Text(value)
                .lineLimit(1)
            Spacer()
            StatusBadge(text: status)
        }
        .font(.subheadline)
    }

    private var latestEventTitle: String {
        allEvents.first?.title ?? "No event yet"
    }
}

private struct LogTimelineEvent: Identifiable {
    var id = UUID()
    var timestamp: String
    var kind: String
    var title: String
    var detail: String
    var exitCode: Int32?
    var date: Date?
}
