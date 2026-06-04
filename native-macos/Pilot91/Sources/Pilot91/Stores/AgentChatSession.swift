import Foundation

@MainActor
final class AgentChatSession: ObservableObject {
    @Published var messages: [AgentChatMessage] = [
        AgentChatMessage(role: .system, provider: nil, model: nil, body: "New workspace chat")
    ]
    @Published var isRunning = false
    @Published var lastError: String?

    func send(
        prompt: String,
        account: AgentAccount,
        model: String,
        cwd: String,
        sandbox: RuntimeSandbox,
        pilotCommand: String,
        onOutput: ((String) -> Void)? = nil
    ) async {
        let trimmed = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, !isRunning else { return }

        messages.append(AgentChatMessage(role: .user, provider: account.provider, model: model, body: trimmed))
        let assistantID = UUID()
        messages.append(AgentChatMessage(
            id: assistantID,
            role: .assistant,
            provider: account.provider,
            model: model,
            body: "",
            isRunning: true
        ))

        isRunning = true
        lastError = nil

        let run = await ShellCommand(workingDirectory: cwd).run(
            command(account: account, model: model, cwd: cwd, sandbox: sandbox, pilotCommand: pilotCommand),
            title: account.name,
            input: trimmed,
            includeStderr: false,
            onOutput: { [weak self] output in
                Task { @MainActor in
                    onOutput?(output)
                    self?.updateAssistant(id: assistantID, body: output.trimmingCharacters(in: .whitespacesAndNewlines))
                }
            }
        )

        isRunning = false
        let response = run.output.trimmingCharacters(in: .whitespacesAndNewlines)
        let stderr = run.stderr.trimmingCharacters(in: .whitespacesAndNewlines)
        onOutput?(response)
        if let index = messages.firstIndex(where: { $0.id == assistantID }) {
            messages[index].body = response.isEmpty ? "(no response)" : response
            messages[index].isRunning = false
            messages[index].exitCode = run.exitCode
        }
        if run.exitCode != 0 {
            if !stderr.isEmpty {
                lastError = stderr
            } else {
                lastError = response.isEmpty ? "\(account.name) exited \(run.exitCode ?? -1)" : response
            }
        }
    }

    func clear() {
        messages = [
            AgentChatMessage(role: .system, provider: nil, model: nil, body: "New workspace chat")
        ]
        lastError = nil
    }

    func beginRun(prompt: String, provider: AgentChatProvider?, model: String?) {
        let trimmed = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, !isRunning else { return }

        messages.append(AgentChatMessage(role: .user, provider: provider, model: model, body: trimmed))
        messages.append(AgentChatMessage(
            role: .assistant,
            provider: provider,
            model: model,
            body: "",
            isRunning: true
        ))
        isRunning = true
        lastError = nil
    }

    func finishRun(body: String, exitCode: Int32?) {
        guard let index = messages.lastIndex(where: { $0.role == .assistant && $0.isRunning }) else {
            isRunning = false
            return
        }
        messages[index].body = body
        messages[index].isRunning = false
        messages[index].exitCode = exitCode
        isRunning = false
        if let exitCode, exitCode != 0 {
            lastError = body
        }
    }

    func updateRunningAssistant(body: String) {
        guard let index = messages.lastIndex(where: { $0.role == .assistant && $0.isRunning }) else { return }
        messages[index].body = body
    }

    private func updateAssistant(id: UUID, body: String) {
        guard let index = messages.firstIndex(where: { $0.id == id }) else { return }
        messages[index].body = body
    }

    private func command(account: AgentAccount, model: String, cwd: String, sandbox: RuntimeSandbox, pilotCommand: String) -> String {
        switch account.provider {
        case .codex:
            return codexAppServerCommand(account: account, model: model, cwd: cwd, sandbox: sandbox, pilotCommand: pilotCommand)
        case .claudeCode:
            return claudeCommand(account: account, model: model)
        }
    }

    private func codexAppServerCommand(account: AgentAccount, model: String, cwd: String, sandbox: RuntimeSandbox, pilotCommand: String) -> String {
        let modelArg = model.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "" : " --model \(shellQuote(model))"
        let profileArg = codexProfileName(for: account).map { " --profile \(shellQuote($0))" } ?? ""
        let envFlags = pilotChatEnvFlags(account: account, extra: codexConfigEnvironment(for: account))
        let explicitExecutable = expandedPath(account.executablePath)
        return """
        prompt="$(cat)"; \
        codex_bin=\(explicitExecutable.isEmpty ? "\"$(command -v codex || true)\"" : shellQuote(explicitExecutable)); \
        if [ -z "$codex_bin" ]; then codex_bin="/Applications/Codex.app/Contents/Resources/codex"; fi; \
        if [ ! -x "$codex_bin" ]; then codex_bin="$HOME/Library/Application Support/com.conductor.app/bin/codex"; fi; \
        if [ ! -x "$codex_bin" ]; then codex_bin="$(find "$HOME/Library/Application Support/com.conductor.app/agent-binaries/codex" "$HOME/Library/Developer/Xcode/CodingAssistant/Agents/codex" -type f -name codex -perm -111 2>/dev/null | sort -r | head -n 1)"; fi; \
        if [ -z "$codex_bin" ] || [ ! -x "$codex_bin" ]; then echo "Codex CLI not found"; exit 127; fi; \
        printf "%s" "$prompt" | \(pilotCommand) chat --command "$codex_bin" --cwd \(shellQuote(cwd)) --sandbox \(shellQuote(sandbox.rawValue))\(profileArg)\(envFlags)\(modelArg)
        """
    }

    private func claudeCommand(account: AgentAccount, model: String) -> String {
        let modelArg = normalizedClaudeModel(model).isEmpty ? "" : " --model \(shellQuote(normalizedClaudeModel(model)))"
        let settingsArg = claudeSettingsArg(for: account)
        let envPrefix = shellEnvironmentPrefix(account: account)
        let explicitExecutable = expandedPath(account.executablePath)
        return """
        prompt="$(cat)"; \
        claude_bin=\(explicitExecutable.isEmpty ? "\"$(command -v claude || true)\"" : shellQuote(explicitExecutable)); \
        claude_code_bin="$(command -v claude-code || true)"; \
        if [ -n "$claude_bin" ] && { [ ! -f "$claude_bin" ] || [ ! -x "$claude_bin" ]; }; then claude_bin=""; fi; \
        if [ -z "$claude_bin" ] && [ -f "$HOME/.local/bin/claude" ] && [ -x "$HOME/.local/bin/claude" ]; then claude_bin="$HOME/.local/bin/claude"; fi; \
        if [ -n "$claude_bin" ]; then \(envPrefix)"$claude_bin" -p\(modelArg)\(settingsArg) "$prompt"; \
        elif [ -n "$claude_code_bin" ]; then \(envPrefix)"$claude_code_bin" -p\(modelArg)\(settingsArg) "$prompt"; \
        else claude_bin="$(find "$HOME/Library/Application Support/Claude/claude-code" "$HOME/Library/Application Support/Claude/claude-code-vm" "$HOME/Library/Application Support/com.conductor.app/agent-binaries/claude" "$HOME/Library/Developer/Xcode/CodingAssistant/Agents/claude" -type f -name claude -perm -111 2>/dev/null | sort -r | head -n 1)"; [ -n "$claude_bin" ] && \(envPrefix)"$claude_bin" -p\(modelArg)\(settingsArg) "$prompt" || { echo "Claude Code CLI not found"; exit 127; }; \
        fi
        """
    }

    private func claudeSettingsArg(for account: AgentAccount) -> String {
        let path = expandedPath(account.configPath)
        guard !path.isEmpty, FileManager.default.fileExists(atPath: path) else { return "" }
        return " --settings \(shellQuote(path))"
    }

    private func codexProfileName(for account: AgentAccount) -> String? {
        let explicit = account.profileName.trimmingCharacters(in: .whitespacesAndNewlines)
        if !explicit.isEmpty {
            return explicit
        }
        let path = expandedPath(account.configPath)
        let last = URL(fileURLWithPath: path).lastPathComponent
        guard last.hasSuffix(".config.toml"), last != "config.toml" else { return nil }
        return String(last.dropLast(".config.toml".count))
    }

    private func codexConfigEnvironment(for account: AgentAccount) -> [(String, String)] {
        let path = expandedPath(account.configPath)
        guard !path.isEmpty else { return [] }
        let dir = URL(fileURLWithPath: path).deletingLastPathComponent().path
        return [("CODEX_HOME", dir)]
    }

    private func pilotChatEnvFlags(account: AgentAccount, extra: [(String, String)] = []) -> String {
        environmentPairs(account: account, extra: extra)
            .map { " --env \(shellQuote("\($0.0)=\($0.1)"))" }
            .joined()
    }

    private func shellEnvironmentPrefix(account: AgentAccount, extra: [(String, String)] = []) -> String {
        let pairs = environmentPairs(account: account, extra: extra)
        guard !pairs.isEmpty else { return "" }
        let assignments = pairs.map { "\($0.0)=\(shellQuote($0.1))" }.joined(separator: " ")
        return "env \(assignments) "
    }

    private func environmentPairs(account: AgentAccount, extra: [(String, String)] = []) -> [(String, String)] {
        let userPairs = parseEnvironment(account.environment)
        let userKeys = Set(userPairs.map(\.0))
        let autoPairs = extra.filter { !userKeys.contains($0.0) && isValidEnvKey($0.0) }
        return autoPairs + userPairs
    }

    private func parseEnvironment(_ value: String) -> [(String, String)] {
        value
            .split(whereSeparator: \.isNewline)
            .compactMap { rawLine -> (String, String)? in
                var line = rawLine.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !line.isEmpty, !line.hasPrefix("#") else { return nil }
                if line.hasPrefix("export ") {
                    line.removeFirst("export ".count)
                    line = line.trimmingCharacters(in: .whitespacesAndNewlines)
                }
                guard let separator = line.firstIndex(of: "=") else { return nil }
                let key = String(line[..<separator]).trimmingCharacters(in: .whitespacesAndNewlines)
                let value = expandedPath(String(line[line.index(after: separator)...]))
                guard isValidEnvKey(key) else { return nil }
                return (key, value)
            }
    }

    private func isValidEnvKey(_ key: String) -> Bool {
        guard let first = key.unicodeScalars.first else { return false }
        guard first == "_" || CharacterSet.letters.contains(first) else { return false }
        return key.unicodeScalars.allSatisfy { scalar in
            scalar == "_" || CharacterSet.alphanumerics.contains(scalar)
        }
    }

    private func expandedPath(_ value: String) -> String {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.hasPrefix("~/") {
            return FileManager.default.homeDirectoryForCurrentUser.path + String(trimmed.dropFirst())
        }
        return trimmed
    }

    private func normalizedClaudeModel(_ model: String) -> String {
        let value = model.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if value.isEmpty {
            return ""
        }
        if value == "opus" || value.hasPrefix("claude-opus-") || value.hasPrefix("opus-") {
            return value.hasPrefix("claude-opus-") ? value : "opus"
        }
        if value == "sonnet" || value.hasPrefix("claude-sonnet-") || value.hasPrefix("sonnet-") {
            return value.hasPrefix("claude-sonnet-") ? value : "sonnet"
        }
        if value == "haiku" || value.hasPrefix("claude-haiku-") || value.hasPrefix("haiku-") {
            return value.hasPrefix("claude-haiku-") ? value : "haiku"
        }
        return value
    }
}
