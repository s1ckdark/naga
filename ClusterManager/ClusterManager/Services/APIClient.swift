import Foundation

actor APIClient {
    static let shared = APIClient()

    private let baseURL: URL
    private let session: URLSession
    private let decoder: JSONDecoder

    init(baseURL: String = "http://localhost:8080") {
        self.baseURL = URL(string: baseURL)!
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 30
        self.session = URLSession(configuration: config)

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let str = try container.decode(String.self)
            // Try ISO8601 with fractional seconds
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = formatter.date(from: str) { return date }
            formatter.formatOptions = [.withInternetDateTime]
            if let date = formatter.date(from: str) { return date }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Invalid date: \(str)")
        }
        self.decoder = decoder
    }

    // MARK: - Devices

    func listDevices(refresh: Bool = false) async throws -> [Device] {
        let path = refresh ? "/api/devices?refresh=true" : "/api/devices"
        return try await get(path)
    }

    func getDevice(id: String) async throws -> Device {
        return try await get("/api/devices/\(id)")
    }

    func executeOnDevice(id: String, command: String, timeout: Int = 30) async throws -> TaskResult {
        return try await post("/api/devices/\(id)/execute", body: ExecuteRequest(command: command, timeout_seconds: timeout))
    }

    // MARK: - Clusters

    func listClusters() async throws -> [Cluster] {
        return try await get("/api/clusters")
    }

    func getCluster(id: String) async throws -> Cluster {
        return try await get("/api/clusters/\(id)")
    }

    func createCluster(name: String, headID: String, workerIDs: [String]) async throws -> Cluster {
        let req = CreateClusterRequest(name: name, head_id: headID, worker_ids: workerIDs)
        return try await post("/api/clusters", body: req)
    }

    func deleteCluster(id: String, force: Bool = false) async throws {
        let path = force ? "/api/clusters/\(id)?force=true" : "/api/clusters/\(id)"
        let _: [String: String] = try await delete(path)
    }

    func getClusterHealth(id: String) async throws -> ClusterHealth {
        return try await get("/api/clusters/\(id)/health")
    }

    func executeOnCluster(id: String, command: String, timeout: Int = 30) async throws -> ExecuteResponse {
        return try await post("/api/clusters/\(id)/execute", body: ExecuteRequest(command: command, timeout_seconds: timeout))
    }

    // MARK: - HTTP

    private func get<T: Decodable>(_ path: String) async throws -> T {
        let url = baseURL.appendingPathComponent(path)
        let (data, response) = try await session.data(from: url)
        try checkResponse(response, data)
        return try decoder.decode(T.self, from: data)
    }

    private func post<T: Decodable, B: Encodable>(_ path: String, body: B) async throws -> T {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)
        let (data, response) = try await session.data(for: request)
        try checkResponse(response, data)
        return try decoder.decode(T.self, from: data)
    }

    private func delete<T: Decodable>(_ path: String) async throws -> T {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = "DELETE"
        let (data, response) = try await session.data(for: request)
        try checkResponse(response, data)
        return try decoder.decode(T.self, from: data)
    }

    private func checkResponse(_ response: URLResponse, _ data: Data) throws {
        guard let http = response as? HTTPURLResponse else { return }
        guard (200...299).contains(http.statusCode) else {
            let msg = (try? JSONDecoder().decode([String: String].self, from: data))?["error"] ?? "Unknown error"
            throw APIError.server(status: http.statusCode, message: msg)
        }
    }
}

enum APIError: LocalizedError {
    case server(status: Int, message: String)

    var errorDescription: String? {
        switch self {
        case .server(let status, let message):
            return "[\(status)] \(message)"
        }
    }
}
