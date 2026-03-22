import Foundation

@MainActor
class GPUMonitorViewModel: ObservableObject {
    @Published var nodes: [GPUNodeStatus] = []
    @Published var isLoading = false
    @Published var lastUpdate: Date?
    @Published var error: String?

    private let api = APIClient.shared
    private var pollTask: Task<Void, Never>?

    var totalGPUs: Int { nodes.reduce(0) { $0 + ($1.gpus?.count ?? 0) } }
    var avgUtilization: Double {
        let allGPUs = nodes.flatMap { $0.gpus ?? [] }
        guard !allGPUs.isEmpty else { return 0 }
        return allGPUs.reduce(0) { $0 + $1.utilizationPercent } / Double(allGPUs.count)
    }
    var avgTemperature: Int {
        let allGPUs = nodes.flatMap { $0.gpus ?? [] }
        guard !allGPUs.isEmpty else { return 0 }
        return allGPUs.reduce(0) { $0 + $1.temperatureC } / allGPUs.count
    }
    var summaryText: String {
        guard !nodes.isEmpty else { return "No GPU data" }
        return String(format: "%.0f%% · %d°C · %d GPUs", avgUtilization, avgTemperature, totalGPUs)
    }

    func startPolling(interval: TimeInterval = 5) {
        pollTask?.cancel()
        pollTask = Task {
            while !Task.isCancelled {
                await refresh()
                try? await Task.sleep(for: .seconds(interval))
            }
        }
    }

    func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    func refresh() async {
        isLoading = true
        do {
            let response = try await api.getGPUMonitor()
            nodes = response.nodes
            lastUpdate = response.timestamp
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}
