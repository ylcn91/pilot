import SwiftUI

struct AutopilotView: View {
    @EnvironmentObject private var store: AppStore
    @State private var localTaskPrompt = "Inspect the current workspace changes, run the smallest relevant local verification, and report what should block merge."
    var compact = false

    var body: some View {
        Panel(title: "Autopilot", subtitle: store.autopilot.enabled ? "PR lifecycle active" : "PR lifecycle inactive") {
            VStack(alignment: .leading, spacing: 10) {
                Grid(alignment: .leading, horizontalSpacing: 18, verticalSpacing: 8) {
                    GridRow {
                        Text("State").foregroundStyle(.secondary)
                        StatusBadge(text: store.autopilot.enabled ? "enabled" : "disabled")
                    }
                    GridRow {
                        Text("Environment").foregroundStyle(.secondary)
                        Text(store.autopilot.environment.isEmpty ? "default" : store.autopilot.environment)
                    }
                    GridRow {
                        Text("Failures").foregroundStyle(.secondary)
                        Text("\(store.autopilot.failureCount)")
                    }
                    GridRow {
                        Text("Auto release").foregroundStyle(.secondary)
                        StatusBadge(text: store.autopilot.autoRelease ? "enabled" : "disabled")
                    }
                }
                .font(.subheadline)

                if !compact {
                    localTaskRunner
                    Divider()
                }

                if store.autopilot.activePRs.isEmpty {
                    emptyAutopilot
                } else {
                    ForEach(store.autopilot.activePRs.prefix(compact ? 4 : 20)) { pr in
                        HStack(spacing: 10) {
                            Text("#\(pr.number)")
                                .font(.system(.body, design: .monospaced))
                            StatusBadge(text: pr.stage)
                            Text(pr.branchName)
                                .lineLimit(1)
                            Spacer()
                            if let ci = pr.ciStatus, !ci.isEmpty {
                                Text(ci)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            if let error = pr.error, !error.isEmpty {
                                Text(error)
                                    .font(.caption)
                                    .foregroundStyle(.red)
                                    .lineLimit(1)
                            }
                        }
                    }
                }
            }
        }
    }

    private var localTaskRunner: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack {
                Label("Local Autopilot Task", systemImage: "play.rectangle")
                    .font(.headline)
                Spacer()
                if store.isAutopilotTaskRunning {
                    StatusBadge(text: "running")
                }
                Button {
                    Task { await store.runPilotArguments("autopilot status --json", title: "autopilot-status") }
                } label: {
                    Label("Status JSON", systemImage: "curlybraces")
                }
            }
            Text("Runs an autopilot preflight and a local Pilot task in this worktree; output is attached to Logs and the workspace timeline.")
                .font(.caption)
                .foregroundStyle(.secondary)
            TextField("Local autopilot task", text: $localTaskPrompt, axis: .vertical)
                .textFieldStyle(.roundedBorder)
                .lineLimit(2...5)
            HStack {
                Button {
                    Task { await store.runAutopilotLocalTask(description: localTaskPrompt) }
                } label: {
                    Label(store.isAutopilotTaskRunning ? "Running..." : "Run local task", systemImage: "hammer")
                }
                .disabled(store.isAutopilotTaskRunning)
                Button {
                    store.selectedSection = .workspaceConsole
                } label: {
                    Label("Open workspace", systemImage: "square.stack.3d.up")
                }
                Spacer()
                StatusBadge(text: store.isAutopilotTaskRunning ? "local task active" : "no PR side effect")
            }
            if let latestTaskRun {
                Text(latestTaskRun.exitCode == 0 ? "Last local task passed" : "Last local task needs review")
                    .font(.caption)
                    .foregroundStyle(latestTaskRun.exitCode == 0 ? Color.secondary : Color.red)
                    .lineLimit(1)
            }
        }
        .padding(12)
        .background(Color.secondary.opacity(0.06))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private var latestTaskRun: CommandRun? {
        store.workspaceRuns.first { run in
            run.commandLine.contains(" task ")
                && run.commandLine.contains("--local")
                && run.commandLine.contains("--skip-self-review")
                && run.exitCode != nil
        }
    }

    private var emptyAutopilot: some View {
        VStack(alignment: .leading, spacing: 14) {
            if compact {
                CompactEmptyState(title: "No tracked PRs", systemImage: "arrow.triangle.2.circlepath")
                    .frame(minHeight: 90)
            } else {
                Divider()
                HStack(spacing: 18) {
                    signal("Tracked PRs", value: "0", status: "idle")
                    signal("CI failures", value: "\(store.autopilot.failureCount)", status: store.autopilot.failureCount == 0 ? "clear" : "warning")
                    signal("Auto release", value: store.autopilot.autoRelease ? "on" : "off", status: store.autopilot.autoRelease ? "enabled" : "disabled")
                    signal("Next action", value: "none", status: "idle")
                }
                Divider()
                VStack(spacing: 8) {
                    lifecycleRow("Watch", value: "No Pilot-created PR is being tracked", status: "idle")
                    lifecycleRow("Repair", value: "No failing CI loop", status: "clear")
                    lifecycleRow("Merge", value: "No merge decision pending", status: "empty")
                }
                Divider()
                HStack {
                    Button {
                        Task { await store.runPilotArguments("autopilot status", title: "autopilot") }
                    } label: {
                        Label("Refresh lifecycle", systemImage: "arrow.clockwise")
                    }
                    Button {
                        store.selectedSection = .settings
                    } label: {
                        Label("Configure", systemImage: "slider.horizontal.3")
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

    private func lifecycleRow(_ title: String, value: String, status: String) -> some View {
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
}
