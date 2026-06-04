import Foundation
import SwiftUI

struct MetricsView: View {
    @EnvironmentObject private var store: AppStore

    private let columns = [
        GridItem(.adaptive(minimum: 220), spacing: 12)
    ]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                LazyVGrid(columns: columns, alignment: .leading, spacing: 12) {
                    kpiCard(
                        "Ship readiness",
                        value: deliveryConfidence,
                        subtitle: deliverySubtitle,
                        status: deliveryStatus,
                        systemImage: "checkmark.seal"
                    )
                    kpiCard(
                        "Verification",
                        value: verificationHeadline,
                        subtitle: verificationSubtitle,
                        status: verificationStatus,
                        systemImage: "testtube.2"
                    )
                    kpiCard(
                        "Code quality",
                        value: "\(codeQualityScore)/100",
                        subtitle: codeQualitySubtitle,
                        status: codeQualityStatus,
                        systemImage: "checkmark.shield"
                    )
                    kpiCard(
                        "Architecture risk",
                        value: architectureHeadline,
                        subtitle: architectureSubtitle,
                        status: architectureStatus,
                        systemImage: "scope"
                    )
                }

                Panel(title: "Delivery Pipeline", subtitle: "Live task, queue and PR flow from the daemon plus local desktop runs") {
                    VStack(alignment: .leading, spacing: 10) {
                        signalRow("Gateway", value: store.serverRunning ? "online" : "offline", status: store.serverRunning ? "connected" : "offline")
                        signalRow("Queue", value: store.queue.isEmpty ? "clear" : "work waiting", status: store.queue.isEmpty ? "clear" : "active")
                        signalRow("Autopilot", value: store.autopilot.activePRs.isEmpty ? "idle" : "tracking PRs", status: store.autopilot.activePRs.isEmpty ? "idle" : "tracking")
                        signalRow("Desktop runs", value: runs.isEmpty ? "no local runs" : "recent command output", status: runs.isEmpty ? "empty" : "active")
                    }
                }

                Panel(title: "Quality Gates", subtitle: "Latest checks run from the desktop workspace") {
                    if verificationRuns.isEmpty {
                        CompactEmptyState(title: "No checks run yet", systemImage: "checklist")
                    } else {
                        VStack(alignment: .leading, spacing: 9) {
                            ForEach(Array(verificationRuns.prefix(5))) { run in
                                runRow(run)
                                Divider()
                            }
                        }
                    }
                }

                Panel(title: "Code Quality", subtitle: "Signals derived from changed files and Architect output") {
                    VStack(alignment: .leading, spacing: 10) {
                        signalRow("Changed source files", value: changedSourceFiles.isEmpty ? "clean" : "source changes need review", status: changedSourceFiles.isEmpty ? "clean" : "review")
                        signalRow("Review size", value: reviewSizeLabel, status: reviewSizeStatus)
                        signalRow("Architect findings", value: architectureHeadline, status: architectureStatus)
                        if let architectRun {
                            Text(architectRun.output.trimmingCharacters(in: .whitespacesAndNewlines))
                                .font(.system(.caption, design: .monospaced))
                                .foregroundStyle(.secondary)
                                .lineLimit(8)
                                .textSelection(.enabled)
                        }
                    }
                }

                Panel(title: "Runtime & Spend", subtitle: "Provider readiness, backend choice and observed usage") {
                    VStack(alignment: .leading, spacing: 10) {
                        signalRow("Workspace account", value: "\(store.settings.selectedAgentAccount.name) / \(store.settings.selectedChatModel)", status: accountStatus)
                        signalRow("Task backend", value: store.settings.selectedBackend.label, status: store.settings.selectedBackend.rawValue)
                        signalRow("Token spend", value: PilotFormatters.compactNumber(observedTokens), status: observedTokens > 0 ? "tracked" : "empty")
                        signalRow("Cost", value: PilotFormatters.currency(observedCostUSD), status: observedCostUSD > 0 ? "tracked" : "empty")
                    }
                }
            }
            .padding(.bottom, 18)
        }
    }

    private func kpiCard(_ title: String, value: String, subtitle: String, status: String, systemImage: String) -> some View {
        Panel(title: title, subtitle: nil) {
            VStack(alignment: .leading, spacing: 10) {
                HStack(spacing: 10) {
                    Image(systemName: systemImage)
                        .font(.title2)
                        .foregroundStyle(.secondary)
                    Spacer()
                    StatusBadge(text: status)
                }
                Text(value)
                    .font(.system(size: 27, weight: .semibold, design: .rounded))
                    .lineLimit(1)
                    .minimumScaleFactor(0.72)
                Text(subtitle)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            .frame(minHeight: 92, alignment: .topLeading)
        }
    }

    private func signalRow(_ title: String, value: String, status: String) -> some View {
        HStack(spacing: 10) {
            Text(title)
                .foregroundStyle(.secondary)
                .frame(width: 150, alignment: .leading)
            Text(value)
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(maxWidth: .infinity, alignment: .leading)
            StatusBadge(text: status)
        }
        .font(.subheadline)
    }

    private func runRow(_ run: CommandRun) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Label(targetCheckLabel(for: run), systemImage: run.exitCode == 0 ? "checkmark.circle" : "xmark.octagon")
                Spacer()
                StatusBadge(text: run.exitCode == 0 ? "success" : "failed")
            }
            Text(checkResultLine(for: run))
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(2)
            if let finishedAt = run.finishedAt {
                Text(finishedAt.formatted(date: .omitted, time: .standard))
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
        }
    }

    private var runs: [CommandRun] {
        var seen = Set<String>()
        return (store.commandRuns + store.workspaceRuns)
            .sorted { $0.startedAt > $1.startedAt }
            .filter { run in
                let key = "\(run.title)|\(run.commandLine)|\(run.startedAt.timeIntervalSince1970)"
                if seen.contains(key) {
                    return false
                }
                seen.insert(key)
                return true
            }
    }

    private var completedRuns: [CommandRun] {
        runs.filter { $0.exitCode != nil }
    }

    private var successfulRuns: [CommandRun] {
        completedRuns.filter { $0.exitCode == 0 }
    }

    private var deliveryConfidence: String {
        if !completedRuns.isEmpty {
            let rate = Double(successfulRuns.count) / Double(completedRuns.count) * 100
            return "\(Int(rate.rounded()))%"
        }
        if store.metrics.totalTasks > 0 {
            let rate = Double(store.metrics.succeededTasks) / Double(store.metrics.totalTasks) * 100
            return "\(Int(rate.rounded()))%"
        }
        return "-"
    }

    private var deliverySubtitle: String {
        if !completedRuns.isEmpty {
            return "\(successfulRuns.count)/\(completedRuns.count) desktop runs succeeded"
        }
        return "\(store.metrics.succeededTasks)/\(store.metrics.totalTasks) daemon tasks succeeded"
    }

    private var deliveryStatus: String {
        guard !completedRuns.isEmpty || store.metrics.totalTasks > 0 else { return "empty" }
        return failedRun == nil ? "success" : "review"
    }

    private var failedRun: CommandRun? {
        completedRuns.first { ($0.exitCode ?? 0) != 0 }
    }

    private var verificationRuns: [CommandRun] {
        runs.filter { run in
            let haystack = "\(run.title) \(run.commandLine) \(run.output)".lowercased()
            return haystack.contains("go test")
                || haystack.contains("swift build")
                || haystack.contains("config validate")
                || haystack.contains("architect")
                || haystack.contains("doctor")
                || haystack.contains("quality gates")
                || haystack.contains("quality passed")
        }
    }

    private var latestVerification: CommandRun? {
        verificationRuns.first
    }

    private var verificationHeadline: String {
        guard let latestVerification else { return "not run" }
        return latestVerification.exitCode == 0 ? "passing" : "failing"
    }

    private var verificationSubtitle: String {
        guard let latestVerification else { return "Run a check from Workspace, Architect or Commands" }
        return targetCheckLabel(for: latestVerification)
    }

    private var verificationStatus: String {
        guard let latestVerification else { return "empty" }
        return latestVerification.exitCode == 0 ? "success" : "failed"
    }

    private var changedSourceFiles: [WorkspaceFileChange] {
        reviewableChangedFiles.filter { file in
            [".go", ".swift", ".ts", ".tsx", ".js", ".py", ".java", ".rs"].contains { file.path.hasSuffix($0) }
        }
    }

    private var workspaceAdditions: Int {
        reviewableChangedFiles.reduce(0) { $0 + $1.additions }
    }

    private var workspaceDeletions: Int {
        reviewableChangedFiles.reduce(0) { $0 + $1.deletions }
    }

    private var reviewSizeLabel: String {
        guard !reviewableChangedFiles.isEmpty else { return "clean" }
        let churn = workspaceAdditions + workspaceDeletions
        if churn >= 500 { return "large review" }
        if churn >= 120 { return "broad review" }
        return "focused review"
    }

    private var reviewSizeStatus: String {
        guard !reviewableChangedFiles.isEmpty else { return "clean" }
        let churn = workspaceAdditions + workspaceDeletions
        return churn >= 500 ? "risk" : "review"
    }

    private var currentBranch: String {
        store.workspace.branch.isEmpty ? "detached" : store.workspace.branch
    }

    private var workspaceDeltaSubtitle: String {
        reviewableChangedFiles.isEmpty ? currentBranch : "Review changed files on \(currentBranch)"
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

    private var codeQualityScore: Int {
        var score = 100

        if let latestVerification {
            if latestVerification.exitCode != 0 {
                score -= 45
            }
        } else {
            score -= 20
        }

        let churn = workspaceAdditions + workspaceDeletions
        if churn >= 1_000 {
            score -= 25
        } else if churn >= 500 {
            score -= 18
        } else if churn >= 120 {
            score -= 10
        }

        if changedSourceFiles.count >= 20 {
            score -= 12
        } else if changedSourceFiles.count >= 8 {
            score -= 6
        }

        score -= architectRiskPenalty

        return max(0, min(100, score))
    }

    private var codeQualitySubtitle: String {
        if latestVerification == nil {
            return "No recent checks"
        }
        if architectureStatus == "empty" {
            return "\(reviewSizeLabel); Architect pending"
        }
        return "\(reviewSizeLabel); \(architectureHeadline)"
    }

    private var codeQualityStatus: String {
        if codeQualityScore >= 85 { return "success" }
        if codeQualityScore >= 65 { return "review" }
        return "risk"
    }

    private var architectRun: CommandRun? {
        runs.first { run in
            let haystack = "\(run.title) \(run.commandLine)".lowercased()
            return haystack.contains("architect")
        }
    }

    private var architectureHeadline: String {
        if !store.findings.isEmpty {
            return "needs review"
        }
        if let count = architectFindingCount, count > 0 {
            return "needs review"
        }
        if architectRun?.exitCode == 0 {
            return "clear"
        }
        return "not scanned"
    }

    private var architectureSubtitle: String {
        if let architectRun {
            if architectRun.exitCode != 0 {
                return "last scan failed"
            }
            return architectureStatus == "review" ? "last scan surfaced repo risk" : "last scan completed"
        }
        return "Schedule or run Architect to populate repo risk"
    }

    private var architectureStatus: String {
        if let architectRun, architectRun.exitCode != 0 {
            return "failed"
        }
        if !store.findings.isEmpty || (architectFindingCount ?? 0) > 0 {
            return "review"
        }
        return "empty"
    }

    private var architectFindingCount: Int? {
        guard let output = architectRun?.output else { return nil }
        if let range = output.range(of: #"findings=(\d+)"#, options: .regularExpression) {
            let match = String(output[range])
            return Int(match.replacingOccurrences(of: "findings=", with: ""))
        }
        if let range = output.range(of: #""count"\s*:\s*\d+"#, options: .regularExpression) {
            let match = String(output[range])
            return Int(match.components(separatedBy: CharacterSet.decimalDigits.inverted).joined())
        }
        return nil
    }

    private var accountStatus: String {
        store.accountStatus(for: store.settings.selectedAgentAccount).connected ? "connected" : "offline"
    }

    private var architectRiskPenalty: Int {
        let risks = store.findings.map { $0.risk.lowercased() }
        if risks.contains("high") || risks.contains("critical") { return 25 }
        if risks.contains("medium") { return 12 }
        if (architectFindingCount ?? 0) > 0 { return 12 }
        if architectureStatus == "empty" { return 8 }
        return 0
    }

    private var observedTokens: Int64 {
        store.metrics.totalTokens + runs.reduce(Int64(0)) { partial, run in
            partial + parsedTokenTotal(from: run.output)
        }
    }

    private var observedCostUSD: Double {
        store.metrics.totalCostUSD + runs.reduce(0) { partial, run in
            partial + parsedCostUSD(from: run.output)
        }
    }

    private func targetCheckLabel(for run: CommandRun) -> String {
        let command = run.commandLine
        let output = run.output
        let commandLower = command.lowercased()
        let haystack = "\(command)\n\(output)"

        if commandLower.contains(" config validate") || commandLower.contains("'config' 'validate'") {
            return "pilot config validate"
        }
        if commandLower.contains(" config show") || commandLower.contains("'config' 'show'") {
            return "pilot config show"
        }
        if commandLower.contains(" config set") || commandLower.contains("'config' 'set'") {
            if commandLower.contains("architect.") {
                return "Architect schedule"
            }
            return "pilot config set"
        }
        if commandLower.contains(" autopilot ") || commandLower.contains("'autopilot'") {
            return "Autopilot config"
        }
        if commandLower.contains(" task ") || commandLower.contains("'task'") {
            if commandLower.contains("--local") && commandLower.contains("--skip-self-review") {
                return "Autopilot local task"
            }
            return "Pilot task"
        }
        if commandLower.contains(" architect ") || commandLower.contains("'architect'") {
            return "Architect scan"
        }
        if haystack.contains("go test ./desktop -run TestQueueTaskBetter") {
            return "go test ./desktop -run TestQueueTaskBetter"
        }
        if haystack.contains("go test ./internal/autopilot -run TestSnapshotIsolation") {
            return "go test ./internal/autopilot -run TestSnapshotIsolation"
        }
        if haystack.contains("go test") {
            return firstGoTestCommand(in: haystack) ?? "go test"
        }
        if haystack.contains("swift build") {
            return "swift build"
        }
        if haystack.contains("config validate") {
            return "pilot config validate"
        }
        if haystack.lowercased().contains("quality gates") {
            return "Quality gates"
        }
        return run.title
    }

    private func checkResultLine(for run: CommandRun) -> String {
        if let taskID = firstCapture(#"Task:\s*(TASK-\d+)"#, in: run.output)
            ?? firstCapture(#"Task ID:\s*(TASK-\d+)"#, in: run.output) {
            if let result = goTestResultLine(in: run.output) {
                return "\(taskID) · \(result)"
            }
            if run.output.contains("all_passed=true") || run.output.contains("Quality Passed") {
                return "\(taskID) · quality gates passed"
            }
            return taskID
        }
        return goTestResultLine(in: run.output) ?? run.commandLine
    }

    private func goTestResultLine(in text: String) -> String? {
        text.split(separator: "\n")
            .map(String.init)
            .first { line in
                line.trimmingCharacters(in: .whitespaces).hasPrefix("ok")
            }
    }

    private func firstGoTestCommand(in text: String) -> String? {
        guard let command = firstCapture(#"go test [^'\"\\n]+"#, in: text) else { return nil }
        return command.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func parsedTokenTotal(from text: String) -> Int64 {
        let input = Int64(firstCapture(#"tokens_in=(\d+)"#, in: text) ?? "") ?? 0
        let output = Int64(firstCapture(#"tokens_out=(\d+)"#, in: text) ?? "") ?? 0
        return input + output
    }

    private func parsedCostUSD(from text: String) -> Double {
        Double(firstCapture(#"cost_usd=([0-9.]+)"#, in: text) ?? "") ?? 0
    }

    private func firstCapture(_ pattern: String, in text: String) -> String? {
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return nil }
        let range = NSRange(text.startIndex..<text.endIndex, in: text)
        guard let match = regex.firstMatch(in: text, range: range), match.numberOfRanges > 1 else { return nil }
        guard let captureRange = Range(match.range(at: 1), in: text) else { return nil }
        return String(text[captureRange])
    }
}
