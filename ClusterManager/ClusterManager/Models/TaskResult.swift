import Foundation

struct TaskResult: Codable, Identifiable {
    let deviceId: String
    let deviceName: String
    let gpu: String
    let output: String
    let error: String?
    let durationMs: Double

    var id: String { deviceId }
    var hasError: Bool { error != nil && !error!.isEmpty }
}

struct ExecuteRequest: Encodable {
    let command: String
    let timeout_seconds: Int
}

struct ExecuteResponse: Codable {
    let cluster_id: String
    let command: String
    let worker_count: Int
    let results: [TaskResult]
}
