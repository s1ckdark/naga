import Foundation

@MainActor
class DashboardViewModel: ObservableObject {
    @Published var devices: [Device] = []
    @Published var clusters: [Cluster] = []
    @Published var isLoading = false
    @Published var error: String?

    var gpuDevices: [Device] { devices.filter { $0.hasGpu } }
    var onlineDevices: [Device] { devices.filter { $0.isOnline } }
    var totalGPUs: Int { gpuDevices.reduce(0) { $0 + $1.gpuCount } }

    private let api = APIClient.shared

    func load() async {
        isLoading = true
        error = nil
        do {
            async let d = api.listDevices()
            async let c = api.listClusters()
            devices = try await d
            clusters = try await c
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}
