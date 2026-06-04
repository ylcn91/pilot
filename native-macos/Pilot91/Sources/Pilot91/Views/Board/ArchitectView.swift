import SwiftUI

private struct ArchitectCadence: Identifiable, Hashable {
    var id: String { cron }
    var title: String
    var detail: String
    var cron: String
}

struct ArchitectView: View {
    @EnvironmentObject private var store: AppStore
    @State private var schedule = "*/30 * * * *"
    @State private var timezone = TimeZone.current.identifier
    @State private var lens = "core"
    var compact = false

    var body: some View {
        Panel(title: "Architect", subtitle: "Find risky code, turn it into reviewable work, and schedule scans") {
            VStack(alignment: .leading, spacing: 12) {
                if !compact {
                    schedulePanel
                    Divider()
                }

                if store.findings.isEmpty {
                    emptyArchitect
                } else {
                    findingsList
                }
            }
        }
    }

    private var schedulePanel: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Label("Scheduled scan", systemImage: "calendar.badge.clock")
                    .font(.headline)
                Spacer()
                StatusBadge(text: store.settings.selectedBackend.label)
            }
            Text(scheduleSummary)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(2)

            Grid(alignment: .leading, horizontalSpacing: 10, verticalSpacing: 8) {
                GridRow {
                    Text("Cadence")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .frame(width: 90, alignment: .leading)
                    Picker("Cadence", selection: $schedule) {
                        ForEach(cadences) { cadence in
                            Text(cadence.title).tag(cadence.cron)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.menu)
                }
                GridRow {
                    Text("Timezone")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    TextField("Europe/Istanbul", text: $timezone)
                        .textFieldStyle(.roundedBorder)
                }
                GridRow {
                    Text("Lens")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Picker("Lens", selection: $lens) {
                        ForEach(lensOptions, id: \.id) { option in
                            Text(option.title).tag(option.id)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.menu)
                }
            }

            HStack {
                Button {
                    Task {
                        await store.scheduleArchitectRadar(
                            schedule: schedule,
                            timezone: timezone,
                            backend: store.settings.selectedBackend
                        )
                    }
                } label: {
                    Label("Create schedule", systemImage: "calendar.badge.plus")
                }
                Button {
                    Task { await store.runArchitectScan(lens: lens) }
                } label: {
                    Label("Run scan now", systemImage: "scope")
                }
                Button {
                    store.selectedSection = .workspaceConsole
                } label: {
                    Label("Open workspace", systemImage: "square.stack.3d.up")
                }
            }
        }
        .padding(12)
        .background(Color.secondary.opacity(0.06))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private var findingsList: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(store.findings.prefix(compact ? 3 : 20)) { finding in
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text(finding.title)
                            .font(.subheadline.weight(.medium))
                            .lineLimit(1)
                        Spacer()
                        StatusBadge(text: finding.risk)
                    }
                    Text(finding.whyItMatters)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(compact ? 2 : 4)
                    if !compact {
                        Text(finding.files.prefix(3).joined(separator: ", "))
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.tertiary)
                            .lineLimit(1)
                    }
                }
                Divider()
            }
        }
    }

    private var emptyArchitect: some View {
        VStack(alignment: .leading, spacing: 14) {
            if compact {
                CompactEmptyState(title: "No findings", systemImage: "scope")
                    .frame(minHeight: 90)
            } else {
                VStack(alignment: .leading, spacing: 10) {
                    ArchitectStepRow(
                        systemImage: "scope",
                        title: "Scan this workspace",
                        detail: "Architect will inspect \(URL(fileURLWithPath: store.settings.projectPath).lastPathComponent) and return concrete files, risks and suggested follow-up tasks.",
                        status: "ready"
                    )
                    ArchitectStepRow(
                        systemImage: "doc.text.magnifyingglass",
                        title: "Review findings",
                        detail: "Results appear here with affected files and why the work matters.",
                        status: "empty"
                    )
                    ArchitectStepRow(
                        systemImage: "arrow.triangle.branch",
                        title: "Send work to Pilot",
                        detail: "Open the workspace when a finding is ready and run the implementation through chat or the Run dock.",
                        status: "pending"
                    )
                }
                Divider()
                HStack {
                    Button {
                        Task { await store.runArchitectScan() }
                    } label: {
                        Label("Scan repo", systemImage: "scope")
                    }
                    Button {
                        store.selectedSection = .workspaceConsole
                    } label: {
                        Label("Open workspace", systemImage: "square.stack.3d.up")
                    }
                }
            }
        }
    }

    private var scheduleSummary: String {
        let cadence = cadences.first { $0.cron == schedule }
        let cadenceText = cadence?.detail ?? "custom schedule"
        let lensText = lensOptions.first { $0.id == lens }?.title ?? lens
        return "\(cadenceText). Lens: \(lensText). Scheduled runs use the real Architect daemon configuration."
    }

    private var cadences: [ArchitectCadence] {
        [
            .init(title: "Every 30 minutes", detail: "Runs every 30 minutes", cron: "*/30 * * * *"),
            .init(title: "Hourly", detail: "Runs once every hour", cron: "0 * * * *"),
            .init(title: "Every morning", detail: "Runs every day at 09:00", cron: "0 9 * * *"),
            .init(title: "Weekday morning", detail: "Runs Monday-Friday at 09:00", cron: "0 9 * * 1-5")
        ]
    }

    private var lensOptions: [(id: String, title: String)] {
        [
            ("core", "Core health"),
            ("radar", "Risk radar"),
            ("refactor", "Refactor candidates"),
            ("rfc", "Architecture RFCs"),
            ("depdoctor", "Dependency health"),
            ("testgap", "Test gaps")
        ]
    }
}

private struct ArchitectStepRow: View {
    var systemImage: String
    var title: String
    var detail: String
    var status: String

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: systemImage)
                .foregroundStyle(.secondary)
                .frame(width: 18)
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.subheadline.weight(.medium))
                Text(detail)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            Spacer()
            StatusBadge(text: status)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 6)
    }
}
