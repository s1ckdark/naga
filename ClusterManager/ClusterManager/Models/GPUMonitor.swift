import Foundation

struct GPUMonitorResponse: Codable {
    let timestamp: Date
    let nodes: [GPUNodeStatus]
    let nodeCount: Int
}

struct GPUNodeStatus: Codable, Identifiable {
    let deviceId: String
    let deviceName: String
    let ip: String
    let gpuModel: String
    let gpuCount: Int
    let gpus: [GPUInfo]?
    let error: String?

    var id: String { deviceId }
    var hasError: Bool { error != nil && !error!.isEmpty }

    struct GPUInfo: Codable, Identifiable {
        let index: Int
        let name: String
        let utilizationPercent: Double
        let memoryUsedMB: Int
        let memoryTotalMB: Int
        let temperatureC: Int
        let powerDrawW: Double
        let powerLimitW: Double

        var id: Int { index }
        var memoryPercent: Double {
            guard memoryTotalMB > 0 else { return 0 }
            return Double(memoryUsedMB) / Double(memoryTotalMB) * 100
        }
    }
}
