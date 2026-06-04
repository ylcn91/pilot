import Foundation

@MainActor
final class RuntimeStore: ObservableObject {
    @Published var connected = false
    @Published var status: RuntimeStatus = .disconnected
    @Published var hasSession = false
    @Published var messages: [RuntimeMessage] = []
    @Published var reasoning = ""
    @Published var approvals: [RuntimeApprovalRequest] = []
    @Published var error: String?

    private var webSocket: URLSessionWebSocketTask?
    private var gatewayURL = ""

    func connect(gatewayURL: String) {
        self.gatewayURL = gatewayURL
        disconnect()
        guard let url = runtimeWebSocketURL(from: gatewayURL) else {
            status = .error
            error = "Invalid gateway URL"
            return
        }

        let socket = URLSession.shared.webSocketTask(with: url)
        webSocket = socket
        socket.resume()
        connected = false
        status = .disconnected
        error = nil
        socket.sendPing { [weak self] error in
            Task { @MainActor in
                guard let self else { return }
                if let error {
                    self.connected = false
                    self.status = .disconnected
                    self.error = error.localizedDescription
                    return
                }
                self.connected = true
                self.status = .connected
                self.receive()
            }
        }
    }

    func disconnect() {
        webSocket?.cancel(with: .normalClosure, reason: nil)
        webSocket = nil
        connected = false
        if status != .running {
            status = .disconnected
        }
    }

    func sendPrompt(prompt: String, cwd: String, model: String, sandbox: RuntimeSandbox) {
        let cleanPrompt = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !cleanPrompt.isEmpty else { return }
        let payload: [String: Any]
        if hasSession {
            payload = [
                "action": "codexruntime.turn",
                "prompt": cleanPrompt
            ]
        } else {
            var start: [String: Any] = [
                "action": "codexruntime.start",
                "prompt": cleanPrompt,
                "cwd": cwd,
                "sandbox": sandbox.rawValue
            ]
            if !model.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                start["model"] = model
            }
            payload = start
        }

        sendTask(payload)
        messages.append(RuntimeMessage(role: "user", text: cleanPrompt))
        hasSession = true
        status = .running
        error = nil
        reasoning = ""
        approvals = []
    }

    func stopSession() {
        sendTask(["action": "codexruntime.stop"])
        hasSession = false
        status = connected ? .connected : .disconnected
        approvals = []
    }

    func newSession() {
        stopSession()
        messages = []
        reasoning = ""
        error = nil
    }

    func respond(to approval: RuntimeApprovalRequest, choice: String) {
        let isPermission = approval.method == "item/permissions/requestApproval"
        var payload: [String: Any] = [
            "action": "codexruntime.approval.respond",
            "requestId": approval.requestId
        ]
        if isPermission {
            payload["scope"] = choice
        } else {
            payload["decision"] = choice
        }
        sendTask(payload)
        approvals.removeAll { $0.requestId == approval.requestId }
    }

    private func receive() {
        webSocket?.receive { [weak self] result in
            Task { @MainActor in
                guard let self else { return }
                switch result {
                case .failure(let error):
                    self.connected = false
                    self.status = self.status == .running ? .running : .disconnected
                    self.error = error.localizedDescription
                case .success(let message):
                    self.handle(message)
                    self.receive()
                }
            }
        }
    }

    private func handle(_ message: URLSessionWebSocketTask.Message) {
        let text: String
        switch message {
        case .string(let value):
            text = value
        case .data(let data):
            text = String(data: data, encoding: .utf8) ?? ""
        @unknown default:
            return
        }
        guard let data = text.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              object["type"] as? String == "progress",
              let payload = object["payload"] as? [String: Any],
              payload["source"] as? String == "codexruntime" else {
            return
        }

        switch payload["kind"] as? String {
        case "error":
            status = .error
            error = payload["error"] as? String ?? "codex runtime error"
        case "approval_request":
            approvals.append(RuntimeApprovalRequest(
                requestId: payload["requestId"] as? Int ?? 0,
                method: payload["method"] as? String ?? "approval",
                params: stringify(payload["params"]),
                choices: payload["choices"] as? [String] ?? []
            ))
        case "event":
            handleRuntimeEvent(payload["event"] as? [String: Any])
        default:
            break
        }
    }

    private func handleRuntimeEvent(_ event: [String: Any]?) {
        guard let event, let type = event["type"] as? String else { return }
        switch type {
        case "agent_message_delta":
            appendAssistantDelta(event["delta"] as? String ?? "")
        case "reasoning_delta":
            reasoning += event["delta"] as? String ?? ""
        case "turn_started":
            status = .running
            hasSession = true
        case "turn_completed":
            status = .completed
        case "error":
            status = .error
            error = event["error"] as? String ?? "codex runtime error"
        default:
            break
        }
    }

    private func appendAssistantDelta(_ delta: String) {
        guard !delta.isEmpty else { return }
        if messages.last?.role == "assistant" {
            messages[messages.count - 1].text += delta
        } else {
            messages.append(RuntimeMessage(role: "assistant", text: delta))
        }
    }

    private func sendTask(_ payload: [String: Any]) {
        guard let webSocket else {
            status = .error
            error = "Gateway websocket is not connected"
            return
        }
        do {
            let envelope: [String: Any] = ["type": "task", "payload": payload]
            let data = try JSONSerialization.data(withJSONObject: envelope)
            let text = String(data: data, encoding: .utf8) ?? "{}"
            webSocket.send(.string(text)) { [weak self] error in
                if let error {
                    Task { @MainActor in
                        self?.status = .error
                        self?.error = error.localizedDescription
                    }
                }
            }
        } catch {
            status = .error
            self.error = error.localizedDescription
        }
    }

    private func runtimeWebSocketURL(from gatewayURL: String) -> URL? {
        guard var components = URLComponents(string: gatewayURL) else { return nil }
        components.scheme = components.scheme == "https" ? "wss" : "ws"
        components.path = "/ws"
        components.query = nil
        return components.url
    }

    private func stringify(_ value: Any?) -> String {
        guard let value else { return "" }
        if JSONSerialization.isValidJSONObject(value),
           let data = try? JSONSerialization.data(withJSONObject: value, options: [.prettyPrinted]),
           let text = String(data: data, encoding: .utf8) {
            return text
        }
        return String(describing: value)
    }
}
