import AppKit
import Foundation
import SwiftUI

private enum SettingsCategory: String, CaseIterable, Identifiable {
    case general = "General"
    case models = "Models"
    case providers = "Providers"
    case environment = "Environment"
    case appearance = "Appearance"
    case git = "Git"
    case account = "Account"
    case experimental = "Experimental"
    case advanced = "Advanced"

    var id: String { rawValue }

    var systemImage: String {
        switch self {
        case .general: "slider.horizontal.3"
        case .models: "cpu"
        case .providers: "key"
        case .environment: "key.horizontal"
        case .appearance: "paintpalette"
        case .git: "point.3.connected.trianglepath.dotted"
        case .account: "person.circle"
        case .experimental: "flask"
        case .advanced: "externaldrive"
        }
    }
}

struct SettingsView: View {
    @EnvironmentObject private var store: AppStore

    @State private var configPath = ""
    @State private var configValues: [String: String] = [:]
    @State private var originalValues: [String: String] = [:]
    @State private var statusMessage = ""
    @State private var statusIsError = false
    @State private var isBusy = false
    @State private var selectedCategory: SettingsCategory = .general

    private let columns = [
        GridItem(.adaptive(minimum: 360), spacing: 12)
    ]

    var body: some View {
        HStack(alignment: .top, spacing: 16) {
            settingsCategoryList
                .frame(width: 210)

            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    settingsTitle
                    selectedSettingsContent
                }
                .padding(.bottom, 18)
            }
        }
        .task {
            if configValues.isEmpty {
                await loadConfig()
            }
        }
    }

    private var settingsCategoryList: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(SettingsCategory.allCases) { category in
                Button {
                    selectedCategory = category
                } label: {
                    HStack(spacing: 9) {
                        Image(systemName: category.systemImage)
                            .frame(width: 18)
                        Text(category.rawValue)
                            .lineLimit(1)
                        Spacer()
                    }
                    .padding(.horizontal, 10)
                    .frame(height: 34)
                    .background(selectedCategory == category ? Color.secondary.opacity(0.14) : Color.clear)
                    .clipShape(RoundedRectangle(cornerRadius: 7))
                }
                .buttonStyle(.plain)
            }
            Spacer()
        }
        .padding(.top, 4)
    }

    @ViewBuilder
    private var selectedSettingsContent: some View {
        switch selectedCategory {
        case .general:
            GeneralSettingsPanel()
        case .models:
            ModelsSettingsPanel()
        case .providers:
            ProvidersSettingsPanel()
        case .environment:
            EnvironmentSettingsPanel()
        case .appearance:
            AppearanceSettingsPanel()
        case .git:
            GitSettingsPanel()
        case .account:
            AccountSettingsPanel()
        case .experimental:
            configGroup("Experimental", sections: ConfigSectionCatalog.automation)
        case .advanced:
            VStack(alignment: .leading, spacing: 14) {
                configHeader
                configGroup("Core", sections: ConfigSectionCatalog.core)
                configGroup("Projects", sections: ConfigSectionCatalog.projects)
                configGroup("Notifications", sections: ConfigSectionCatalog.notifications)
                configGroup("More ticket sources", sections: Array(ConfigSectionCatalog.ticketSources.dropFirst(4)))
            }
        }
    }

    private var settingsTitle: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(selectedCategory.rawValue)
                .font(.title.weight(.semibold))
            Text(settingsSubtitle)
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding(.top, 18)
    }

    private var settingsSubtitle: String {
        switch selectedCategory {
        case .general:
            "Message behavior, notifications and desktop workflow."
        case .models:
            "Model defaults used by chat, Pilot tasks and runtime sessions."
        case .providers:
            "Configure Claude Code and Codex accounts."
        case .environment:
            "Local project path, gateway, CLI command and sandbox defaults."
        case .appearance:
            "Theme, density and type choices for the desktop shell."
        case .git:
            "Repository providers and ticket source integrations."
        case .account:
            "The active account used by this workspace."
        case .experimental:
            "Automation and pilot features that need explicit control."
        case .advanced:
            "Raw Pilot config editor for daemon and adapter settings."
        }
    }

    private var configHeader: some View {
        Panel(title: "Pilot Configuration", subtitle: "Edits the active Pilot config file used by the CLI and daemon") {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 8) {
                    Text(configPath.isEmpty ? "~/.pilot/config.yaml" : configPath)
                        .font(.system(.callout, design: .monospaced))
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 7)
                        .background(Color.secondary.opacity(0.08))
                        .clipShape(RoundedRectangle(cornerRadius: 6))

                    Button {
                        Task { await loadConfig() }
                    } label: {
                        Label("Reload", systemImage: "arrow.clockwise")
                    }
                    .disabled(isBusy)

                    Button {
                        Task { await saveAll() }
                    } label: {
                        Label("Save All", systemImage: "square.and.arrow.down")
                    }
                    .disabled(isBusy || changedFields.isEmpty)

                    Button {
                        Task { await validateConfig() }
                    } label: {
                        Label("Validate", systemImage: "checkmark.seal")
                    }
                    .disabled(isBusy)

                    Button {
                        openConfigFile()
                    } label: {
                        Label("Open File", systemImage: "doc")
                    }
                    .disabled(configPath.isEmpty)
                }

                if !statusMessage.isEmpty {
                    Text(statusMessage)
                        .font(.caption)
                        .foregroundStyle(statusIsError ? .red : .secondary)
                        .lineLimit(3)
                }
            }
        }
    }

    private func configGroup(_ title: String, sections: [ConfigEditorSection]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title)
                .font(.headline)
                .foregroundStyle(.secondary)

            LazyVGrid(columns: columns, alignment: .leading, spacing: 12) {
                ForEach(sections) { section in
                    configSectionCard(section)
                }
            }
        }
    }

    private func configSectionCard(_ section: ConfigEditorSection) -> some View {
        VStack(alignment: .leading, spacing: 11) {
            HStack(alignment: .top, spacing: 8) {
                Label(section.title, systemImage: section.systemImage)
                    .font(.headline)
                Spacer()
                if section.fields.contains(where: isDirty) {
                    StatusBadge(text: "modified")
                }
            }

            if let subtitle = section.subtitle {
                Text(subtitle)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            VStack(alignment: .leading, spacing: 8) {
                ForEach(section.fields) { field in
                    configFieldRow(field)
                }
            }

            HStack {
                Spacer()
                Button {
                    Task { await save(section) }
                } label: {
                    Label("Save", systemImage: "square.and.arrow.down")
                }
                .controlSize(.small)
                .disabled(isBusy || !section.fields.contains(where: isDirty))
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .background(Color.secondary.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func configFieldRow(_ field: ConfigFieldDefinition) -> some View {
        Grid(alignment: .leading, horizontalSpacing: 10, verticalSpacing: 4) {
            GridRow {
                Text(field.title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(width: 112, alignment: .leading)

                fieldControl(field)
            }
        }
    }

    @ViewBuilder
    private func fieldControl(_ field: ConfigFieldDefinition) -> some View {
        switch field.kind {
        case .boolean:
            Toggle("", isOn: boolBinding(for: field))
                .toggleStyle(.switch)
                .labelsHidden()
        case .integer:
            TextField(field.placeholder, text: textBinding(for: field))
                .textFieldStyle(.roundedBorder)
                .frame(maxWidth: .infinity)
        case .list:
            TextField(field.placeholder, text: textBinding(for: field))
                .textFieldStyle(.roundedBorder)
                .frame(maxWidth: .infinity)
        case .picker(let options):
            Picker("", selection: textBinding(for: field)) {
                ForEach(options, id: \.self) { option in
                    Text(option.isEmpty ? "default" : option).tag(option)
                }
            }
            .labelsHidden()
            .frame(maxWidth: .infinity, alignment: .leading)
        case .secure:
            SecureField(field.placeholder, text: textBinding(for: field))
                .textFieldStyle(.roundedBorder)
                .frame(maxWidth: .infinity)
        case .text:
            TextField(field.placeholder, text: textBinding(for: field))
                .textFieldStyle(.roundedBorder)
                .frame(maxWidth: .infinity)
        }
    }

    private func textBinding(for field: ConfigFieldDefinition) -> Binding<String> {
        Binding(
            get: { configValues[field.id] ?? "" },
            set: { configValues[field.id] = $0 }
        )
    }

    private func boolBinding(for field: ConfigFieldDefinition) -> Binding<Bool> {
        Binding(
            get: { (configValues[field.id] ?? "").lowercased() == "true" },
            set: { configValues[field.id] = $0 ? "true" : "false" }
        )
    }

    private func isDirty(_ field: ConfigFieldDefinition) -> Bool {
        (configValues[field.id] ?? "") != (originalValues[field.id] ?? "")
    }

    private var changedFields: [ConfigFieldDefinition] {
        ConfigSectionCatalog.allFields.filter(isDirty)
    }

    private func loadConfig() async {
        isBusy = true
        defer { isBusy = false }

        let cli = makeCLI()
        let pathRun = await cli.run(arguments: ["config", "path"])
        record(pathRun)
        if pathRun.exitCode == 0 {
            configPath = pathRun.output.trimmingCharacters(in: .whitespacesAndNewlines)
        }

        let run = await cli.run(arguments: ["config", "show", "--json"])
        record(run)
        guard run.exitCode == 0 else {
            setStatus("Config load failed: \(trimmed(run.output))", error: true)
            return
        }

        do {
            let data = Data(run.output.utf8)
            let object = try JSONSerialization.jsonObject(with: data)
            var loaded: [String: String] = [:]
            for field in ConfigSectionCatalog.allFields {
                loaded[field.id] = fieldValue(field, object: object)
            }
            configValues = loaded
            originalValues = loaded
            syncRuntimeDefaultsFromConfig()
            setStatus("Loaded \(configPath.isEmpty ? "active config" : configPath)", error: false)
        } catch {
            setStatus("Config JSON parse failed: \(error.localizedDescription)", error: true)
        }
    }

    private func save(_ section: ConfigEditorSection) async {
        await save(fields: section.fields.filter(isDirty), title: section.title)
    }

    private func saveAll() async {
        await save(fields: changedFields, title: "all changes")
    }

    private func save(fields: [ConfigFieldDefinition], title: String) async {
        guard !fields.isEmpty else {
            setStatus("No changes to save.", error: false)
            return
        }

        isBusy = true
        defer { isBusy = false }

        var args = ["config", "set"]
        for field in fields {
            args.append(field.yamlPath)
            args.append(serializedValue(for: field))
        }

        let run = await makeCLI().run(arguments: args)
        record(run)
        guard run.exitCode == 0 else {
            setStatus("Save failed: \(trimmed(run.output))", error: true)
            return
        }

        let validateRun = await makeCLI().run(arguments: ["config", "validate"])
        record(validateRun)
        if validateRun.exitCode == 0 {
            setStatus("Saved \(title). Validation OK.", error: false)
            await loadConfig()
        } else {
            setStatus("Saved, but validation failed: \(trimmed(validateRun.output))", error: true)
        }
    }

    private func validateConfig() async {
        isBusy = true
        defer { isBusy = false }

        let run = await makeCLI().run(arguments: ["config", "validate"])
        record(run)
        if run.exitCode == 0 {
            setStatus(trimmed(run.output), error: false)
        } else {
            setStatus("Validation failed: \(trimmed(run.output))", error: true)
        }
    }

    private func openConfigFile() {
        guard !configPath.isEmpty else { return }
        NSWorkspace.shared.open(URL(fileURLWithPath: configPath))
    }

    private func syncRuntimeDefaultsFromConfig() {
        let host = configValues["gateway.host"] ?? "127.0.0.1"
        let port = configValues["gateway.port"] ?? "9090"
        if !host.isEmpty && !port.isEmpty {
            store.settings.gatewayURL = "http://\(host):\(port)"
        }
        if let backend = ExecutionBackend(rawValue: configValues["executor.type"] ?? "") {
            store.settings.selectedBackend = backend
        }
        store.settings.sandbox = RuntimeSandbox(rawValue: configValues["gateway.codex_runtime.sandbox"] ?? "") ?? store.settings.sandbox
        store.settings.selectedModel = configValues["gateway.codex_runtime.model"] ?? store.settings.selectedModel
    }

    private func makeCLI() -> PilotCLI {
        PilotCLI(pilotCommand: store.settings.pilotCommand, workingDirectory: store.settings.projectPath)
    }

    private func record(_ run: CommandRun) {
        store.commandRuns.insert(run, at: 0)
    }

    private func setStatus(_ message: String, error: Bool) {
        statusMessage = message
        statusIsError = error
    }

    private func fieldValue(_ field: ConfigFieldDefinition, object: Any) -> String {
        guard let value = jsonValue(object, path: field.jsonPath) else {
            return ""
        }
        if let bool = value as? Bool {
            return bool ? "true" : "false"
        }
        if let string = value as? String {
            return string
        }
        if let number = value as? NSNumber {
            return number.stringValue
        }
        if let array = value as? [Any] {
            return array.map { "\($0)" }.joined(separator: ", ")
        }
        return ""
    }

    private func jsonValue(_ object: Any, path: [String]) -> Any? {
        var current: Any? = object
        for segment in path {
            if let index = Int(segment), let array = current as? [Any] {
                current = array.indices.contains(index) ? array[index] : nil
            } else if let dictionary = current as? [String: Any] {
                current = dictionary[segment]
            } else {
                return nil
            }
        }
        return current
    }

    private func serializedValue(for field: ConfigFieldDefinition) -> String {
        let value = (configValues[field.id] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        switch field.kind {
        case .list(let element):
            let values = value
                .split(separator: ",")
                .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
                .filter { !$0.isEmpty }
            switch element {
            case .integer:
                let numbers = values.compactMap(Int.init)
                return jsonString(numbers)
            case .string:
                return jsonString(values)
            }
        default:
            return value
        }
    }

    private func jsonString(_ value: Any) -> String {
        guard JSONSerialization.isValidJSONObject(value),
              let data = try? JSONSerialization.data(withJSONObject: value),
              let string = String(data: data, encoding: .utf8) else {
            return "[]"
        }
        return string
    }

    private func trimmed(_ value: String) -> String {
        value.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

private struct GeneralSettingsPanel: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        Panel(title: "General", subtitle: "Desktop behavior for every workspace chat") {
            VStack(alignment: .leading, spacing: 0) {
                settingsPickerRow(
                    title: "Send messages with",
                    detail: "Choose which key combination sends messages.",
                    width: 260
                ) {
                    Picker("Send messages with", selection: $store.settings.sendMessageShortcut) {
                        ForEach(SendMessageShortcut.allCases) { shortcut in
                            Text(shortcut.label).tag(shortcut)
                        }
                    }
                    .labelsHidden()
                }
                settingsDivider
                settingsPickerRow(
                    title: "Follow-up behavior",
                    detail: "Steer the running agent or queue the next message.",
                    width: 260
                ) {
                    Picker("Follow-up behavior", selection: $store.settings.followUpBehavior) {
                        ForEach(FollowUpBehavior.allCases) { behavior in
                            Text(behavior.label).tag(behavior)
                        }
                    }
                    .labelsHidden()
                }
                settingsDivider
                settingsToggleRow(
                    title: "Desktop notifications",
                    detail: "Get notified when AI finishes working in a chat.",
                    isOn: $store.settings.desktopNotifications
                )
                settingsDivider
                settingsPickerRow(
                    title: "Completion sound",
                    detail: "Choose what plays when AI finishes working.",
                    width: 260
                ) {
                    Picker("Completion sound", selection: $store.settings.completionSound) {
                        ForEach(CompletionSound.allCases) { sound in
                            Text(sound.label).tag(sound)
                        }
                    }
                    .labelsHidden()

                    Button("Test") {
                        playCompletionSound(store.settings.completionSound)
                    }
                    .disabled(store.settings.completionSound == .none)
                }
                settingsDivider
                settingsToggleRow(
                    title: "Auto-convert long text",
                    detail: "Convert pasted text over 5000 characters into text attachments.",
                    isOn: $store.settings.autoConvertLongText
                )
                settingsDivider
                settingsToggleRow(
                    title: "Strip praise prefaces",
                    detail: "Remove validation-style prefaces from assistant messages.",
                    isOn: $store.settings.stripPraisePreamble
                )
                settingsDivider
                settingsToggleRow(
                    title: "Always show context usage",
                    detail: "Show model context usage in the workspace controls.",
                    isOn: $store.settings.alwaysShowContextUsage
                )
            }
        }
    }

    private func playCompletionSound(_ sound: CompletionSound) {
        let soundName: NSSound.Name
        switch sound {
        case .none:
            return
        case .chime:
            soundName = .init("Ping")
        case .glass:
            soundName = .init("Glass")
        }
        NSSound(named: soundName)?.play()
    }
}

private struct ModelsSettingsPanel: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        Panel(title: "Models", subtitle: "Defaults used by new chats, reviews and task execution") {
            VStack(alignment: .leading, spacing: 0) {
                settingsPickerRow(title: "Default model", detail: "Model for new chats and workspace tasks.", width: 470) {
                    HStack(spacing: 0) {
                        Picker("Default model", selection: Binding(
                            get: { store.settings.selectedChatModel },
                            set: { store.settings.selectedChatModel = $0 }
                        )) {
                            ForEach(defaultModelOptions, id: \.self) { model in
                                Text(model).tag(model)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 235)

                        Picker("Claude Code effort", selection: $store.settings.defaultModelEffort) {
                            ForEach(ModelEffort.allCases) { effort in
                                Text(effort.label).tag(effort)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 235)
                    }
                }
                settingsDivider
                settingsPickerRow(title: "Review model", detail: "Model for code reviews and PR checks.", width: 470) {
                    HStack(spacing: 0) {
                        Picker("Review model", selection: $store.settings.reviewModel) {
                            ForEach(reviewModelOptions, id: \.self) { model in
                                Text(model).tag(model)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 235)

                        Picker("OpenAI reasoning effort", selection: $store.settings.reviewThinking) {
                            ForEach(ThinkingEffort.allCases) { reasoning in
                                Text(reasoning.label).tag(reasoning)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 235)
                    }
                }
                settingsDivider
                settingsPickerRow(title: "Codex personality for new chats", detail: "Style to use when a new chat starts with a Codex model.", width: 310) {
                    Picker("Codex personality", selection: $store.settings.codexPersonality) {
                        ForEach(CodexPersonality.allCases) { personality in
                            Text(personality.label).tag(personality)
                        }
                    }
                    .labelsHidden()
                }
                settingsDivider
                settingsToggleRow(
                    title: "Default to plan mode",
                    detail: "Start new chats in plan mode.",
                    isOn: $store.settings.defaultPlanMode
                )
                settingsDivider
                settingsToggleRow(
                    title: "Default to fast mode",
                    detail: "Start new chats in fast mode.",
                    isOn: $store.settings.defaultFastMode
                )
                settingsDivider
                settingsToggleRow(
                    title: "Use Claude Code with Chrome",
                    detail: "Allow Claude Code browser workflows when the extension is installed.",
                    isOn: $store.settings.useClaudeChrome
                )
                settingsDivider
                settingsPickerRow(title: "Workspace backend", detail: "Executor used for Pilot tasks.", width: 250) {
                    Picker("Workspace backend", selection: $store.settings.selectedBackend) {
                        ForEach(ExecutionBackend.allCases) { backend in
                            Text(backend.label).tag(backend)
                        }
                    }
                    .labelsHidden()
                }
                settingsDivider
                settingsPickerRow(title: "Runtime sandbox", detail: "Default filesystem access for runtime sessions.", width: 250) {
                    Picker("Sandbox", selection: $store.settings.sandbox) {
                        ForEach(RuntimeSandbox.allCases) { sandbox in
                            Text(sandbox.rawValue).tag(sandbox)
                        }
                    }
                    .labelsHidden()
                }
            }
        }
    }

    private var defaultModelOptions: [String] {
        uniqueModels(["Opus 4.8 1M", store.settings.selectedChatModel] + store.settings.selectedAgentAccount.modelOptions)
    }

    private var reviewModelOptions: [String] {
        uniqueModels([store.settings.reviewModel, "GPT-5.5", "GPT-5.4", "GPT-5", "Opus 4.8 1M", "Claude Sonnet 4.6"])
    }

    private func uniqueModels(_ models: [String]) -> [String] {
        var seen = Set<String>()
        return models.filter { model in
            let trimmed = model.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty, !seen.contains(trimmed) else { return false }
            seen.insert(trimmed)
            return true
        }
    }
}

private struct EnvironmentSettingsPanel: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        Panel(title: "Environment", subtitle: "Local app runtime and selected project") {
            VStack(alignment: .leading, spacing: 0) {
                settingsTextRow(title: "Project path", detail: "Workspace root used by chat, terminal and Pilot tasks.", text: $store.settings.projectPath)
                settingsDivider
                settingsTextRow(title: "Pilot command", detail: "CLI command used for daemon and task actions.", text: $store.settings.pilotCommand)
                settingsDivider
                settingsTextRow(title: "Gateway URL", detail: "Local app-server/gateway endpoint.", text: $store.settings.gatewayURL)
                settingsDivider
                settingsSecureRow(title: "Auth token", detail: "Optional local gateway token.", text: $store.settings.authToken)

                HStack {
                    Button {
                        Task { await store.openProjectFolder() }
                    } label: {
                        Label("Open project...", systemImage: "folder.badge.plus")
                    }
                    Button {
                        store.connectRuntime()
                    } label: {
                        Label("Reconnect runtime", systemImage: "bolt.horizontal")
                    }
                    Spacer()
                }
                .padding(.top, 12)
            }
        }
    }
}

private struct AccountSettingsPanel: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        Panel(title: "Account", subtitle: "Active account for the current workspace") {
            VStack(alignment: .leading, spacing: 14) {
                HStack(spacing: 10) {
                    Picker("Account", selection: $store.settings.selectedAgentAccountID) {
                        ForEach(store.settings.agentAccounts) { account in
                            Text(account.name).tag(account.id)
                        }
                    }
                    .frame(width: 240)

                    StatusBadge(text: store.accountStatus(for: store.settings.selectedAgentAccount).headline)
                    StatusBadge(text: store.settings.selectedAgentAccount.provider.label)
                    Spacer()
                }

                ProviderDetailTable(status: store.accountStatus(for: store.settings.selectedAgentAccount))
            }
        }
    }
}

private struct AppearanceSettingsPanel: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        Panel(title: "Appearance", subtitle: "Theme, code colors and editor typography") {
            VStack(alignment: .leading, spacing: 0) {
                settingsPickerRow(title: "Theme", detail: "Dark matches the workspace, code preview and terminal.", width: 260) {
                    Picker("Theme", selection: $store.settings.appearance) {
                        ForEach(AppAppearance.allCases) { appearance in
                            Text(appearance.label).tag(appearance)
                        }
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                }
                settingsDivider
                settingsToggleRow(
                    title: "Colored sidebar diffs",
                    detail: "Always show line change colors in the sidebar, even for unselected workspaces.",
                    isOn: $store.settings.coloredSidebarDiffs
                )
                settingsDivider
                settingsPickerRow(title: "Accessible colors", detail: "Theme optimized for color vision differences.", width: 360) {
                    Picker("Accessible colors", selection: $store.settings.accessibleColors) {
                        ForEach(AccessibleColorMode.allCases) { mode in
                            Text(mode.label).tag(mode)
                        }
                    }
                    .labelsHidden()
                }
                settingsDivider
                settingsPickerRow(title: "Code theme", detail: "Syntax highlighting colors for code blocks and editors.", width: 360) {
                    Picker("Code theme", selection: $store.settings.codeTheme) {
                        ForEach(CodeTheme.allCases) { theme in
                            Text(theme.label).tag(theme)
                        }
                    }
                    .labelsHidden()
                }
                codePreview
                settingsDivider
                settingsPickerRow(title: "Mono Font", detail: "Font used for code and diffs.", width: 360) {
                    Picker("Mono Font", selection: $store.settings.monoFont) {
                        ForEach(MonoFontChoice.allCases) { font in
                            Text(font.label).tag(font)
                        }
                    }
                    .labelsHidden()
                }
                monoPreview
                settingsDivider
                settingsToggleRow(
                    title: "Code ligatures",
                    detail: "Use font ligatures in file editors and diffs.",
                    isOn: $store.settings.codeLigatures
                )
                settingsDivider
                settingsPickerRow(title: "Markdown Style", detail: "Rendering style for markdown files.", width: 360) {
                    Picker("Markdown Style", selection: $store.settings.markdownStyle) {
                        ForEach(MarkdownStyle.allCases) { style in
                            Text(style.label).tag(style)
                        }
                    }
                    .labelsHidden()
                }
                markdownPreview
                settingsDivider
                settingsTextRow(
                    title: "Terminal Font",
                    detail: "Leave empty for default. Enter font name exactly as installed.",
                    text: $store.settings.terminalFont
                )
                settingsDivider
                HStack(alignment: .center, spacing: 18) {
                    settingsRowText(title: "Terminal Font Size", detail: "Font size used by the terminal dock.")
                    Spacer()
                    Text("\(Int(store.settings.terminalFontSize))px")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.secondary)
                    Slider(value: $store.settings.terminalFontSize, in: 10...18, step: 1)
                        .frame(width: 280)
                }
                terminalPreview
            }
        }
    }

    private var codePreview: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text("// Fetch user data")
                .foregroundStyle(store.settings.codeTheme.comment)
            HStack(spacing: 0) {
                Text("async function ")
                    .foregroundStyle(store.settings.codeTheme.keyword)
                Text("getUser")
                    .foregroundStyle(store.settings.codeTheme.function)
                Text("(id: number): Promise<User> {")
                    .foregroundStyle(.primary)
            }
            HStack(spacing: 0) {
                Text("  const ")
                    .foregroundStyle(store.settings.codeTheme.keyword)
                Text("response")
                Text(" = await ")
                    .foregroundStyle(store.settings.codeTheme.keyword)
                Text("fetch")
                    .foregroundStyle(store.settings.codeTheme.function)
                Text("('/api/users/${id}');")
                    .foregroundStyle(store.settings.codeTheme.string)
            }
            Text("}")
                .foregroundStyle(.primary)
        }
        .font(store.settings.monoFont.font(size: 14))
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(store.settings.codeTheme.background)
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .padding(.top, 10)
    }

    private var monoPreview: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text("// Preview")
                .foregroundStyle(.secondary)
            Text("const greeting = 'Hello, World!';")
            Text("function sum(a, b) { return a + b; }")
        }
        .font(store.settings.monoFont.font(size: 14))
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.secondary.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .padding(.top, 10)
    }

    private var markdownPreview: some View {
        VStack(alignment: .leading, spacing: store.settings.markdownStyle == .compact ? 8 : 16) {
            Text("Add New Button")
                .font(markdownTitleFont)
            Text("Believe it or not, this plan will add a new button to the interface.")
                .font(.body)
                .foregroundStyle(.secondary)
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.secondary.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .padding(.top, 10)
    }

    private var markdownTitleFont: Font {
        switch store.settings.markdownStyle {
        case .standard: .largeTitle.weight(.semibold)
        case .compact: .title2.weight(.semibold)
        case .document: .title.weight(.semibold)
        }
    }

    private var terminalPreview: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text("~/project main +3 ??")
            Text("> npm test ✓")
            Text("└─ ▶ All tests passed!")
        }
        .font(terminalFont)
        .foregroundStyle(Color.green)
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.black)
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .padding(.top, 10)
    }

    private var terminalFont: Font {
        let fontName = store.settings.terminalFont.trimmingCharacters(in: .whitespacesAndNewlines)
        if fontName.isEmpty {
            return store.settings.monoFont.font(size: CGFloat(store.settings.terminalFontSize))
        }
        return .custom(fontName, size: CGFloat(store.settings.terminalFontSize))
    }
}

private struct GitSettingsPanel: View {
    @EnvironmentObject private var store: AppStore

    var body: some View {
        Panel(title: "Git", subtitle: "Workspace branch naming, archive and merge behavior") {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .top, spacing: 18) {
                    settingsRowText(
                        title: "Branch name prefix",
                        detail: "Prefix for new workspace branch names."
                    )
                    Spacer()
                    Picker("Branch name prefix", selection: $store.settings.gitBranchPrefixMode) {
                        ForEach(GitBranchPrefixMode.allCases) { mode in
                            Text(mode.label).tag(mode)
                        }
                    }
                    .pickerStyle(.radioGroup)
                    .labelsHidden()
                    .frame(width: 310, alignment: .leading)
                }
                if store.settings.gitBranchPrefixMode == .custom {
                    HStack {
                        Spacer()
                        TextField("custom-prefix", text: $store.settings.customBranchPrefix)
                            .textFieldStyle(.roundedBorder)
                            .frame(width: 310)
                    }
                    .padding(.top, 8)
                }
                settingsDivider
                settingsToggleRow(
                    title: "Rename workspace when branch is named",
                    detail: "Automatically rename placeholder workspaces to the generated branch name.",
                    isOn: $store.settings.renameWorkspaceFromBranch
                )
                settingsDivider
                settingsToggleRow(
                    title: "Delete branch on archive",
                    detail: "Delete the local branch when archiving a workspace.",
                    isOn: $store.settings.deleteBranchOnArchive
                )
                settingsDivider
                settingsToggleRow(
                    title: "Archive on merge",
                    detail: "Automatically archive a workspace after merging its PR.",
                    isOn: $store.settings.archiveOnMerge
                )
                settingsDivider
                settingsToggleRow(
                    title: "Automerge",
                    detail: "Show an automerge button in the Git panel when checks are pending.",
                    isOn: $store.settings.showAutomerge
                )
                settingsDivider
                configLinks
            }
        }
    }

    private var configLinks: some View {
        HStack(spacing: 10) {
            Button {
                store.selectedSection = .settings
            } label: {
                Label("Project config", systemImage: "folder")
            }
            Button {
                NSWorkspace.shared.open(URL(string: "https://github.com/settings/repositories")!)
            } label: {
                Label("Configure on GitHub", systemImage: "arrow.up.right.square")
            }
            Spacer()
        }
    }
}

private var settingsDivider: some View {
    Divider()
        .padding(.vertical, 12)
}

private func settingsPickerRow<Content: View>(
    title: String,
    detail: String,
    width: CGFloat,
    @ViewBuilder content: () -> Content
) -> some View {
    HStack(alignment: .center, spacing: 18) {
        settingsRowText(title: title, detail: detail)
        Spacer()
        HStack(spacing: 10) {
            content()
        }
        .frame(width: width, alignment: .trailing)
    }
}

private func settingsToggleRow(title: String, detail: String, isOn: Binding<Bool>) -> some View {
    HStack(alignment: .center, spacing: 18) {
        settingsRowText(title: title, detail: detail)
        Spacer()
        Toggle("", isOn: isOn)
            .toggleStyle(.switch)
            .labelsHidden()
    }
}

private func settingsTextRow(title: String, detail: String, text: Binding<String>) -> some View {
    HStack(alignment: .center, spacing: 18) {
        settingsRowText(title: title, detail: detail)
        TextField(title, text: text)
            .textFieldStyle(.roundedBorder)
            .frame(width: 420)
    }
}

private func settingsSecureRow(title: String, detail: String, text: Binding<String>) -> some View {
    HStack(alignment: .center, spacing: 18) {
        settingsRowText(title: title, detail: detail)
        SecureField(title, text: text)
            .textFieldStyle(.roundedBorder)
            .frame(width: 420)
    }
}

private func settingsRowText(title: String, detail: String) -> some View {
    VStack(alignment: .leading, spacing: 4) {
        Text(title)
            .font(.headline)
        Text(detail)
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .lineLimit(2)
    }
}

private struct ProvidersSettingsPanel: View {
    @EnvironmentObject private var store: AppStore
    @State private var selectedAccountID = "codex"

    var body: some View {
        Panel(title: "Providers", subtitle: "Manage the accounts used by workspace sessions") {
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    Picker("Account", selection: $selectedAccountID) {
                        ForEach(store.settings.agentAccounts) { account in
                            Text(account.name).tag(account.id)
                        }
                    }
                    .pickerStyle(.menu)
                    .frame(width: 220)
                    .onChange(of: selectedAccountID) { _, value in
                        store.settings.selectedAgentAccountID = value
                    }

                    StatusBadge(text: status.headline)
                    StatusBadge(text: account.authMethod.rawValue)
                    Spacer()
                    Button {
                        let account = store.settings.addAgentAccount()
                        selectedAccountID = account.id
                    } label: {
                        Label("Add account", systemImage: "plus")
                    }
                    Button(role: .destructive) {
                        store.settings.deleteAgentAccount(id: selectedAccountID)
                        selectedAccountID = store.settings.selectedAgentAccountID
                    } label: {
                        Label("Delete", systemImage: "trash")
                    }
                    .disabled(store.settings.agentAccounts.count <= 1)
                    Button {
                        Task { await store.refreshProviderStatuses() }
                    } label: {
                        Label("Refresh", systemImage: "arrow.clockwise")
                    }
                }

                accountEditor
                ProviderDetailTable(status: status)

                HStack(spacing: 10) {
                    Button {
                        Task { await store.runAccountLogin(account) }
                    } label: {
                        Label("Run \(account.loginCommand)", systemImage: "play.fill")
                    }

                    Button {
                        Task { await store.openAccountConfig(account) }
                    } label: {
                        Label("Open config", systemImage: "doc")
                    }
                    .disabled(account.configPath.isEmpty)

                    Spacer()
                }

                Text("Authentication method")
                    .font(.headline)

                HStack(spacing: 12) {
                    ProviderAuthMethodCard(title: "CLI", systemImage: "terminal", selected: account.authMethod == .cli)
                    ProviderAuthMethodCard(title: "API key", systemImage: "key", selected: account.authMethod == .apiKey)
                }
            }
        }
        .task {
            selectedAccountID = store.settings.selectedAgentAccountID
            await store.refreshProviderStatuses()
        }
    }

    private var accountEditor: some View {
        Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 10) {
            GridRow {
                Text("Name").foregroundStyle(.secondary)
                TextField("Account name", text: textBinding(\.name))
                    .textFieldStyle(.roundedBorder)
                Text("Provider").foregroundStyle(.secondary)
                Picker("Provider", selection: providerBinding) {
                    ForEach(AgentChatProvider.allCases) { provider in
                        Text(provider.label).tag(provider)
                    }
                }
                .labelsHidden()
            }
            GridRow {
                Text("Plan").foregroundStyle(.secondary)
                TextField("Plan", text: textBinding(\.plan))
                    .textFieldStyle(.roundedBorder)
                Text("Scope").foregroundStyle(.secondary)
                TextField("Scope", text: textBinding(\.scope))
                    .textFieldStyle(.roundedBorder)
            }
            GridRow {
                Text("Org").foregroundStyle(.secondary)
                TextField("Organization", text: textBinding(\.organization))
                    .textFieldStyle(.roundedBorder)
                Text("Auth").foregroundStyle(.secondary)
                Picker("Auth method", selection: authMethodBinding) {
                    ForEach(ProviderAuthMethod.allCases) { method in
                        Text(method.rawValue).tag(method)
                    }
                }
                .labelsHidden()
            }
            GridRow {
                Text("Model").foregroundStyle(.secondary)
                TextField("Default model", text: textBinding(\.defaultModel))
                    .textFieldStyle(.roundedBorder)
                Text("Executable").foregroundStyle(.secondary)
                TextField("Optional executable path", text: textBinding(\.executablePath))
                    .textFieldStyle(.roundedBorder)
            }
            GridRow {
                Text("Config").foregroundStyle(.secondary)
                TextField("Config path", text: textBinding(\.configPath))
                    .textFieldStyle(.roundedBorder)
                Text("Login").foregroundStyle(.secondary)
                TextField("Login command", text: textBinding(\.loginCommand))
                    .textFieldStyle(.roundedBorder)
            }
            GridRow {
                Text("Profile").foregroundStyle(.secondary)
                TextField("Codex profile or account alias", text: textBinding(\.profileName))
                    .textFieldStyle(.roundedBorder)
                Text("Environment").foregroundStyle(.secondary)
                TextField("KEY=value per line", text: textBinding(\.environment), axis: .vertical)
                    .textFieldStyle(.roundedBorder)
                    .lineLimit(2...4)
            }
            GridRow {
                Text("Default").foregroundStyle(.secondary)
                Button(store.settings.selectedAgentAccountID == account.id ? "Workspace default" : "Use in workspace") {
                    store.settings.selectedAgentAccountID = account.id
                }
                .disabled(store.settings.selectedAgentAccountID == account.id)
            }
        }
        .font(.subheadline)
    }

    private var account: AgentAccount {
        AgentAccountCatalog.account(id: selectedAccountID, in: store.settings.agentAccounts)
    }

    private var status: ProviderStatus {
        store.accountStatus(for: account)
    }

    private func textBinding(_ keyPath: WritableKeyPath<AgentAccount, String>) -> Binding<String> {
        Binding(
            get: { account[keyPath: keyPath] },
            set: { value in
                var updated = account
                updated[keyPath: keyPath] = value
                store.settings.updateAgentAccount(updated)
            }
        )
    }

    private var providerBinding: Binding<AgentChatProvider> {
        Binding(
            get: { account.provider },
            set: { value in
                var updated = account
                let oldDefaultConfig = AgentAccount.defaultConfigPath(for: updated.provider)
                let oldDefaultLogin = AgentAccount.defaultLoginCommand(for: updated.provider)
                updated.provider = value
                if updated.configPath.isEmpty || updated.configPath == oldDefaultConfig {
                    updated.configPath = AgentAccount.defaultConfigPath(for: value)
                }
                if updated.loginCommand.isEmpty || updated.loginCommand == oldDefaultLogin {
                    updated.loginCommand = AgentAccount.defaultLoginCommand(for: value)
                }
                updated.defaultModel = value.defaultModel
                store.settings.updateAgentAccount(updated)
            }
        )
    }

    private var authMethodBinding: Binding<ProviderAuthMethod> {
        Binding(
            get: { account.authMethod },
            set: { value in
                var updated = account
                updated.authMethod = value
                store.settings.updateAgentAccount(updated)
            }
        )
    }
}

private struct ProviderDetailTable: View {
    var status: ProviderStatus

    var body: some View {
        Grid(alignment: .leading, horizontalSpacing: 0, verticalSpacing: 0) {
            ForEach(status.detailRows) { row in
                GridRow {
                    Text(row.label)
                        .foregroundStyle(.secondary)
                        .frame(width: 130, alignment: .leading)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 9)
                        .background(Color.secondary.opacity(0.07))
                    Text(row.value)
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 9)
                }
                Divider()
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay {
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.secondary.opacity(0.16))
        }
    }
}

private struct ProviderAuthMethodCard: View {
    var title: String
    var systemImage: String
    var selected: Bool

    var body: some View {
        VStack(spacing: 10) {
            HStack {
                Spacer()
                if selected {
                    Image(systemName: "checkmark")
                        .foregroundStyle(.secondary)
                }
            }
            Image(systemName: systemImage)
                .font(.title2)
                .foregroundStyle(.secondary)
            Text(title)
                .font(.headline)
        }
        .padding(18)
        .frame(maxWidth: .infinity, minHeight: 118)
        .background(selected ? Color.secondary.opacity(0.12) : Color.secondary.opacity(0.04))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay {
            RoundedRectangle(cornerRadius: 8)
                .stroke(selected ? Color.secondary.opacity(0.35) : Color.secondary.opacity(0.16))
        }
    }
}

private struct ConfigEditorSection: Identifiable {
    let id: String
    let title: String
    let subtitle: String?
    let systemImage: String
    let fields: [ConfigFieldDefinition]
}

private struct ConfigFieldDefinition: Identifiable {
    let id: String
    let title: String
    let yamlPath: String
    let jsonPath: [String]
    let kind: ConfigFieldKind
    let placeholder: String

    init(
        _ title: String,
        yamlPath: String,
        jsonPath: [String],
        kind: ConfigFieldKind = .text,
        placeholder: String = ""
    ) {
        id = yamlPath
        self.title = title
        self.yamlPath = yamlPath
        self.jsonPath = jsonPath
        self.kind = kind
        self.placeholder = placeholder
    }
}

private enum ConfigFieldKind: Equatable {
    case text
    case secure
    case boolean
    case integer
    case picker([String])
    case list(ConfigListElement)
}

private enum ConfigListElement: Equatable {
    case string
    case integer
}

private enum ConfigSectionCatalog {
    static let core: [ConfigEditorSection] = [
        .init(
            id: "gateway",
            title: "Gateway",
            subtitle: "Daemon HTTP/WebSocket endpoint and local runtime bridge.",
            systemImage: "network",
            fields: [
                .init("Host", yamlPath: "gateway.host", jsonPath: ["Gateway", "Host"], placeholder: "127.0.0.1"),
                .init("Port", yamlPath: "gateway.port", jsonPath: ["Gateway", "Port"], kind: .integer, placeholder: "9090"),
                .init("Codex command", yamlPath: "gateway.codex_runtime.command", jsonPath: ["Gateway", "CodexRuntime", "Command"], placeholder: "codex"),
                .init("Runtime model", yamlPath: "gateway.codex_runtime.model", jsonPath: ["Gateway", "CodexRuntime", "Model"], placeholder: "optional"),
                .init("Runtime sandbox", yamlPath: "gateway.codex_runtime.sandbox", jsonPath: ["Gateway", "CodexRuntime", "Sandbox"], kind: .picker(["read-only", "workspace-write", "danger-full-access"]), placeholder: "read-only")
            ]
        ),
        .init(
            id: "auth",
            title: "Auth",
            subtitle: "Local Claude Code auth or remote API token mode.",
            systemImage: "lock",
            fields: [
                .init("Type", yamlPath: "auth.type", jsonPath: ["Auth", "Type"], kind: .picker(["claude-code", "api-token"]), placeholder: "claude-code"),
                .init("Token", yamlPath: "auth.token", jsonPath: ["Auth", "Token"], kind: .secure, placeholder: "only for api-token")
            ]
        ),
        .init(
            id: "execution",
            title: "Execution Backend",
            subtitle: "Runnable backend used by task execution and workspace handoffs.",
            systemImage: "terminal",
            fields: [
                .init("Backend", yamlPath: "executor.type", jsonPath: ["Executor", "Type"], kind: .picker(["claude-code", "codex-exec", "opencode", "anthropic-api", "openai-api"]), placeholder: "codex-exec"),
                .init("Auto PR", yamlPath: "executor.auto_create_pr", jsonPath: ["Executor", "AutoCreatePR"], kind: .boolean),
                .init("Sub issues", yamlPath: "executor.create_sub_issues", jsonPath: ["Executor", "CreateSubIssues"], kind: .boolean),
                .init("Skip self-review", yamlPath: "executor.skip_self_review", jsonPath: ["Executor", "SkipSelfReview"], kind: .boolean),
                .init("Worktrees", yamlPath: "executor.use_worktree", jsonPath: ["Executor", "UseWorktree"], kind: .boolean),
                .init("Default model", yamlPath: "executor.default_model", jsonPath: ["Executor", "DefaultModel"], placeholder: "optional"),
                .init("API base URL", yamlPath: "executor.api_base_url", jsonPath: ["Executor", "APIBaseURL"], placeholder: "optional"),
                .init("API token", yamlPath: "executor.api_auth_token", jsonPath: ["Executor", "APIAuthToken"], kind: .secure, placeholder: "${TOKEN}")
            ]
        ),
        .init(
            id: "codex-exec",
            title: "Codex",
            subtitle: "Codex CLI task backend.",
            systemImage: "sparkles",
            fields: [
                .init("Command", yamlPath: "executor.codex_exec.command", jsonPath: ["Executor", "CodexExec", "Command"], placeholder: "codex"),
                .init("Model", yamlPath: "executor.codex_exec.model", jsonPath: ["Executor", "CodexExec", "Model"], placeholder: "optional"),
                .init("Effort", yamlPath: "executor.codex_exec.effort", jsonPath: ["Executor", "CodexExec", "Effort"], kind: .picker(["", "low", "medium", "high", "max"])),
                .init("Sandbox", yamlPath: "executor.codex_exec.sandbox", jsonPath: ["Executor", "CodexExec", "Sandbox"], kind: .picker(["read-only", "workspace-write", "danger-full-access"])),
                .init("Resume", yamlPath: "executor.codex_exec.use_session_resume", jsonPath: ["Executor", "CodexExec", "UseSessionResume"], kind: .boolean)
            ]
        ),
        .init(
            id: "opencode",
            title: "OpenCode",
            subtitle: "OpenCode server backend.",
            systemImage: "chevron.left.forwardslash.chevron.right",
            fields: [
                .init("Server URL", yamlPath: "executor.opencode.server_url", jsonPath: ["Executor", "OpenCode", "ServerURL"], placeholder: "http://127.0.0.1:4096"),
                .init("Model", yamlPath: "executor.opencode.model", jsonPath: ["Executor", "OpenCode", "Model"], placeholder: "anthropic/claude-sonnet-4-6"),
                .init("Provider", yamlPath: "executor.opencode.provider", jsonPath: ["Executor", "OpenCode", "Provider"], placeholder: "anthropic"),
                .init("Auto start", yamlPath: "executor.opencode.auto_start_server", jsonPath: ["Executor", "OpenCode", "AutoStartServer"], kind: .boolean),
                .init("Command", yamlPath: "executor.opencode.server_command", jsonPath: ["Executor", "OpenCode", "ServerCommand"], placeholder: "opencode serve")
            ]
        )
    ]

    static let projects: [ConfigEditorSection] = [
        .init(
            id: "project",
            title: "Default Project",
            subtitle: "Same project setup collected by onboard.",
            systemImage: "folder",
            fields: [
                .init("Default", yamlPath: "default_project", jsonPath: ["DefaultProject"], placeholder: "project name"),
                .init("Name", yamlPath: "projects.0.name", jsonPath: ["Projects", "0", "Name"], placeholder: "pilot"),
                .init("Path", yamlPath: "projects.0.path", jsonPath: ["Projects", "0", "Path"], placeholder: "/path/to/repo"),
                .init("Branch", yamlPath: "projects.0.default_branch", jsonPath: ["Projects", "0", "DefaultBranch"], placeholder: "dev"),
                .init("Navigator", yamlPath: "projects.0.navigator", jsonPath: ["Projects", "0", "Navigator"], kind: .boolean),
                .init("GH owner", yamlPath: "projects.0.github.owner", jsonPath: ["Projects", "0", "GitHub", "Owner"], placeholder: "ylcn91"),
                .init("GH repo", yamlPath: "projects.0.github.repo", jsonPath: ["Projects", "0", "GitHub", "Repo"], placeholder: "pilot"),
                .init("Linear project", yamlPath: "projects.0.linear.project_id", jsonPath: ["Projects", "0", "Linear", "ProjectID"], placeholder: "optional")
            ]
        )
    ]

    static let ticketSources: [ConfigEditorSection] = [
        .init(
            id: "github",
            title: "GitHub",
            subtitle: "Issue polling, PR creation, project boards and webhook intake.",
            systemImage: "chevron.left.forwardslash.chevron.right",
            fields: [
                .init("Enabled", yamlPath: "adapters.github.enabled", jsonPath: ["Adapters", "GitHub", "Enabled"], kind: .boolean),
                .init("Token", yamlPath: "adapters.github.token", jsonPath: ["Adapters", "GitHub", "Token"], kind: .secure, placeholder: "${GITHUB_TOKEN}"),
                .init("Repo", yamlPath: "adapters.github.repo", jsonPath: ["Adapters", "GitHub", "Repo"], placeholder: "owner/repo"),
                .init("Project path", yamlPath: "adapters.github.project_path", jsonPath: ["Adapters", "GitHub", "ProjectPath"], placeholder: "/path/to/repo"),
                .init("Label", yamlPath: "adapters.github.pilot_label", jsonPath: ["Adapters", "GitHub", "PilotLabel"], placeholder: "pilot"),
                .init("Webhook", yamlPath: "adapters.github.webhook_secret", jsonPath: ["Adapters", "GitHub", "WebhookSecret"], kind: .secure),
                .init("Polling", yamlPath: "adapters.github.polling.enabled", jsonPath: ["Adapters", "GitHub", "Polling", "Enabled"], kind: .boolean)
            ]
        ),
        .init(
            id: "gitlab",
            title: "GitLab",
            subtitle: "Issue polling, MR creation and webhook intake.",
            systemImage: "shippingbox",
            fields: [
                .init("Enabled", yamlPath: "adapters.gitlab.enabled", jsonPath: ["Adapters", "GitLab", "Enabled"], kind: .boolean),
                .init("Token", yamlPath: "adapters.gitlab.token", jsonPath: ["Adapters", "GitLab", "Token"], kind: .secure, placeholder: "${GITLAB_TOKEN}"),
                .init("Project", yamlPath: "adapters.gitlab.project", jsonPath: ["Adapters", "GitLab", "Project"], placeholder: "namespace/project"),
                .init("Base URL", yamlPath: "adapters.gitlab.base_url", jsonPath: ["Adapters", "GitLab", "BaseURL"], placeholder: "https://gitlab.com"),
                .init("Label", yamlPath: "adapters.gitlab.pilot_label", jsonPath: ["Adapters", "GitLab", "PilotLabel"], placeholder: "pilot"),
                .init("Webhook", yamlPath: "adapters.gitlab.webhook_secret", jsonPath: ["Adapters", "GitLab", "WebhookSecret"], kind: .secure),
                .init("Polling", yamlPath: "adapters.gitlab.polling.enabled", jsonPath: ["Adapters", "GitLab", "Polling", "Enabled"], kind: .boolean)
            ]
        ),
        .init(
            id: "bitbucket",
            title: "Bitbucket",
            subtitle: "Issue polling and PR creation.",
            systemImage: "bitcoinsign.circle",
            fields: [
                .init("Enabled", yamlPath: "adapters.bitbucket.enabled", jsonPath: ["Adapters", "Bitbucket", "Enabled"], kind: .boolean),
                .init("Token", yamlPath: "adapters.bitbucket.token", jsonPath: ["Adapters", "Bitbucket", "Token"], kind: .secure, placeholder: "${BITBUCKET_TOKEN}"),
                .init("Username", yamlPath: "adapters.bitbucket.username", jsonPath: ["Adapters", "Bitbucket", "Username"]),
                .init("Workspace", yamlPath: "adapters.bitbucket.workspace", jsonPath: ["Adapters", "Bitbucket", "Workspace"]),
                .init("Repo", yamlPath: "adapters.bitbucket.repo", jsonPath: ["Adapters", "Bitbucket", "Repo"]),
                .init("Base URL", yamlPath: "adapters.bitbucket.base_url", jsonPath: ["Adapters", "Bitbucket", "BaseURL"], placeholder: "https://api.bitbucket.org/2.0"),
                .init("Label", yamlPath: "adapters.bitbucket.pilot_label", jsonPath: ["Adapters", "Bitbucket", "PilotLabel"], placeholder: "pilot"),
                .init("Webhook", yamlPath: "adapters.bitbucket.webhook_secret", jsonPath: ["Adapters", "Bitbucket", "WebhookSecret"], kind: .secure)
            ]
        ),
        .init(
            id: "azure-devops",
            title: "Azure DevOps",
            subtitle: "Work item polling and PR creation.",
            systemImage: "cloud",
            fields: [
                .init("Enabled", yamlPath: "adapters.azure_devops.enabled", jsonPath: ["Adapters", "AzureDevOps", "Enabled"], kind: .boolean),
                .init("PAT", yamlPath: "adapters.azure_devops.pat", jsonPath: ["Adapters", "AzureDevOps", "PAT"], kind: .secure, placeholder: "${AZURE_DEVOPS_PAT}"),
                .init("Org", yamlPath: "adapters.azure_devops.organization", jsonPath: ["Adapters", "AzureDevOps", "Organization"]),
                .init("Project", yamlPath: "adapters.azure_devops.project", jsonPath: ["Adapters", "AzureDevOps", "Project"]),
                .init("Repo", yamlPath: "adapters.azure_devops.repository", jsonPath: ["Adapters", "AzureDevOps", "Repository"]),
                .init("Base URL", yamlPath: "adapters.azure_devops.base_url", jsonPath: ["Adapters", "AzureDevOps", "BaseURL"], placeholder: "https://dev.azure.com"),
                .init("Tag", yamlPath: "adapters.azure_devops.pilot_tag", jsonPath: ["Adapters", "AzureDevOps", "PilotTag"], placeholder: "pilot"),
                .init("Types", yamlPath: "adapters.azure_devops.work_item_types", jsonPath: ["Adapters", "AzureDevOps", "WorkItemTypes"], kind: .list(.string), placeholder: "Bug, Task, User Story"),
                .init("Webhook", yamlPath: "adapters.azure_devops.webhook_secret", jsonPath: ["Adapters", "AzureDevOps", "WebhookSecret"], kind: .secure)
            ]
        ),
        .init(
            id: "jira",
            title: "Jira",
            subtitle: "Jira issue polling and transition wiring.",
            systemImage: "square.stack.3d.up",
            fields: [
                .init("Enabled", yamlPath: "adapters.jira.enabled", jsonPath: ["Adapters", "Jira", "Enabled"], kind: .boolean),
                .init("Platform", yamlPath: "adapters.jira.platform", jsonPath: ["Adapters", "Jira", "Platform"], kind: .picker(["cloud", "server"])),
                .init("Base URL", yamlPath: "adapters.jira.base_url", jsonPath: ["Adapters", "Jira", "BaseURL"], placeholder: "https://company.atlassian.net"),
                .init("User", yamlPath: "adapters.jira.username", jsonPath: ["Adapters", "Jira", "Username"]),
                .init("API token", yamlPath: "adapters.jira.api_token", jsonPath: ["Adapters", "Jira", "APIToken"], kind: .secure),
                .init("Project", yamlPath: "adapters.jira.project_key", jsonPath: ["Adapters", "Jira", "ProjectKey"], placeholder: "PROJ"),
                .init("Label", yamlPath: "adapters.jira.pilot_label", jsonPath: ["Adapters", "Jira", "PilotLabel"], placeholder: "pilot"),
                .init("Webhook", yamlPath: "adapters.jira.webhook_secret", jsonPath: ["Adapters", "Jira", "WebhookSecret"], kind: .secure)
            ]
        ),
        .init(
            id: "linear",
            title: "Linear",
            subtitle: "Linear issue source and webhook verification.",
            systemImage: "line.3.horizontal.decrease.circle",
            fields: [
                .init("Enabled", yamlPath: "adapters.linear.enabled", jsonPath: ["Adapters", "Linear", "Enabled"], kind: .boolean),
                .init("API key", yamlPath: "adapters.linear.api_key", jsonPath: ["Adapters", "Linear", "APIKey"], kind: .secure, placeholder: "${LINEAR_API_KEY}"),
                .init("Team ID", yamlPath: "adapters.linear.team_id", jsonPath: ["Adapters", "Linear", "TeamID"]),
                .init("Label", yamlPath: "adapters.linear.pilot_label", jsonPath: ["Adapters", "Linear", "PilotLabel"], placeholder: "pilot"),
                .init("Auto assign", yamlPath: "adapters.linear.auto_assign", jsonPath: ["Adapters", "Linear", "AutoAssign"], kind: .boolean),
                .init("Projects", yamlPath: "adapters.linear.project_ids", jsonPath: ["Adapters", "Linear", "ProjectIDs"], kind: .list(.string)),
                .init("Webhook key", yamlPath: "adapters.linear.webhook_public_key", jsonPath: ["Adapters", "Linear", "WebhookPublicKey"], kind: .secure)
            ]
        ),
        .init(
            id: "plane",
            title: "Plane",
            subtitle: "Plane.so polling and webhook intake.",
            systemImage: "airplane",
            fields: [
                .init("Enabled", yamlPath: "adapters.plane.enabled", jsonPath: ["Adapters", "Plane", "Enabled"], kind: .boolean),
                .init("Base URL", yamlPath: "adapters.plane.base_url", jsonPath: ["Adapters", "Plane", "BaseURL"], placeholder: "https://api.plane.so"),
                .init("API key", yamlPath: "adapters.plane.api_key", jsonPath: ["Adapters", "Plane", "APIKey"], kind: .secure, placeholder: "${PLANE_API_KEY}"),
                .init("Workspace", yamlPath: "adapters.plane.workspace_slug", jsonPath: ["Adapters", "Plane", "WorkspaceSlug"]),
                .init("Projects", yamlPath: "adapters.plane.project_ids", jsonPath: ["Adapters", "Plane", "ProjectIDs"], kind: .list(.string)),
                .init("Label", yamlPath: "adapters.plane.pilot_label", jsonPath: ["Adapters", "Plane", "PilotLabel"], placeholder: "pilot"),
                .init("Webhook", yamlPath: "adapters.plane.webhook_secret", jsonPath: ["Adapters", "Plane", "WebhookSecret"], kind: .secure)
            ]
        ),
        .init(
            id: "asana",
            title: "Asana",
            subtitle: "Asana task source and webhook intake.",
            systemImage: "circle.grid.3x3",
            fields: [
                .init("Enabled", yamlPath: "adapters.asana.enabled", jsonPath: ["Adapters", "Asana", "Enabled"], kind: .boolean),
                .init("Token", yamlPath: "adapters.asana.access_token", jsonPath: ["Adapters", "Asana", "AccessToken"], kind: .secure, placeholder: "${ASANA_ACCESS_TOKEN}"),
                .init("Workspace", yamlPath: "adapters.asana.workspace_id", jsonPath: ["Adapters", "Asana", "WorkspaceID"]),
                .init("Tag", yamlPath: "adapters.asana.pilot_tag", jsonPath: ["Adapters", "Asana", "PilotTag"], placeholder: "pilot"),
                .init("Webhook", yamlPath: "adapters.asana.webhook_secret", jsonPath: ["Adapters", "Asana", "WebhookSecret"], kind: .secure)
            ]
        )
    ]

    static let notifications: [ConfigEditorSection] = [
        .init(
            id: "slack",
            title: "Slack",
            subtitle: "Socket Mode, notifications and approval channel.",
            systemImage: "bubble.left.and.bubble.right",
            fields: [
                .init("Enabled", yamlPath: "adapters.slack.enabled", jsonPath: ["Adapters", "Slack", "Enabled"], kind: .boolean),
                .init("Bot token", yamlPath: "adapters.slack.bot_token", jsonPath: ["Adapters", "Slack", "BotToken"], kind: .secure, placeholder: "${SLACK_BOT_TOKEN}"),
                .init("App token", yamlPath: "adapters.slack.app_token", jsonPath: ["Adapters", "Slack", "AppToken"], kind: .secure, placeholder: "${SLACK_APP_TOKEN}"),
                .init("Channel", yamlPath: "adapters.slack.channel", jsonPath: ["Adapters", "Slack", "Channel"], placeholder: "#dev-notifications"),
                .init("Signing", yamlPath: "adapters.slack.signing_secret", jsonPath: ["Adapters", "Slack", "SigningSecret"], kind: .secure),
                .init("Socket mode", yamlPath: "adapters.slack.socket_mode", jsonPath: ["Adapters", "Slack", "SocketMode"], kind: .boolean),
                .init("Users", yamlPath: "adapters.slack.allowed_users", jsonPath: ["Adapters", "Slack", "AllowedUsers"], kind: .list(.string)),
                .init("Channels", yamlPath: "adapters.slack.allowed_channels", jsonPath: ["Adapters", "Slack", "AllowedChannels"], kind: .list(.string))
            ]
        ),
        .init(
            id: "telegram",
            title: "Telegram",
            subtitle: "Bot polling, chat ID and allowed users.",
            systemImage: "paperplane",
            fields: [
                .init("Enabled", yamlPath: "adapters.telegram.enabled", jsonPath: ["Adapters", "Telegram", "Enabled"], kind: .boolean),
                .init("Bot token", yamlPath: "adapters.telegram.bot_token", jsonPath: ["Adapters", "Telegram", "BotToken"], kind: .secure, placeholder: "${TELEGRAM_BOT_TOKEN}"),
                .init("Chat ID", yamlPath: "adapters.telegram.chat_id", jsonPath: ["Adapters", "Telegram", "ChatID"], placeholder: "${TELEGRAM_CHAT_ID}"),
                .init("Polling", yamlPath: "adapters.telegram.polling", jsonPath: ["Adapters", "Telegram", "Polling"], kind: .boolean),
                .init("Plain text", yamlPath: "adapters.telegram.plain_text_mode", jsonPath: ["Adapters", "Telegram", "PlainTextMode"], kind: .boolean),
                .init("Allowed IDs", yamlPath: "adapters.telegram.allowed_ids", jsonPath: ["Adapters", "Telegram", "AllowedIDs"], kind: .list(.integer))
            ]
        ),
        .init(
            id: "discord",
            title: "Discord",
            subtitle: "Discord bot adapter and allowlists.",
            systemImage: "gamecontroller",
            fields: [
                .init("Enabled", yamlPath: "adapters.discord.enabled", jsonPath: ["Adapters", "Discord", "Enabled"], kind: .boolean),
                .init("Bot token", yamlPath: "adapters.discord.bot_token", jsonPath: ["Adapters", "Discord", "BotToken"], kind: .secure, placeholder: "${DISCORD_BOT_TOKEN}"),
                .init("Bot ID", yamlPath: "adapters.discord.bot_id", jsonPath: ["Adapters", "Discord", "BotID"]),
                .init("Prefix", yamlPath: "adapters.discord.command_prefix", jsonPath: ["Adapters", "Discord", "CommandPrefix"], placeholder: "/"),
                .init("Guilds", yamlPath: "adapters.discord.allowed_guilds", jsonPath: ["Adapters", "Discord", "AllowedGuilds"], kind: .list(.string)),
                .init("Channels", yamlPath: "adapters.discord.allowed_channels", jsonPath: ["Adapters", "Discord", "AllowedChannels"], kind: .list(.string))
            ]
        )
    ]

    static let automation: [ConfigEditorSection] = [
        .init(
            id: "orchestrator",
            title: "Orchestrator",
            subtitle: "Task concurrency and execution mode.",
            systemImage: "slider.horizontal.3",
            fields: [
                .init("Model", yamlPath: "orchestrator.model", jsonPath: ["Orchestrator", "Model"], placeholder: "claude-sonnet-4-6"),
                .init("Concurrent", yamlPath: "orchestrator.max_concurrent", jsonPath: ["Orchestrator", "MaxConcurrent"], kind: .integer, placeholder: "2"),
                .init("Mode", yamlPath: "orchestrator.execution.mode", jsonPath: ["Orchestrator", "Execution", "Mode"], kind: .picker(["auto", "sequential", "parallel"])),
                .init("Wait merge", yamlPath: "orchestrator.execution.wait_for_merge", jsonPath: ["Orchestrator", "Execution", "WaitForMerge"], kind: .boolean)
            ]
        ),
        .init(
            id: "autopilot-config",
            title: "Autopilot",
            subtitle: "PR lifecycle, CI and merge behavior.",
            systemImage: "arrow.triangle.2.circlepath",
            fields: [
                .init("Enabled", yamlPath: "orchestrator.autopilot.enabled", jsonPath: ["Orchestrator", "Autopilot", "Enabled"], kind: .boolean),
                .init("Environment", yamlPath: "orchestrator.autopilot.environment", jsonPath: ["Orchestrator", "Autopilot", "Environment"], placeholder: "stage"),
                .init("Auto merge", yamlPath: "orchestrator.autopilot.auto_merge", jsonPath: ["Orchestrator", "Autopilot", "AutoMerge"], kind: .boolean),
                .init("Merge method", yamlPath: "orchestrator.autopilot.merge_method", jsonPath: ["Orchestrator", "Autopilot", "MergeMethod"], kind: .picker(["squash", "merge", "rebase"])),
                .init("Approval", yamlPath: "orchestrator.autopilot.approval_source", jsonPath: ["Orchestrator", "Autopilot", "ApprovalSource"], kind: .picker(["telegram", "slack", "github-review"]))
            ]
        ),
        .init(
            id: "architect-config",
            title: "Architect",
            subtitle: "Proactive scan and refactor proposal pipeline.",
            systemImage: "scope",
            fields: [
                .init("Enabled", yamlPath: "architect.enabled", jsonPath: ["Architect", "Enabled"], kind: .boolean),
                .init("Schedule", yamlPath: "architect.schedule", jsonPath: ["Architect", "Schedule"], placeholder: "0 8 * * 1"),
                .init("Timezone", yamlPath: "architect.timezone", jsonPath: ["Architect", "Timezone"], placeholder: "America/New_York"),
                .init("Max tickets", yamlPath: "architect.max_tickets", jsonPath: ["Architect", "MaxTickets"], kind: .integer, placeholder: "10"),
                .init("Backend", yamlPath: "architect.backend.type", jsonPath: ["Architect", "Backend", "Type"], kind: .picker(["", "claude-code", "codex-exec", "opencode", "anthropic-api", "openai-api"]))
            ]
        ),
        .init(
            id: "budget",
            title: "Budget",
            subtitle: "Cost controls enforced by Pilot.",
            systemImage: "dollarsign.circle",
            fields: [
                .init("Enabled", yamlPath: "budget.enabled", jsonPath: ["Budget", "Enabled"], kind: .boolean),
                .init("Daily", yamlPath: "budget.daily_limit", jsonPath: ["Budget", "DailyLimit"], kind: .integer, placeholder: "50"),
                .init("Monthly", yamlPath: "budget.monthly_limit", jsonPath: ["Budget", "MonthlyLimit"], kind: .integer, placeholder: "500"),
                .init("Warn pct", yamlPath: "budget.thresholds.warn_percent", jsonPath: ["Budget", "Thresholds", "WarnPercent"], kind: .integer, placeholder: "80")
            ]
        ),
        .init(
            id: "memory",
            title: "Memory",
            subtitle: "SQLite store and pattern learning.",
            systemImage: "brain",
            fields: [
                .init("Path", yamlPath: "memory.path", jsonPath: ["Memory", "Path"], placeholder: "~/.pilot/data"),
                .init("Cross project", yamlPath: "memory.cross_project", jsonPath: ["Memory", "CrossProject"], kind: .boolean),
                .init("Learning", yamlPath: "memory.learning.enabled", jsonPath: ["Memory", "Learning", "Enabled"], kind: .boolean),
                .init("Sync files", yamlPath: "memory.sync_to_files", jsonPath: ["Memory", "SyncToFiles"], kind: .boolean)
            ]
        )
    ]

    static var allSections: [ConfigEditorSection] {
        core + projects + ticketSources + notifications + automation
    }

    static var allFields: [ConfigFieldDefinition] {
        allSections.flatMap(\.fields)
    }
}
