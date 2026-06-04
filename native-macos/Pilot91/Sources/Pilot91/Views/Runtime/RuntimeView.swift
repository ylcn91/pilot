import SwiftUI

struct RuntimeView: View {
    @EnvironmentObject private var store: AppStore
    @State private var prompt = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            header

            HStack(alignment: .top, spacing: 12) {
                conversationPanel
                    .frame(minWidth: 520, maxWidth: .infinity, maxHeight: .infinity)
                    .layoutPriority(1)

                VStack(spacing: 12) {
                    approvalsPanel
                    sessionPanel
                }
                .frame(width: 360, alignment: .top)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)

            composer
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .center, spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Agent Runtime")
                        .font(.title2.weight(.semibold))
                    Text("Codex app-server session, live transcript and tool approvals")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                StatusBadge(text: runtimeHeadline)
                Button {
                    store.connectRuntime()
                } label: {
                    Label(store.runtime.connected ? "Reconnect" : "Connect", systemImage: "bolt.horizontal")
                }
                Button {
                    store.runtime.newSession()
                } label: {
                    Label("New session", systemImage: "plus")
                }
                Button(role: .destructive) {
                    store.runtime.stopSession()
                } label: {
                    Label("Stop", systemImage: "stop.fill")
                }
                .disabled(!store.runtime.hasSession)
            }

            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) {
                    runtimeFact("Workspace", workspaceName)
                    runtimeFact("Model", runtimeModel)
                    runtimeFact("Filesystem", store.settings.sandbox.rawValue)
                    runtimeFact("State", runtimeDetail)
                }
                VStack(alignment: .leading, spacing: 8) {
                    runtimeFact("Workspace", workspaceName)
                    runtimeFact("Model", runtimeModel)
                    runtimeFact("Filesystem", store.settings.sandbox.rawValue)
                    runtimeFact("State", runtimeDetail)
                }
            }
        }
        .padding(14)
        .background(Color.secondary.opacity(0.07))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private var conversationPanel: some View {
        Panel(title: "Conversation", subtitle: conversationSubtitle) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 10) {
                        if store.runtime.messages.isEmpty && store.runtime.reasoning.isEmpty {
                            RuntimeInlineEmpty(
                                title: "No turns yet",
                                subtitle: "Start a session and send a real prompt from the composer.",
                                systemImage: "text.bubble"
                            )
                        }

                        ForEach(store.runtime.messages) { message in
                            runtimeMessage(message)
                        }

                        if !store.runtime.reasoning.isEmpty {
                            RuntimeReasoningBlock(text: store.runtime.reasoning)
                        }

                        Color.clear.frame(height: 1).id("runtime-bottom")
                    }
                }
                .onChange(of: conversationScrollToken) {
                    withAnimation(.easeOut(duration: 0.16)) {
                        proxy.scrollTo("runtime-bottom", anchor: .bottom)
                    }
                }
            }
        }
    }

    private var approvalsPanel: some View {
        Panel(title: "Tool Approvals", subtitle: approvalSubtitle) {
            if store.runtime.approvals.isEmpty {
                RuntimeInlineEmpty(
                    title: "No approval waiting",
                    subtitle: "Tool requests appear here before the runtime continues.",
                    systemImage: "checkmark.seal"
                )
            } else {
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(store.runtime.approvals) { approval in
                        approvalCard(approval)
                    }
                }
            }
        }
    }

    private var sessionPanel: some View {
        Panel(title: "Session", subtitle: "Current runtime context") {
            VStack(alignment: .leading, spacing: 8) {
                RuntimeFactRow(title: "Connection", value: runtimeHeadline)
                RuntimeFactRow(title: "Workspace", value: workspaceName)
                RuntimeFactRow(title: "Model", value: runtimeModel)
                RuntimeFactRow(title: "Sandbox", value: store.settings.sandbox.rawValue)
                RuntimeFactRow(title: "Messages", value: "\(store.runtime.messages.count)")
            }
        }
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("New turn")
                    .font(.headline)
                Spacer()
                Text("Command-Return")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            HStack(alignment: .bottom, spacing: 10) {
                TextField("Ask the app-server runtime to work in this workspace", text: $prompt, axis: .vertical)
                    .textFieldStyle(.roundedBorder)
                    .lineLimit(2...5)
                Button {
                    sendPrompt()
                } label: {
                    Label("Run", systemImage: "paperplane.fill")
                }
                .keyboardShortcut(.return, modifiers: [.command])
                .disabled(prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(14)
        .background(Color.secondary.opacity(0.07))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func runtimeMessage(_ message: RuntimeMessage) -> some View {
        HStack(alignment: .top) {
            if message.role == "user" {
                Spacer(minLength: 80)
            }

            VStack(alignment: .leading, spacing: 7) {
                HStack(spacing: 7) {
                    Image(systemName: message.role == "user" ? "person.crop.circle" : "sparkles")
                    Text(message.role == "user" ? "You" : "Runtime")
                        .font(.caption.weight(.semibold))
                }
                .foregroundStyle(message.role == "user" ? Color.blue : Color.green)

                Text(message.text.isEmpty ? "Working..." : message.text)
                    .textSelection(.enabled)
                    .lineSpacing(3)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(12)
            .frame(maxWidth: message.role == "user" ? 620 : .infinity, alignment: .leading)
            .background(message.role == "user" ? Color.blue.opacity(0.11) : Color.secondary.opacity(0.08))
            .clipShape(RoundedRectangle(cornerRadius: 8))

            if message.role != "user" {
                Spacer(minLength: 80)
            }
        }
    }

    private func approvalCard(_ approval: RuntimeApprovalRequest) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(approval.method)
                .font(.subheadline.weight(.semibold))
            Text(approval.params)
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(8)
                .textSelection(.enabled)
            HStack {
                ForEach(approval.choices, id: \.self) { choice in
                    Button(choice) {
                        store.runtime.respond(to: approval, choice: choice)
                    }
                }
            }
        }
        .padding(10)
        .background(Color.primary.opacity(0.035))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func sendPrompt() {
        let text = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        prompt = ""
        if !store.runtime.connected {
            store.connectRuntime()
        }
        store.runtime.sendPrompt(
            prompt: text,
            cwd: store.settings.projectPath,
            model: store.settings.selectedModel,
            sandbox: store.settings.sandbox
        )
    }

    private var runtimeHeadline: String {
        if store.runtime.status == .running {
            return "running"
        }
        return store.runtime.connected ? "connected" : "disconnected"
    }

    private var runtimeDetail: String {
        if store.runtime.connected {
            return store.runtime.hasSession ? "session ready" : "connected, no session"
        }
        return "connect first"
    }

    private var runtimeModel: String {
        store.settings.selectedModel.isEmpty ? store.settings.selectedChatModel : store.settings.selectedModel
    }

    private var workspaceName: String {
        URL(fileURLWithPath: store.settings.projectPath).lastPathComponent
    }

    private var conversationSubtitle: String {
        store.runtime.messages.isEmpty ? "Live runtime messages" : "\(store.runtime.messages.count) messages"
    }

    private var approvalSubtitle: String {
        store.runtime.approvals.isEmpty ? "No pending decision" : "\(store.runtime.approvals.count) pending"
    }

    private var conversationScrollToken: String {
        store.runtime.messages
            .map { "\($0.id.uuidString):\($0.text.count)" }
            .joined(separator: "|") + ":\(store.runtime.reasoning.count)"
    }

    private func runtimeFact(_ label: String, _ value: String) -> some View {
        HStack(spacing: 7) {
            Text(label)
                .foregroundStyle(.secondary)
            Text(value.isEmpty ? "default" : value)
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .font(.subheadline)
        .padding(.horizontal, 10)
        .frame(height: 30)
        .background(Color.primary.opacity(0.035))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }
}

private struct RuntimeInlineEmpty: View {
    var title: String
    var subtitle: String
    var systemImage: String

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: systemImage)
                .font(.title3)
                .foregroundStyle(.secondary)
                .frame(width: 24)
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.subheadline.weight(.semibold))
                Text(subtitle)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.primary.opacity(0.035))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }
}

private struct RuntimeReasoningBlock: View {
    var text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label("Reasoning", systemImage: "brain")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
            Text(text)
                .font(.caption)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
        }
        .padding(10)
        .background(Color.primary.opacity(0.035))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }
}

private struct RuntimeFactRow: View {
    var title: String
    var value: String

    var body: some View {
        HStack(spacing: 8) {
            Text(title)
                .foregroundStyle(.secondary)
                .frame(width: 82, alignment: .leading)
            Text(value)
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.caption)
    }
}
