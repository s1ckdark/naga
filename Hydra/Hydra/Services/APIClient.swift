import Foundation

actor APIClient {
    static let shared = APIClient()

    private var baseURL: URL
    private let session: URLSession
    private let decoder: JSONDecoder

    init() {
        let stored = UserDefaults.standard.string(forKey: "serverURL") ?? "http://localhost:8080"
        self.baseURL = URL(string: stored)!
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

    /// Reloads base URL from UserDefaults (call after settings change).
    func reloadBaseURL() {
        let stored = UserDefaults.standard.string(forKey: "serverURL") ?? "http://localhost:8080"
        self.baseURL = URL(string: stored)!
    }

    // MARK: - Devices

    func listDevices(refresh: Bool = false) async throws -> [Device] {
        let path = refresh ? "/api/devices?refresh=true" : "/api/devices"
        return try await get(path)
    }

    func getDevice(id: String) async throws -> Device {
        return try await get("/api/devices/\(id)")
    }

    func getDeviceMetrics(id: String) async throws -> DeviceMetrics {
        return try await get("/api/devices/\(id)/metrics")
    }

    func executeOnDevice(id: String, command: String, timeout: Int = 30) async throws -> TaskResult {
        return try await post("/api/devices/\(id)/execute", body: ExecuteRequest(command: command, timeout_seconds: timeout))
    }

    /// Reports the device's enabled capabilities to the server. Used by
    /// CapabilityReporter on app launch and reconnect so the AI scheduler
    /// can route capability-tagged tasks here.
    func registerCapabilities(deviceID: String, capabilities: [String]) async throws -> CapabilityRegisterResponse {
        let req = CapabilityRegisterRequest(capabilities: capabilities)
        return try await post("/api/devices/\(deviceID)/capabilities", body: req)
    }

    private struct CapabilityRegisterRequest: Encodable {
        let capabilities: [String]
    }

    struct CapabilityRegisterResponse: Decodable {
        let deviceId: String
        let capabilities: [String]
    }

    // MARK: - SSH Recovery

    struct EmptyBody: Encodable {}

    struct AcceptKeyRequest: Encodable { let fingerprint: String }

    struct OKResponse: Decodable { let status: String }

    func diagnoseSSH(id: String) async throws -> SSHDiagnosis {
        return try await post("/api/devices/\(id)/ssh/diagnose", body: EmptyBody())
    }

    func acceptSSHHostKey(id: String, fingerprint: String) async throws -> OKResponse {
        return try await post("/api/devices/\(id)/ssh/accept-key", body: AcceptKeyRequest(fingerprint: fingerprint))
    }

    // MARK: - Orchs

    func listOrchs() async throws -> [Orch] {
        return try await get("/api/orchs")
    }

    func getOrch(id: String) async throws -> Orch {
        return try await get("/api/orchs/\(id)")
    }

    func createOrch(name: String, headID: String, workerIDs: [String]) async throws -> Orch {
        let req = CreateOrchRequest(name: name, head_id: headID, worker_ids: workerIDs)
        return try await post("/api/orchs", body: req)
    }

    func getOrchProcesses(id: String) async throws -> OrchProcessesResponse {
        return try await get("/api/orchs/\(id)/processes")
    }

    func getGPUMonitor() async throws -> GPUMonitorResponse {
        return try await get("/api/monitor/gpu")
    }

    func deleteOrch(id: String, force: Bool = false) async throws {
        let path = force ? "/api/orchs/\(id)?force=true" : "/api/orchs/\(id)"
        let _: [String: String] = try await delete(path)
    }

    func getOrchHealth(id: String) async throws -> OrchHealth {
        return try await get("/api/orchs/\(id)/health")
    }

    func executeOnOrch(id: String, command: String, timeout: Int = 30) async throws -> ExecuteResponse {
        return try await post("/api/orchs/\(id)/execute", body: ExecuteRequest(command: command, timeout_seconds: timeout))
    }

    // MARK: - Tasks

    func listTasks() async throws -> [NagaTask] {
        return try await get("/api/tasks")
    }

    // MARK: - Health

    func healthCheck() async throws -> HealthResponse {
        return try await get("/health")
    }

    struct HealthResponse: Decodable {
        let status: String
        let version: String
    }

    // MARK: - Auth

    struct AuthMeResponse: Decodable {
        let authenticated: Bool
        let ip: String?
        let network: String?
        let user: String?
        let device: Device?
    }

    func authMe() async throws -> AuthMeResponse {
        return try await get("/api/auth/me")
    }

    func setBaseURL(_ urlString: String) {
        guard let url = URL(string: urlString) else { return }
        self.baseURL = url
        UserDefaults.standard.set(urlString, forKey: "serverURL")
    }

    // MARK: - HTTP

    private func makeURL(_ path: String) -> URL {
        URL(string: path, relativeTo: baseURL)!.absoluteURL
    }

    private func get<T: Decodable>(_ path: String) async throws -> T {
        let (data, response) = try await session.data(from: makeURL(path))
        try checkResponse(response, data)
        return try decoder.decode(T.self, from: data)
    }

    private func post<T: Decodable, B: Encodable>(_ path: String, body: B) async throws -> T {
        var request = URLRequest(url: makeURL(path))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        applyAuth(&request)
        request.httpBody = try JSONEncoder().encode(body)
        let (data, response) = try await session.data(for: request)
        try checkResponse(response, data)
        return try decoder.decode(T.self, from: data)
    }

    private func delete<T: Decodable>(_ path: String) async throws -> T {
        var request = URLRequest(url: makeURL(path))
        request.httpMethod = "DELETE"
        applyAuth(&request)
        let (data, response) = try await session.data(for: request)
        try checkResponse(response, data)
        return try decoder.decode(T.self, from: data)
    }

    private func applyAuth(_ request: inout URLRequest) {
        let key = CredentialStore.shared.get(.serverAPIKey)
        if !key.isEmpty {
            request.setValue("Bearer \(key)", forHTTPHeaderField: "Authorization")
        }
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
