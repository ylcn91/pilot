import Foundation

struct PilotGatewayClient {
    var baseURL: URL
    var token: String

    func health() async throws -> Bool {
        let url = baseURL.appendingPathComponent("health")
        let (_, response) = try await URLSession.shared.data(from: url)
        return (response as? HTTPURLResponse)?.statusCode == 200
    }

    func status() async throws -> ServerStatus {
        try await get("/api/v1/status")
    }

    func metrics() async throws -> DashboardMetrics {
        try await get("/api/v1/metrics")
    }

    func queue() async throws -> [QueueTask] {
        try await get("/api/v1/queue")
    }

    func history(limit: Int) async throws -> [HistoryEntry] {
        try await get("/api/v1/history?limit=\(limit)")
    }

    func logs(limit: Int) async throws -> [LogEntry] {
        try await get("/api/v1/logs?limit=\(limit)")
    }

    func autopilot() async throws -> AutopilotStatus {
        try await get("/api/v1/autopilot")
    }

    func architectFindings() async throws -> [Finding] {
        let response: ArchitectResponse = try await get("/api/v1/architect")
        return response.findings
    }

    func gitGraph(limit: Int) async throws -> GitGraphData {
        try await get("/api/v1/gitgraph?limit=\(limit)")
    }

    func tasks() async throws -> [TaskInfo] {
        let response: TasksResponse = try await get("/api/v1/tasks")
        return response.tasks
    }

    func liveness() async throws -> DaemonLiveness {
        try await get("/live")
    }

    /// Reads the Prometheus text exposition from /metrics and extracts the
    /// pilot_api_error_rate gauge. Returns nil when metrics are unconfigured
    /// (503) or the gauge is absent, so callers treat it as "unknown" rather
    /// than surfacing a spurious error.
    func apiErrorRate() async throws -> Double? {
        var request = URLRequest(url: url(for: "/metrics"))
        request.httpMethod = "GET"
        if !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            return nil
        }
        let text = String(decoding: data, as: UTF8.self)
        for rawLine in text.split(whereSeparator: \.isNewline) {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard !line.hasPrefix("#") else { continue }
            guard line.hasPrefix("pilot_api_error_rate ") || line.hasPrefix("pilot_api_error_rate{") else { continue }
            if let value = line.split(whereSeparator: \.isWhitespace).last {
                return Double(value)
            }
        }
        return nil
    }

    private func get<T: Decodable>(_ path: String) async throws -> T {
        var request = URLRequest(url: url(for: path))
        request.httpMethod = "GET"
        if !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            let body = String(data: data, encoding: .utf8) ?? ""
            throw GatewayError.badStatus((response as? HTTPURLResponse)?.statusCode ?? -1, body)
        }
        let decoder = JSONDecoder()
        return try decoder.decode(T.self, from: data)
    }

    private func url(for path: String) -> URL {
        if path.hasPrefix("/") {
            return URL(string: path, relativeTo: baseURL)!.absoluteURL
        }
        return URL(string: "/" + path, relativeTo: baseURL)!.absoluteURL
    }
}

enum GatewayError: LocalizedError {
    case badStatus(Int, String)

    var errorDescription: String? {
        switch self {
        case .badStatus(let status, let body):
            body.isEmpty ? "Gateway returned HTTP \(status)" : "Gateway returned HTTP \(status): \(body)"
        }
    }
}
