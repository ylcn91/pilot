import Foundation

enum CommandInputStyle: String {
    case arguments
    case prompt
    case none
}

struct PilotCommandDefinition: Identifiable, Equatable, Hashable {
    var id: String { name }
    var name: String
    var summary: String
    var inputStyle: CommandInputStyle = .arguments
    var defaultArguments: String = ""
    var destructive: Bool = false
}

enum PilotCommandCatalog {
    static let all: [PilotCommandDefinition] = [
        .init(name: "allow", summary: "Manage Telegram allowed users"),
        .init(name: "architect", summary: "Scan the repo and propose refactor tickets", defaultArguments: "--dry-run --json"),
        .init(name: "autopilot", summary: "PR lifecycle automation commands", defaultArguments: "status"),
        .init(name: "backend", summary: "Manage execution backends", defaultArguments: "status"),
        .init(name: "brief", summary: "Generate and send daily briefs"),
        .init(name: "budget", summary: "View and manage cost controls"),
        .init(name: "chat", summary: "Chat with the current repository through the selected workspace backend", inputStyle: .prompt, defaultArguments: "--sandbox read-only"),
        .init(name: "completion", summary: "Generate shell completion scripts", defaultArguments: "zsh"),
        .init(name: "config", summary: "Manage Pilot configuration"),
        .init(name: "doctor", summary: "Check system health and configuration"),
        .init(name: "github", summary: "GitHub integration commands"),
        .init(name: "help", summary: "Help about any command"),
        .init(name: "init", summary: "Initialize Pilot configuration or scaffold a project"),
        .init(name: "logs", summary: "View task execution logs"),
        .init(name: "metrics", summary: "View execution metrics and analytics"),
        .init(name: "onboard", summary: "Interactive onboarding wizard"),
        .init(name: "patterns", summary: "Manage cross-project patterns"),
        .init(name: "project", summary: "Manage Pilot projects", defaultArguments: "list"),
        .init(name: "release", summary: "Create a release manually"),
        .init(name: "replay", summary: "Replay and debug execution recordings"),
        .init(name: "setup", summary: "Interactive setup wizard"),
        .init(name: "start", summary: "Start Pilot with config-driven inputs", defaultArguments: "--github"),
        .init(name: "status", summary: "Show Pilot status and running tasks"),
        .init(name: "stop", summary: "Stop the Pilot daemon", destructive: true),
        .init(name: "task", summary: "Execute a ticket task through the active backend", inputStyle: .prompt, defaultArguments: "--verbose"),
        .init(name: "team", summary: "Manage teams and permissions"),
        .init(name: "tunnel", summary: "Manage Cloudflare Tunnel for webhooks"),
        .init(name: "upgrade", summary: "Upgrade Pilot to the latest version", destructive: true),
        .init(name: "usage", summary: "View usage metering and billing data"),
        .init(name: "version", summary: "Show Pilot version", inputStyle: .none),
        .init(name: "webhooks", summary: "Manage outbound webhooks")
    ]

    static let backendSetCommands: [ExecutionBackend: String] = [
        .claudeCode: "backend set claude-code",
        .codexExec: "backend set codex-exec",
        .opencode: "backend set opencode"
    ]
}
