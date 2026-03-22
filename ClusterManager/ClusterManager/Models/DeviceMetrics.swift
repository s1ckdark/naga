import Foundation

struct DeviceMetrics: Codable {
    let deviceId: String
    let cpu: CPUMetrics
    let memory: MemoryMetrics
    let disk: DiskMetrics
    let collectedAt: Date
    let error: String?

    var hasError: Bool { error != nil && !error!.isEmpty }

    struct CPUMetrics: Codable {
        let usagePercent: Double
        let cores: Int
        let modelName: String
        let loadAvg1: Double
        let loadAvg5: Double
        let loadAvg15: Double
    }

    struct MemoryMetrics: Codable {
        let total: UInt64
        let used: UInt64
        let free: UInt64
        let available: UInt64
        let usagePercent: Double
        let swapTotal: UInt64
        let swapUsed: UInt64
        let swapFree: UInt64

        var totalGB: String { formatBytes(total) }
        var usedGB: String { formatBytes(used) }
        var availableGB: String { formatBytes(available) }

        private func formatBytes(_ bytes: UInt64) -> String {
            let gb = Double(bytes) / 1_073_741_824
            return String(format: "%.1fG", gb)
        }
    }

    struct DiskMetrics: Codable {
        let partitions: [Partition]?

        struct Partition: Codable, Identifiable {
            let mountPoint: String
            let device: String
            let total: UInt64
            let used: UInt64
            let free: UInt64
            let usagePercent: Double

            var id: String { mountPoint }
            var totalGB: String { String(format: "%.0fG", Double(total) / 1_073_741_824) }
            var usedGB: String { String(format: "%.0fG", Double(used) / 1_073_741_824) }
        }
    }
}
