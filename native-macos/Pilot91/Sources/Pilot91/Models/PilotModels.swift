import Foundation

enum SidebarSection: String, CaseIterable, Identifiable {
    case workspaceConsole = "Workspace"
    case missionControl = "Dashboard"
    case runtime = "Agent Runtime"
    case commandCenter = "Commands"
    case queue = "Task Board"
    case autopilot = "Autopilot"
    case architect = "Architect"
    case history = "History"
    case metrics = "Metrics"
    case logs = "Logs"
    case settings = "Settings"

    var id: String { rawValue }

    var systemImage: String {
        switch self {
        case .workspaceConsole: "square.stack.3d.up"
        case .missionControl: "rectangle.3.group"
        case .runtime: "sparkles"
        case .commandCenter: "terminal"
        case .queue: "tray.full"
        case .autopilot: "arrow.triangle.2.circlepath"
        case .architect: "scope"
        case .history: "clock.arrow.circlepath"
        case .metrics: "chart.xyaxis.line"
        case .logs: "doc.text"
        case .settings: "gearshape"
        }
    }
}

enum RuntimeStatus: String {
    case disconnected
    case connected
    case running
    case completed
    case error
}

enum RuntimeSandbox: String, CaseIterable, Identifiable {
    case readOnly = "read-only"
    case workspaceWrite = "workspace-write"
    case dangerFullAccess = "danger-full-access"

    var id: String { rawValue }
}

enum ExecutionBackend: String, CaseIterable, Identifiable {
    case claudeCode = "claude-code"
    case codexExec = "codex-exec"
    case codexAppServer = "codex-app-server"
    case opencode = "opencode"

    var id: String { rawValue }

    var label: String {
        switch self {
        case .claudeCode: "Claude Code"
        case .codexExec: "Codex"
        case .codexAppServer: "Codex App Server"
        case .opencode: "OpenCode"
        }
    }
}

enum AgentChatProvider: String, CaseIterable, Identifiable, Codable {
    case codex
    case claudeCode

    var id: String { rawValue }

    var label: String {
        switch self {
        case .codex: "Codex"
        case .claudeCode: "Claude Code"
        }
    }

    var systemImage: String {
        switch self {
        case .codex: "sparkles"
        case .claudeCode: "terminal"
        }
    }

    var defaultModel: String {
        switch self {
        case .codex: "gpt-5.5"
        case .claudeCode: "opus"
        }
    }

    var modelOptions: [String] {
        switch self {
        case .codex:
            return ["gpt-5.5", "gpt-5.4", "gpt-5", "o3"]
        case .claudeCode:
            return ["opus", "sonnet", "haiku", "claude-opus-4-8", "claude-sonnet-4-6"]
        }
    }
}

struct AgentAccount: Identifiable, Equatable, Hashable, Codable {
    var id: String
    var name: String
    var provider: AgentChatProvider
    var plan: String
    var scope: String
    var organization: String
    var authMethod: ProviderAuthMethod
    var executablePath: String
    var configPath: String
    var profileName: String
    var environment: String
    var loginCommand: String
    var defaultModel: String

    var modelOptions: [String] { provider.modelOptions }

    init(
        id: String,
        name: String,
        provider: AgentChatProvider,
        plan: String,
        scope: String,
        organization: String,
        authMethod: ProviderAuthMethod = .cli,
        executablePath: String = "",
        configPath: String = "",
        profileName: String = "",
        environment: String = "",
        loginCommand: String = "",
        defaultModel: String = ""
    ) {
        self.id = id
        self.name = name
        self.provider = provider
        self.plan = plan
        self.scope = scope
        self.organization = organization
        self.authMethod = authMethod
        self.executablePath = executablePath
        self.configPath = configPath.isEmpty ? Self.defaultConfigPath(for: provider) : configPath
        self.profileName = profileName
        self.environment = environment
        self.loginCommand = loginCommand.isEmpty ? Self.defaultLoginCommand(for: provider) : loginCommand
        self.defaultModel = defaultModel.isEmpty ? provider.defaultModel : defaultModel
    }

    private enum CodingKeys: String, CodingKey {
        case id
        case name
        case provider
        case plan
        case scope
        case organization
        case authMethod
        case executablePath
        case configPath
        case profileName
        case environment
        case loginCommand
        case defaultModel
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let provider = try container.decodeIfPresent(AgentChatProvider.self, forKey: .provider) ?? .claudeCode
        self.init(
            id: try container.decodeIfPresent(String.self, forKey: .id) ?? "account-\(UUID().uuidString)",
            name: try container.decodeIfPresent(String.self, forKey: .name) ?? "Account",
            provider: provider,
            plan: try container.decodeIfPresent(String.self, forKey: .plan) ?? "",
            scope: try container.decodeIfPresent(String.self, forKey: .scope) ?? "",
            organization: try container.decodeIfPresent(String.self, forKey: .organization) ?? "",
            authMethod: try container.decodeIfPresent(ProviderAuthMethod.self, forKey: .authMethod) ?? .cli,
            executablePath: try container.decodeIfPresent(String.self, forKey: .executablePath) ?? "",
            configPath: try container.decodeIfPresent(String.self, forKey: .configPath) ?? "",
            profileName: try container.decodeIfPresent(String.self, forKey: .profileName) ?? "",
            environment: try container.decodeIfPresent(String.self, forKey: .environment) ?? "",
            loginCommand: try container.decodeIfPresent(String.self, forKey: .loginCommand) ?? "",
            defaultModel: try container.decodeIfPresent(String.self, forKey: .defaultModel) ?? ""
        )
    }

    static func defaultConfigPath(for provider: AgentChatProvider) -> String {
        switch provider {
        case .codex: "~/.codex/config.toml"
        case .claudeCode: "~/.claude/settings.json"
        }
    }

    static func defaultLoginCommand(for provider: AgentChatProvider) -> String {
        switch provider {
        case .codex: "codex login"
        case .claudeCode: "claude /login"
        }
    }
}

enum AgentAccountCatalog {
    static let defaults: [AgentAccount] = [
        AgentAccount(
            id: "claude",
            name: "claude",
            provider: .claudeCode,
            plan: "Claude Max",
            scope: "work",
            organization: "Emlakjet",
            configPath: "~/.claude/settings.json"
        ),
        AgentAccount(
            id: "claude-admin",
            name: "claude-admin",
            provider: .claudeCode,
            plan: "Claude Max",
            scope: "shared admin",
            organization: "Emlakjet",
            configPath: "~/.claude-admin/settings.json",
            environment: "CLAUDE_CONFIG_DIR=~/.claude-admin"
        ),
        AgentAccount(
            id: "claude-doksanbir",
            name: "claude-doksanbir",
            provider: .claudeCode,
            plan: "Claude Max",
            scope: "personal",
            organization: "doksanbir",
            configPath: "~/.claude-doksanbir/settings.json",
            environment: "CLAUDE_CONFIG_DIR=~/.claude-doksanbir"
        ),
        AgentAccount(
            id: "codex",
            name: "Codex",
            provider: .codex,
            plan: "Pro 20x",
            scope: "work",
            organization: "Emlakjet"
        )
    ]

    static func account(id: String, in accounts: [AgentAccount] = defaults) -> AgentAccount {
        accounts.first { $0.id == id } ?? accounts.first ?? defaults[0]
    }
}

enum ProviderAuthMethod: String, CaseIterable, Identifiable, Codable {
    case cli = "CLI"
    case apiKey = "API key"

    var id: String { rawValue }
}

struct ProviderDetailRow: Identifiable, Equatable {
    var id: String { label }
    var label: String
    var value: String
}

struct ProviderStatus: Identifiable, Equatable {
    var id: AgentChatProvider { provider }
    var provider: AgentChatProvider
    var connected: Bool
    var headline: String
    var authMethod: ProviderAuthMethod
    var executablePath: String
    var configPath: String
    var loginCommand: String
    var detailRows: [ProviderDetailRow]

    static func placeholder(for provider: AgentChatProvider) -> ProviderStatus {
        switch provider {
        case .claudeCode:
            return ProviderStatus(
                provider: provider,
                connected: false,
                headline: "Not checked",
                authMethod: .cli,
                executablePath: "",
                configPath: "~/.claude/settings.json",
                loginCommand: "claude /login",
                detailRows: [
                    ProviderDetailRow(label: "Provider", value: "Claude Code"),
                    ProviderDetailRow(label: "Auth", value: "CLI")
                ]
            )
        case .codex:
            return ProviderStatus(
                provider: provider,
                connected: false,
                headline: "Not checked",
                authMethod: .cli,
                executablePath: "",
                configPath: "~/.codex/config.toml",
                loginCommand: "codex login",
                detailRows: [
                    ProviderDetailRow(label: "Provider", value: "Codex"),
                    ProviderDetailRow(label: "Auth", value: "CLI")
                ]
            )
        }
    }
}

enum AgentChatRole: String, Codable {
    case user
    case assistant
    case system
}

struct AgentChatMessage: Identifiable, Equatable {
    var id = UUID()
    var role: AgentChatRole
    var provider: AgentChatProvider?
    var model: String?
    var body: String
    var createdAt = Date()
    var isRunning = false
    var exitCode: Int32?
}

struct IntegrationDefinition: Identifiable, Equatable {
    var id: String { name }
    var name: String
    var systemImage: String
    var summary: String
    var envVars: [String]
    var configKeys: [String]
    var command: String
}

enum IntegrationCatalog {
    static let all: [IntegrationDefinition] = [
        .init(
            name: "GitHub",
            systemImage: "chevron.left.forwardslash.chevron.right",
            summary: "Issue polling, PR creation, review approval and webhook intake.",
            envVars: ["GITHUB_TOKEN"],
            configKeys: ["adapters.github.repo", "adapters.github.polling.enabled", "adapters.github.pilot_label"],
            command: "github --help"
        ),
        .init(
            name: "Atlassian Jira",
            systemImage: "square.stack.3d.up",
            summary: "Jira issue polling, transition wiring and webhook intake.",
            envVars: ["JIRA_API_TOKEN", "JIRA_EMAIL", "JIRA_BASE_URL"],
            configKeys: ["adapters.jira.enabled", "adapters.jira.project_key", "adapters.jira.polling.enabled"],
            command: "start --help"
        ),
        .init(
            name: "Linear",
            systemImage: "line.3.horizontal.decrease.circle",
            summary: "Linear webhook and polling adapter configuration.",
            envVars: ["LINEAR_API_KEY"],
            configKeys: ["adapters.linear.enabled", "adapters.linear.polling"],
            command: "start --linear --help"
        ),
        .init(
            name: "GitLab",
            systemImage: "shippingbox",
            summary: "GitLab webhook intake and issue source wiring.",
            envVars: ["GITLAB_TOKEN"],
            configKeys: ["adapters.gitlab.enabled", "adapters.gitlab.webhook_secret"],
            command: "start --help"
        ),
        .init(
            name: "Bitbucket",
            systemImage: "tray.full",
            summary: "Bitbucket Cloud issue polling, PR creation and webhook intake.",
            envVars: ["BITBUCKET_TOKEN"],
            configKeys: ["adapters.bitbucket.workspace", "adapters.bitbucket.repo", "adapters.bitbucket.polling.enabled"],
            command: "start --help"
        ),
        .init(
            name: "Azure DevOps",
            systemImage: "cloud",
            summary: "Azure DevOps work-item webhook intake.",
            envVars: ["AZURE_DEVOPS_PAT"],
            configKeys: ["adapters.azure_devops.enabled", "adapters.azure_devops.project", "adapters.azure_devops.polling.enabled"],
            command: "start --help"
        ),
        .init(
            name: "Slack",
            systemImage: "bubble.left.and.bubble.right",
            summary: "Socket Mode, notifications and approval workflows.",
            envVars: ["SLACK_BOT_TOKEN", "SLACK_APP_TOKEN"],
            configKeys: ["adapters.slack.enabled", "adapters.slack.socket_mode"],
            command: "start --slack --help"
        ),
        .init(
            name: "Telegram",
            systemImage: "paperplane",
            summary: "Telegram bot polling, allowed users and approvals.",
            envVars: ["TELEGRAM_BOT_TOKEN"],
            configKeys: ["adapters.telegram.enabled", "adapters.telegram.allowed_users"],
            command: "allow --help"
        ),
        .init(
            name: "Discord",
            systemImage: "gamecontroller",
            summary: "Discord bot adapter and notification route.",
            envVars: ["DISCORD_BOT_TOKEN"],
            configKeys: ["adapters.discord.enabled"],
            command: "start --discord --help"
        ),
        .init(
            name: "Plane",
            systemImage: "airplane",
            summary: "Plane.so polling and webhook intake.",
            envVars: ["PLANE_API_KEY"],
            configKeys: ["adapters.plane.enabled", "adapters.plane.polling"],
            command: "start --plane --help"
        ),
        .init(
            name: "Asana",
            systemImage: "circle.grid.3x3",
            summary: "Asana webhook intake and task-source configuration.",
            envVars: ["ASANA_ACCESS_TOKEN"],
            configKeys: ["adapters.asana.enabled", "adapters.asana.webhook_secret"],
            command: "start --help"
        )
    ]
}

struct DashboardMetrics: Codable, Equatable {
    var totalTokens: Int64 = 0
    var inputTokens: Int64 = 0
    var outputTokens: Int64 = 0
    var totalCostUSD: Double = 0
    var totalTasks: Int = 0
    var succeededTasks: Int = 0
    var failedTasks: Int = 0
    var tokenSparkline: [Int64] = []
    var costSparkline: [Double] = []
    var queueSparkline: [Int] = []
}

struct QueueTask: Codable, Identifiable, Equatable {
    var id: String
    var issueID: String
    var title: String
    var status: String
    var progress: Double
    var prURL: String?
    var issueURL: String?
    var projectPath: String
    var createdAt: String
}

struct HistoryEntry: Codable, Identifiable, Equatable {
    var id: String
    var issueID: String
    var title: String
    var status: String
    var prURL: String?
    var projectPath: String
    var completedAt: String
    var durationMs: Int64
}

struct LogEntry: Codable, Identifiable, Equatable {
    var id: String { "\(ts)-\(component ?? "")-\(message)" }
    var ts: String
    var level: String
    var message: String
    var component: String?
}

struct ServerStatus: Codable, Equatable {
    var version: String?
    var running: Bool
    var sessions: Int?
}

struct TaskInfo: Codable, Identifiable, Equatable {
    var id: String
    var title: String
    var status: String
    var projectPath: String?
    var priority: Int?
}

struct TasksResponse: Codable, Equatable {
    var tasks: [TaskInfo]
}

/// Mirrors the gateway /live payload: overall liveness plus the individual
/// probe checks (goroutine count, recent panics, main-loop heartbeat). All
/// nested fields are optional so partial payloads still decode.
struct DaemonLiveness: Codable, Equatable {
    var alive: Bool
    var checks: Checks?

    struct Checks: Codable, Equatable {
        var goroutines: GoroutineCheck?
        var panics: PanicCheck?
        var heartbeat: HeartbeatCheck?
    }

    struct GoroutineCheck: Codable, Equatable {
        var count: Int?
        var max: Int?
        var ok: Bool?
    }

    struct PanicCheck: Codable, Equatable {
        var count: Int?
        var recent: Bool?
        var windowSeconds: Int?
        var ok: Bool?

        enum CodingKeys: String, CodingKey {
            case count, recent, ok
            case windowSeconds = "window_seconds"
        }
    }

    struct HeartbeatCheck: Codable, Equatable {
        var lastSecondsAgo: Int?
        var ok: Bool?

        enum CodingKeys: String, CodingKey {
            case ok
            case lastSecondsAgo = "last_seconds_ago"
        }
    }
}

struct ActivePR: Codable, Identifiable, Equatable {
    var id: Int { number }
    var number: Int
    var url: String
    var stage: String
    var ciStatus: String?
    var error: String?
    var branchName: String
}

struct AutopilotStatus: Codable, Equatable {
    var enabled: Bool = false
    var environment: String = ""
    var autoRelease: Bool = false
    var activePRs: [ActivePR] = []
    var failureCount: Int = 0
}

struct Finding: Codable, Identifiable, Equatable {
    var id: String { "\(kind)-\(title)" }
    var title: String
    var kind: String
    var risk: String
    var whyItMatters: String
    var suggestedPRPieces: [String]
    var testPlan: String
    var files: [String]

    enum CodingKeys: String, CodingKey {
        case title
        case kind
        case risk
        case whyItMatters = "why_it_matters"
        case suggestedPRPieces = "suggested_pr_pieces"
        case testPlan = "test_plan"
        case files
    }
}

struct ArchitectResponse: Codable {
    var findings: [Finding]
    var count: Int
}

struct GitGraphLine: Codable, Identifiable, Equatable {
    var id: String { "\(sha ?? "")-\(message ?? "")-\(graphChars)" }
    var graphChars: String
    var refs: String?
    var message: String?
    var author: String?
    var sha: String?

    enum CodingKeys: String, CodingKey {
        case graphChars = "graph_chars"
        case refs
        case message
        case author
        case sha
    }
}

struct GitGraphData: Codable, Equatable {
    var lines: [GitGraphLine] = []
    var totalCount: Int = 0
    var error: String?
    var lastRefresh: String?

    enum CodingKeys: String, CodingKey {
        case lines
        case totalCount = "total_count"
        case error
        case lastRefresh = "last_refresh"
    }
}

struct WorkspaceSnapshot: Equatable {
    var repoRoot: String = ""
    var branch: String = ""
    var baseBranch: String = "dev"
    var diffStat: String = ""
    var statusSummary: String = "clean"
    var changedFiles: [WorkspaceFileChange] = []
    var pullRequest: WorkspacePullRequest?
    var checks: [WorkspaceCheck] = []
    var lastRefreshedAt: Date?

    var hasChanges: Bool { !changedFiles.isEmpty }
}

struct WorkspaceListItem: Identifiable, Equatable {
    var id: String { path }
    var path: String
    var name: String
    var branch: String
    var isCurrent: Bool
}

struct WorkspaceFileChange: Identifiable, Equatable {
    var id: String { path }
    var path: String
    var status: String
    var additions: Int
    var deletions: Int
}

struct WorkspacePullRequest: Equatable {
    var number: Int
    var title: String
    var url: String
    var state: String
    var reviewDecision: String?
}

struct WorkspaceCheck: Identifiable, Equatable {
    var id: String { name }
    var name: String
    var status: String
    var conclusion: String?
}

struct CommandRun: Identifiable, Equatable {
    var id = UUID()
    var title: String
    var commandLine: String
    var output: String
    var stderr: String = ""
    var exitCode: Int32?
    var startedAt: Date
    var finishedAt: Date?

    var isRunning: Bool { exitCode == nil && finishedAt == nil }
}

struct RuntimeMessage: Identifiable, Equatable {
    let id = UUID()
    var role: String
    var text: String
}

struct RuntimeApprovalRequest: Identifiable, Equatable {
    var id: Int { requestId }
    var requestId: Int
    var method: String
    var params: String
    var choices: [String]
}
