import Foundation

struct ClusterProcessesResponse: Codable {
    let cluster_id: String
    let timestamp: Date
    let worker_count: Int
    let workers: [WorkerStatus]
}

struct WorkerStatus: Codable, Identifiable {
    let deviceId: String
    let deviceName: String
    let gpu: String?
    let processes: [WorkerProcess]?
    let error: String?

    var id: String { deviceId }
    var hasError: Bool { error != nil && !error!.isEmpty }
    var shortName: String {
        deviceName.components(separatedBy: ".").first ?? deviceName
    }
    var gpuProcesses: [WorkerProcess] {
        (processes ?? []).filter { $0.isGpu }
    }
    var cpuProcesses: [WorkerProcess] {
        (processes ?? []).filter { !$0.isGpu }
    }
}

struct WorkerProcess: Codable, Identifiable {
    let pid: String
    let processName: String?
    let cpuPercent: Double
    let memPercent: Double
    let vramMB: Int?
    let command: String
    let isGpu: Bool

    var id: String { pid }
}
