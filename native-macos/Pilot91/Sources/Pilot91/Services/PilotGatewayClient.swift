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
