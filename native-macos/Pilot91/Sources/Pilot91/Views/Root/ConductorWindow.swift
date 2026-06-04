import SwiftUI

private func sidebarProjectName(from root: String) -> String {
    let url = URL(fileURLWithPath: root)
    if let codexIndex = url.pathComponents.firstIndex(of: ".Codex"), codexIndex > 1 {
        return url.pathComponents[codexIndex - 1]
    }
    return url.lastPathComponent
}

private func sidebarWorkspaceName(from root: String) -> String {
    URL(fileURLWithPath: root).lastPathComponent
}

struct ConductorWindow: View {
    @EnvironmentObject private var store: AppStore
    @State private var columnVisibility: NavigationSplitViewVisibility = .all

    var body: some View {
        NavigationSplitView(columnVisibility: $columnVisibility) {
            ConductorSidebar()
                .navigationSplitViewColumnWidth(min: 230, ideal: 250, max: 290)
        } detail: {
            ConductorContent()
                .navigationSplitViewColumnWidth(min: 900, ideal: 1180)
        }
    }
}

struct ConductorContent: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if store.selectedSection == .missionControl {
                WindowBrandView()
                    .padding(.top, 2)
            }

            Group {
                switch store.selectedSection {
                case .workspaceConsole:
                    WorkspaceConsoleView()
                case .missionControl:
                    MissionControlView()
                case .runtime:
                    RuntimeView()
                case .commandCenter:
                    CommandCenterView()
                case .queue:
                    QueueView()
                case .autopilot:
                    AutopilotView()
                case .architect:
                    ArchitectView()
                case .history:
                    HistoryView()
                case .metrics:
                    MetricsView()
                case .logs:
                    LogsView()
                case .settings:
                    SettingsView()
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
        .padding(.horizontal, store.selectedSection == .workspaceConsole ? 0 : 16)
        .padding(.top, store.selectedSection == .workspaceConsole ? 0 : 8)
        .padding(.bottom, store.selectedSection == .workspaceConsole ? 0 : 14)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

struct ConductorSidebar: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        List(selection: $store.selectedSection) {
            sidebarRow(.missionControl)
            sidebarRow(.history)

            Section {
                ProjectActionRows()
                WorkspaceProjectSidebarRow()
            } header: {
                Text("Projects")
            }

            Section {
                sidebarRow(.commandCenter)
                sidebarRow(.queue)
                sidebarRow(.autopilot)
                sidebarRow(.architect)
                sidebarRow(.logs)
                sidebarRow(.runtime)
                sidebarRow(.metrics)
            } header: {
                Text("Pilot Operations")
            }

            Section {
                sidebarRow(.settings)
            }
        }
        .listStyle(.sidebar)
    }

    private func sidebarRow(_ section: SidebarSection) -> some View {
        HStack(spacing: 8) {
            Label(section.rawValue, systemImage: section.systemImage)
            Spacer()
        }
        .tag(section)
    }
}

private struct ProjectActionRows: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        Button {
            Task { await store.openProjectFolder() }
        } label: {
            Label("Open project...", systemImage: "folder.badge.plus")
        }
        .buttonStyle(.plain)

        Button {
            Task { await store.createWorkspaceCopy() }
        } label: {
            Label(store.isCreatingWorkspace ? "Creating workspace..." : "New workspace", systemImage: "plus.square")
        }
        .buttonStyle(.plain)
        .disabled(store.isCreatingWorkspace)
    }
}

private struct WorkspaceProjectSidebarRow: View {
    @EnvironmentObject private var store: AppStore
    @State private var showAllWorkspaces = false

    var body: some View {
        Group {
            projectRow
            ForEach(workspaces) { workspace in
                workspaceRow(workspace)
            }
        }
        .task {
            await store.refreshProjectWorkspaces()
        }
    }

    private var projectRow: some View {
        HStack(spacing: 8) {
            Image(systemName: "folder")
                .foregroundStyle(.secondary)
            Text(projectName)
                .lineLimit(1)
            Spacer()
            Button {
                showAllWorkspaces.toggle()
            } label: {
                Image(systemName: showAllWorkspaces ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
            }
            .buttonStyle(.borderless)
            .help(showAllWorkspaces ? "Hide generated workspaces" : "Show all workspaces")
        }
        .font(.subheadline)
    }

    private func workspaceRow(_ workspace: WorkspaceListItem) -> some View {
        Button {
            if workspace.isCurrent {
                store.selectedSection = .workspaceConsole
            } else {
                Task { await store.switchWorkspace(workspace) }
            }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: "point.3.connected.trianglepath.dotted")
                    .foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text(workspace.name)
                        .lineLimit(1)
                    Text(workspace.branch.isEmpty ? "detached" : workspace.branch)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
            }
            .padding(.leading, 18)
            .padding(.vertical, 4)
            .background(workspace.isCurrent ? Color.secondary.opacity(0.12) : Color.clear)
            .clipShape(RoundedRectangle(cornerRadius: 6))
        }
        .buttonStyle(.plain)
        .disabled(workspace.isCurrent && store.selectedSection == .workspaceConsole)
        .accessibilityLabel(workspace.name)
        .accessibilityValue(workspace.branch.isEmpty ? "detached" : workspace.branch)
    }

    private var projectName: String {
        sidebarProjectName(from: effectivePath)
    }

    private var workspaces: [WorkspaceListItem] {
        let items: [WorkspaceListItem]
        if store.projectWorkspaces.isEmpty {
            items = [currentWorkspaceItem]
        } else {
            items = store.projectWorkspaces
        }

        guard !showAllWorkspaces else { return items }

        let filtered = items.filter { workspace in
            workspace.isCurrent || isHumanWorkspace(workspace)
        }
        if filtered.contains(where: \.isCurrent) {
            return filtered
        }
        return [currentWorkspaceItem] + filtered
    }

    private var effectivePath: String {
        store.workspace.repoRoot.isEmpty ? store.settings.projectPath : store.workspace.repoRoot
    }

    private var currentWorkspaceItem: WorkspaceListItem {
        WorkspaceListItem(
            path: effectivePath,
            name: sidebarWorkspaceName(from: effectivePath),
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
}

struct WindowBrandView: View {
    var body: some View {
        HStack(spacing: 6) {
            Text("Pilot")
                .font(.system(size: 20, weight: .semibold, design: .rounded))
                .foregroundStyle(Color.blue)
            Text("91")
                .font(.system(size: 12, weight: .bold, design: .rounded))
                .foregroundStyle(Color.blue)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.blue.opacity(0.10))
                .clipShape(RoundedRectangle(cornerRadius: 5))
        }
        .fixedSize()
        .accessibilityLabel("Pilot 91")
    }
}

struct ConnectionStatusView: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(store.serverRunning ? Color.green : Color.red)
                .frame(width: 8, height: 8)
            Text(store.serverRunning ? "gateway online" : "gateway offline")
                .font(.caption.weight(.medium))
        }
        .padding(.horizontal, 9)
        .frame(height: 28)
        .background(.regularMaterial)
        .clipShape(Capsule())
    }
}

struct BackendPicker: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        Picker("Backend", selection: Binding(
            get: { store.settings.selectedBackend },
            set: { backend in
                Task { await store.switchBackend(backend) }
            }
        )) {
            ForEach(ExecutionBackend.allCases) { backend in
                Text(backend.label).tag(backend)
            }
        }
        .pickerStyle(.menu)
        .controlSize(.small)
        .frame(width: 150)
    }
}
