import Foundation
import SwiftUI

enum AppAppearance: String, CaseIterable, Identifiable {
    case dark
    case system
    case light

    var id: String { rawValue }

    var label: String {
        switch self {
        case .dark: "Dark"
        case .system: "System"
        case .light: "Light"
        }
    }

    var colorScheme: ColorScheme? {
        switch self {
        case .dark: .dark
        case .system: nil
        case .light: .light
        }
    }
}

enum SendMessageShortcut: String, CaseIterable, Identifiable {
    case enter
    case commandReturn

    var id: String { rawValue }

    var label: String {
        switch self {
        case .enter: "Enter"
        case .commandReturn: "Command-Enter"
        }
    }
}

enum FollowUpBehavior: String, CaseIterable, Identifiable {
    case steer
    case queue

    var id: String { rawValue }

    var label: String {
        switch self {
        case .steer: "Steer"
        case .queue: "Queue"
        }
    }
}

enum CompletionSound: String, CaseIterable, Identifiable {
    case none
    case chime
    case glass

    var id: String { rawValue }

    var label: String {
        switch self {
        case .none: "None"
        case .chime: "Chime"
        case .glass: "Glass"
        }
    }
}

enum InterfaceFont: String, CaseIterable, Identifiable {
    case system
    case rounded
    case monospaced

    var id: String { rawValue }

    var label: String {
        switch self {
        case .system: "System"
        case .rounded: "Rounded"
        case .monospaced: "Monospaced"
        }
    }

    var baseFont: Font {
        switch self {
        case .system: .body
        case .rounded: .system(.body, design: .rounded)
        case .monospaced: .system(.body, design: .monospaced)
        }
    }
}

enum AccessibleColorMode: String, CaseIterable, Identifiable {
    case standard
    case redGreenSafe
    case blueYellowSafe

    var id: String { rawValue }

    var label: String {
        switch self {
        case .standard: "Default"
        case .redGreenSafe: "Deuteranopia / Protanopia"
        case .blueYellowSafe: "Tritanopia"
        }
    }
}

enum CodeTheme: String, CaseIterable, Identifiable {
    case standard
    case catppuccinLatte
    case catppuccinMacchiato
    case catppuccinMocha
    case dracula
    case nord
    case tokyoNight
    case gruvboxDark
    case solarizedDark

    var id: String { rawValue }

    var label: String {
        switch self {
        case .standard: "Default"
        case .catppuccinLatte: "Catppuccin Latte"
        case .catppuccinMacchiato: "Catppuccin Macchiato"
        case .catppuccinMocha: "Catppuccin Mocha"
        case .dracula: "Dracula"
        case .nord: "Nord"
        case .tokyoNight: "Tokyo Night"
        case .gruvboxDark: "Gruvbox Dark"
        case .solarizedDark: "Solarized Dark"
        }
    }

    var background: Color {
        switch self {
        case .catppuccinLatte: Color(red: 0.94, green: 0.93, blue: 0.88)
        case .catppuccinMacchiato: Color(red: 0.15, green: 0.15, blue: 0.22)
        case .catppuccinMocha: Color(red: 0.12, green: 0.11, blue: 0.16)
        case .dracula: Color(red: 0.16, green: 0.16, blue: 0.21)
        case .nord: Color(red: 0.18, green: 0.20, blue: 0.25)
        case .tokyoNight: Color(red: 0.09, green: 0.10, blue: 0.16)
        case .gruvboxDark: Color(red: 0.16, green: 0.13, blue: 0.10)
        case .solarizedDark: Color(red: 0.00, green: 0.17, blue: 0.21)
        case .standard: Color.secondary.opacity(0.10)
        }
    }

    var comment: Color {
        switch self {
        case .catppuccinLatte: Color(red: 0.42, green: 0.44, blue: 0.54)
        case .nord: Color(red: 0.38, green: 0.44, blue: 0.53)
        case .solarizedDark: Color(red: 0.51, green: 0.58, blue: 0.59)
        default: .secondary
        }
    }

    var keyword: Color {
        switch self {
        case .dracula: Color(red: 1.00, green: 0.47, blue: 0.78)
        case .nord: Color(red: 0.50, green: 0.69, blue: 0.80)
        case .tokyoNight: Color(red: 0.73, green: 0.44, blue: 1.00)
        case .gruvboxDark: Color(red: 0.98, green: 0.29, blue: 0.20)
        case .solarizedDark: Color(red: 0.52, green: 0.60, blue: 0.00)
        default: Color(red: 1.00, green: 0.38, blue: 0.40)
        }
    }

    var function: Color {
        switch self {
        case .dracula: Color(red: 0.74, green: 0.58, blue: 0.98)
        case .nord: Color(red: 0.53, green: 0.75, blue: 0.82)
        case .tokyoNight: Color(red: 0.48, green: 0.68, blue: 1.00)
        case .gruvboxDark: Color(red: 0.98, green: 0.74, blue: 0.18)
        case .solarizedDark: Color(red: 0.15, green: 0.55, blue: 0.82)
        default: Color(red: 0.84, green: 0.25, blue: 0.92)
        }
    }

    var string: Color {
        switch self {
        case .dracula: Color(red: 0.95, green: 0.98, blue: 0.55)
        case .nord: Color(red: 0.64, green: 0.75, blue: 0.55)
        case .tokyoNight: Color(red: 0.62, green: 0.84, blue: 0.52)
        case .gruvboxDark: Color(red: 0.72, green: 0.73, blue: 0.15)
        case .solarizedDark: Color(red: 0.52, green: 0.60, blue: 0.00)
        default: Color(red: 0.22, green: 0.79, blue: 0.46)
        }
    }
}

enum MonoFontChoice: String, CaseIterable, Identifiable {
    case geistMono
    case sfMono
    case jetBrainsMono
    case firaCode
    case menlo

    var id: String { rawValue }

    var label: String {
        switch self {
        case .geistMono: "Geist Mono"
        case .sfMono: "SF Mono"
        case .jetBrainsMono: "JetBrains Mono"
        case .firaCode: "Fira Code"
        case .menlo: "Menlo"
        }
    }

    var fontName: String {
        switch self {
        case .geistMono: "Geist Mono"
        case .sfMono: "SF Mono"
        case .jetBrainsMono: "JetBrains Mono"
        case .firaCode: "Fira Code"
        case .menlo: "Menlo"
        }
    }

    func font(size: CGFloat) -> Font {
        .custom(fontName, size: size)
    }
}

enum MarkdownStyle: String, CaseIterable, Identifiable {
    case standard
    case compact
    case document

    var id: String { rawValue }

    var label: String {
        switch self {
        case .standard: "Default"
        case .compact: "Compact"
        case .document: "Document"
        }
    }
}

enum ModelEffort: String, CaseIterable, Identifiable {
    case auto
    case low
    case medium
    case high
    case xhigh
    case max
    case ultracode

    var id: String { rawValue }

    var label: String {
        switch self {
        case .auto: "auto"
        case .low: "low"
        case .medium: "medium"
        case .high: "high"
        case .xhigh: "xhigh"
        case .max: "max"
        case .ultracode: "ultracode"
        }
    }
}

enum ThinkingEffort: String, CaseIterable, Identifiable {
    case none
    case low
    case medium
    case high
    case xhigh

    var id: String { rawValue }

    var label: String {
        switch self {
        case .none: "none"
        case .low: "low"
        case .medium: "medium"
        case .high: "high"
        case .xhigh: "xhigh"
        }
    }
}

enum CodexPersonality: String, CaseIterable, Identifiable {
    case pragmatic
    case concise
    case meticulous

    var id: String { rawValue }

    var label: String {
        switch self {
        case .pragmatic: "Pragmatic (default)"
        case .concise: "Concise"
        case .meticulous: "Meticulous"
        }
    }
}

enum GitBranchPrefixMode: String, CaseIterable, Identifiable {
    case githubUsername
    case custom
    case none

    var id: String { rawValue }

    var label: String {
        switch self {
        case .githubUsername: "GitHub username (ylcn91)"
        case .custom: "Custom"
        case .none: "None"
        }
    }
}

@MainActor
final class AppSettings: ObservableObject {
    @Published var gatewayURL: String {
        didSet { UserDefaults.standard.set(gatewayURL, forKey: Keys.gatewayURL) }
    }
    @Published var authToken: String {
        didSet { UserDefaults.standard.set(authToken, forKey: Keys.authToken) }
    }
    @Published var pilotCommand: String {
        didSet { UserDefaults.standard.set(pilotCommand, forKey: Keys.pilotCommand) }
    }
    @Published var projectPath: String {
        didSet { UserDefaults.standard.set(projectPath, forKey: Keys.projectPath) }
    }
    @Published var selectedBackend: ExecutionBackend {
        didSet { UserDefaults.standard.set(selectedBackend.rawValue, forKey: Keys.selectedBackend) }
    }
    @Published var selectedModel: String {
        didSet { UserDefaults.standard.set(selectedModel, forKey: Keys.selectedModel) }
    }
    @Published var sandbox: RuntimeSandbox {
        didSet { UserDefaults.standard.set(sandbox.rawValue, forKey: Keys.sandbox) }
    }
    @Published var chatProvider: AgentChatProvider {
        didSet { UserDefaults.standard.set(chatProvider.rawValue, forKey: Keys.chatProvider) }
    }
    @Published var agentAccounts: [AgentAccount] {
        didSet {
            Self.saveAccounts(agentAccounts)
            if !agentAccounts.contains(where: { $0.id == selectedAgentAccountID }) {
                selectedAgentAccountID = agentAccounts.first?.id ?? AgentAccountCatalog.defaults[0].id
            }
        }
    }
    @Published var selectedAgentAccountID: String {
        didSet {
            UserDefaults.standard.set(selectedAgentAccountID, forKey: Keys.selectedAgentAccountID)
            chatProvider = selectedAgentAccount.provider
            if !selectedAgentAccount.defaultModel.isEmpty {
                selectedChatModel = selectedAgentAccount.defaultModel
            }
        }
    }
    @Published var codexModel: String {
        didSet { UserDefaults.standard.set(codexModel, forKey: Keys.codexModel) }
    }
    @Published var claudeModel: String {
        didSet { UserDefaults.standard.set(claudeModel, forKey: Keys.claudeModel) }
    }
    @Published var appearance: AppAppearance {
        didSet { UserDefaults.standard.set(appearance.rawValue, forKey: Keys.appearance) }
    }
    @Published var sendMessageShortcut: SendMessageShortcut {
        didSet { UserDefaults.standard.set(sendMessageShortcut.rawValue, forKey: Keys.sendMessageShortcut) }
    }
    @Published var followUpBehavior: FollowUpBehavior {
        didSet { UserDefaults.standard.set(followUpBehavior.rawValue, forKey: Keys.followUpBehavior) }
    }
    @Published var desktopNotifications: Bool {
        didSet { UserDefaults.standard.set(desktopNotifications, forKey: Keys.desktopNotifications) }
    }
    @Published var completionSound: CompletionSound {
        didSet { UserDefaults.standard.set(completionSound.rawValue, forKey: Keys.completionSound) }
    }
    @Published var autoConvertLongText: Bool {
        didSet { UserDefaults.standard.set(autoConvertLongText, forKey: Keys.autoConvertLongText) }
    }
    @Published var stripPraisePreamble: Bool {
        didSet { UserDefaults.standard.set(stripPraisePreamble, forKey: Keys.stripPraisePreamble) }
    }
    @Published var alwaysShowContextUsage: Bool {
        didSet { UserDefaults.standard.set(alwaysShowContextUsage, forKey: Keys.alwaysShowContextUsage) }
    }
    @Published var interfaceFont: InterfaceFont {
        didSet { UserDefaults.standard.set(interfaceFont.rawValue, forKey: Keys.interfaceFont) }
    }
    @Published var coloredSidebarDiffs: Bool {
        didSet { UserDefaults.standard.set(coloredSidebarDiffs, forKey: Keys.coloredSidebarDiffs) }
    }
    @Published var accessibleColors: AccessibleColorMode {
        didSet { UserDefaults.standard.set(accessibleColors.rawValue, forKey: Keys.accessibleColors) }
    }
    @Published var codeTheme: CodeTheme {
        didSet { UserDefaults.standard.set(codeTheme.rawValue, forKey: Keys.codeTheme) }
    }
    @Published var monoFont: MonoFontChoice {
        didSet { UserDefaults.standard.set(monoFont.rawValue, forKey: Keys.monoFont) }
    }
    @Published var codeLigatures: Bool {
        didSet { UserDefaults.standard.set(codeLigatures, forKey: Keys.codeLigatures) }
    }
    @Published var markdownStyle: MarkdownStyle {
        didSet { UserDefaults.standard.set(markdownStyle.rawValue, forKey: Keys.markdownStyle) }
    }
    @Published var terminalFont: String {
        didSet { UserDefaults.standard.set(terminalFont, forKey: Keys.terminalFont) }
    }
    @Published var terminalFontSize: Double {
        didSet { UserDefaults.standard.set(terminalFontSize, forKey: Keys.terminalFontSize) }
    }
    @Published var defaultModelEffort: ModelEffort {
        didSet { UserDefaults.standard.set(defaultModelEffort.rawValue, forKey: Keys.defaultModelEffort) }
    }
    @Published var reviewModel: String {
        didSet { UserDefaults.standard.set(reviewModel, forKey: Keys.reviewModel) }
    }
    @Published var reviewThinking: ThinkingEffort {
        didSet { UserDefaults.standard.set(reviewThinking.rawValue, forKey: Keys.reviewThinking) }
    }
    @Published var codexPersonality: CodexPersonality {
        didSet { UserDefaults.standard.set(codexPersonality.rawValue, forKey: Keys.codexPersonality) }
    }
    @Published var defaultPlanMode: Bool {
        didSet { UserDefaults.standard.set(defaultPlanMode, forKey: Keys.defaultPlanMode) }
    }
    @Published var defaultFastMode: Bool {
        didSet { UserDefaults.standard.set(defaultFastMode, forKey: Keys.defaultFastMode) }
    }
    @Published var useClaudeChrome: Bool {
        didSet { UserDefaults.standard.set(useClaudeChrome, forKey: Keys.useClaudeChrome) }
    }
    @Published var gitBranchPrefixMode: GitBranchPrefixMode {
        didSet { UserDefaults.standard.set(gitBranchPrefixMode.rawValue, forKey: Keys.gitBranchPrefixMode) }
    }
    @Published var customBranchPrefix: String {
        didSet { UserDefaults.standard.set(customBranchPrefix, forKey: Keys.customBranchPrefix) }
    }
    @Published var renameWorkspaceFromBranch: Bool {
        didSet { UserDefaults.standard.set(renameWorkspaceFromBranch, forKey: Keys.renameWorkspaceFromBranch) }
    }
    @Published var deleteBranchOnArchive: Bool {
        didSet { UserDefaults.standard.set(deleteBranchOnArchive, forKey: Keys.deleteBranchOnArchive) }
    }
    @Published var archiveOnMerge: Bool {
        didSet { UserDefaults.standard.set(archiveOnMerge, forKey: Keys.archiveOnMerge) }
    }
    @Published var showAutomerge: Bool {
        didSet { UserDefaults.standard.set(showAutomerge, forKey: Keys.showAutomerge) }
    }

    var selectedChatModel: String {
        get {
            switch selectedAgentAccount.provider {
            case .codex: codexModel
            case .claudeCode: claudeModel
            }
        }
        set {
            switch selectedAgentAccount.provider {
            case .codex: codexModel = newValue
            case .claudeCode: claudeModel = newValue
            }
        }
    }

    var selectedAgentAccount: AgentAccount {
        AgentAccountCatalog.account(id: selectedAgentAccountID, in: agentAccounts)
    }

    init() {
        let bundledSourceRoot = Bundle.main.object(forInfoDictionaryKey: "PilotSourceRoot") as? String
        let defaultProjectPath = bundledSourceRoot ?? FileManager.default.currentDirectoryPath
        let defaultPilotCommand = bundledSourceRoot == nil ? "pilot" : "go run ./cmd/pilot"
        let storedGatewayURL = UserDefaults.standard.string(forKey: Keys.gatewayURL)
        let resolvedGatewayURL = storedGatewayURL == "http://127.0.0.1:7345" ? Self.defaultGatewayURL : storedGatewayURL ?? Self.defaultGatewayURL
        gatewayURL = resolvedGatewayURL
        UserDefaults.standard.set(resolvedGatewayURL, forKey: Keys.gatewayURL)
        authToken = UserDefaults.standard.string(forKey: Keys.authToken) ?? ""
        pilotCommand = UserDefaults.standard.string(forKey: Keys.pilotCommand) ?? defaultPilotCommand
        let storedProjectPath = UserDefaults.standard.string(forKey: Keys.projectPath)
        let resolvedProjectPath = storedProjectPath.flatMap { path in
            FileManager.default.fileExists(atPath: path) ? path : nil
        } ?? defaultProjectPath
        projectPath = resolvedProjectPath
        UserDefaults.standard.set(resolvedProjectPath, forKey: Keys.projectPath)
        selectedBackend = ExecutionBackend(rawValue: UserDefaults.standard.string(forKey: Keys.selectedBackend) ?? "") ?? .claudeCode
        selectedModel = UserDefaults.standard.string(forKey: Keys.selectedModel) ?? ""
        sandbox = RuntimeSandbox(rawValue: UserDefaults.standard.string(forKey: Keys.sandbox) ?? "") ?? .readOnly
        chatProvider = AgentChatProvider(rawValue: UserDefaults.standard.string(forKey: Keys.chatProvider) ?? "") ?? .codex
        agentAccounts = Self.loadAccounts()
        selectedAgentAccountID = UserDefaults.standard.string(forKey: Keys.selectedAgentAccountID) ?? "codex"
        codexModel = UserDefaults.standard.string(forKey: Keys.codexModel) ?? AgentChatProvider.codex.defaultModel
        claudeModel = UserDefaults.standard.string(forKey: Keys.claudeModel) ?? AgentChatProvider.claudeCode.defaultModel
        appearance = AppAppearance(rawValue: UserDefaults.standard.string(forKey: Keys.appearance) ?? "") ?? .dark
        sendMessageShortcut = SendMessageShortcut(rawValue: UserDefaults.standard.string(forKey: Keys.sendMessageShortcut) ?? "") ?? .enter
        followUpBehavior = FollowUpBehavior(rawValue: UserDefaults.standard.string(forKey: Keys.followUpBehavior) ?? "") ?? .steer
        desktopNotifications = UserDefaults.standard.object(forKey: Keys.desktopNotifications) as? Bool ?? false
        completionSound = CompletionSound(rawValue: UserDefaults.standard.string(forKey: Keys.completionSound) ?? "") ?? .chime
        autoConvertLongText = UserDefaults.standard.object(forKey: Keys.autoConvertLongText) as? Bool ?? true
        stripPraisePreamble = UserDefaults.standard.object(forKey: Keys.stripPraisePreamble) as? Bool ?? false
        alwaysShowContextUsage = UserDefaults.standard.object(forKey: Keys.alwaysShowContextUsage) as? Bool ?? true
        interfaceFont = InterfaceFont(rawValue: UserDefaults.standard.string(forKey: Keys.interfaceFont) ?? "") ?? .system
        coloredSidebarDiffs = UserDefaults.standard.object(forKey: Keys.coloredSidebarDiffs) as? Bool ?? false
        accessibleColors = AccessibleColorMode(rawValue: UserDefaults.standard.string(forKey: Keys.accessibleColors) ?? "") ?? .standard
        codeTheme = CodeTheme(rawValue: UserDefaults.standard.string(forKey: Keys.codeTheme) ?? "") ?? .standard
        monoFont = MonoFontChoice(rawValue: UserDefaults.standard.string(forKey: Keys.monoFont) ?? "") ?? .geistMono
        codeLigatures = UserDefaults.standard.object(forKey: Keys.codeLigatures) as? Bool ?? true
        markdownStyle = MarkdownStyle(rawValue: UserDefaults.standard.string(forKey: Keys.markdownStyle) ?? "") ?? .standard
        terminalFont = UserDefaults.standard.string(forKey: Keys.terminalFont) ?? ""
        terminalFontSize = UserDefaults.standard.object(forKey: Keys.terminalFontSize) as? Double ?? 12
        defaultModelEffort = ModelEffort(rawValue: UserDefaults.standard.string(forKey: Keys.defaultModelEffort) ?? "") ?? .high
        reviewModel = UserDefaults.standard.string(forKey: Keys.reviewModel) ?? "GPT-5.5"
        reviewThinking = ThinkingEffort(rawValue: UserDefaults.standard.string(forKey: Keys.reviewThinking) ?? "") ?? .high
        codexPersonality = CodexPersonality(rawValue: UserDefaults.standard.string(forKey: Keys.codexPersonality) ?? "") ?? .pragmatic
        defaultPlanMode = UserDefaults.standard.object(forKey: Keys.defaultPlanMode) as? Bool ?? false
        defaultFastMode = UserDefaults.standard.object(forKey: Keys.defaultFastMode) as? Bool ?? false
        useClaudeChrome = UserDefaults.standard.object(forKey: Keys.useClaudeChrome) as? Bool ?? false
        gitBranchPrefixMode = GitBranchPrefixMode(rawValue: UserDefaults.standard.string(forKey: Keys.gitBranchPrefixMode) ?? "") ?? .githubUsername
        customBranchPrefix = UserDefaults.standard.string(forKey: Keys.customBranchPrefix) ?? ""
        renameWorkspaceFromBranch = UserDefaults.standard.object(forKey: Keys.renameWorkspaceFromBranch) as? Bool ?? true
        deleteBranchOnArchive = UserDefaults.standard.object(forKey: Keys.deleteBranchOnArchive) as? Bool ?? false
        archiveOnMerge = UserDefaults.standard.object(forKey: Keys.archiveOnMerge) as? Bool ?? false
        showAutomerge = UserDefaults.standard.object(forKey: Keys.showAutomerge) as? Bool ?? false
        chatProvider = AgentAccountCatalog.account(id: selectedAgentAccountID, in: agentAccounts).provider
        Self.saveAccounts(agentAccounts)
    }

    func addAgentAccount() -> AgentAccount {
        let account = AgentAccount(
            id: "account-\(UUID().uuidString)",
            name: "New account",
            provider: .claudeCode,
            plan: "Claude Max",
            scope: "work",
            organization: ""
        )
        agentAccounts.append(account)
        selectedAgentAccountID = account.id
        return account
    }

    func updateAgentAccount(_ account: AgentAccount) {
        guard let index = agentAccounts.firstIndex(where: { $0.id == account.id }) else { return }
        agentAccounts[index] = account
        if selectedAgentAccountID == account.id {
            chatProvider = account.provider
            if !account.defaultModel.isEmpty {
                selectedChatModel = account.defaultModel
            }
        }
    }

    func deleteAgentAccount(id: String) {
        guard agentAccounts.count > 1 else { return }
        agentAccounts.removeAll { $0.id == id }
    }

    private enum Keys {
        static let gatewayURL = "pilot91.gatewayURL"
        static let authToken = "pilot91.authToken"
        static let pilotCommand = "pilot91.pilotCommand"
        static let projectPath = "pilot91.projectPath"
        static let selectedBackend = "pilot91.selectedBackend"
        static let selectedModel = "pilot91.selectedModel"
        static let sandbox = "pilot91.sandbox"
        static let chatProvider = "pilot91.chatProvider"
        static let agentAccounts = "pilot91.agentAccounts"
        static let selectedAgentAccountID = "pilot91.selectedAgentAccountID"
        static let codexModel = "pilot91.codexModel"
        static let claudeModel = "pilot91.claudeModel"
        static let appearance = "pilot91.appearance"
        static let sendMessageShortcut = "pilot91.sendMessageShortcut"
        static let followUpBehavior = "pilot91.followUpBehavior"
        static let desktopNotifications = "pilot91.desktopNotifications"
        static let completionSound = "pilot91.completionSound"
        static let autoConvertLongText = "pilot91.autoConvertLongText"
        static let stripPraisePreamble = "pilot91.stripPraisePreamble"
        static let alwaysShowContextUsage = "pilot91.alwaysShowContextUsage"
        static let interfaceFont = "pilot91.interfaceFont"
        static let coloredSidebarDiffs = "pilot91.coloredSidebarDiffs"
        static let accessibleColors = "pilot91.accessibleColors"
        static let codeTheme = "pilot91.codeTheme"
        static let monoFont = "pilot91.monoFont"
        static let codeLigatures = "pilot91.codeLigatures"
        static let markdownStyle = "pilot91.markdownStyle"
        static let terminalFont = "pilot91.terminalFont"
        static let terminalFontSize = "pilot91.terminalFontSize"
        static let defaultModelEffort = "pilot91.defaultModelEffort"
        static let reviewModel = "pilot91.reviewModel"
        static let reviewThinking = "pilot91.reviewThinking"
        static let codexPersonality = "pilot91.codexPersonality"
        static let defaultPlanMode = "pilot91.defaultPlanMode"
        static let defaultFastMode = "pilot91.defaultFastMode"
        static let useClaudeChrome = "pilot91.useClaudeChrome"
        static let gitBranchPrefixMode = "pilot91.gitBranchPrefixMode"
        static let customBranchPrefix = "pilot91.customBranchPrefix"
        static let renameWorkspaceFromBranch = "pilot91.renameWorkspaceFromBranch"
        static let deleteBranchOnArchive = "pilot91.deleteBranchOnArchive"
        static let archiveOnMerge = "pilot91.archiveOnMerge"
        static let showAutomerge = "pilot91.showAutomerge"
    }

    private static let defaultGatewayURL = "http://127.0.0.1:9090"

    private static func loadAccounts() -> [AgentAccount] {
        guard let data = UserDefaults.standard.data(forKey: Keys.agentAccounts),
              let accounts = try? JSONDecoder().decode([AgentAccount].self, from: data),
              !accounts.isEmpty else {
            return AgentAccountCatalog.defaults
        }
        return normalizeAccounts(accounts)
    }

    private static func saveAccounts(_ accounts: [AgentAccount]) {
        guard let data = try? JSONEncoder().encode(accounts) else { return }
        UserDefaults.standard.set(data, forKey: Keys.agentAccounts)
    }

    private static func normalizeAccounts(_ accounts: [AgentAccount]) -> [AgentAccount] {
        accounts.map { account in
            guard let catalog = AgentAccountCatalog.defaults.first(where: { $0.id == account.id }) else {
                return account
            }
            var updated = account
            if updated.configPath.isEmpty || updated.configPath == AgentAccount.defaultConfigPath(for: updated.provider) {
                updated.configPath = catalog.configPath
            }
            if updated.environment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                updated.environment = catalog.environment
            } else if updated.id == "claude" && updated.environment.trimmingCharacters(in: .whitespacesAndNewlines) == "CLAUDE_CONFIG_DIR=~/.claude" {
                updated.environment = ""
            }
            if updated.profileName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                updated.profileName = catalog.profileName
            }
            return updated
        }
    }
}
