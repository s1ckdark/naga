import Foundation

@MainActor
class DashboardViewModel: ObservableObject {
    @Published var devices: [Device] = []
    @Published var orchs: [Orch] = []
    @Published var gpuNodes: [GPUNodeStatus] = []
    @Published var tasks: [NagaTask] = []
    @Published var isLoading = false
    @Published var error: String?
    @Published var serverStatus: ServerStatus = .unknown
    @Published var serverVersion: String = ""
    @Published var lastRefresh: Date?

    // Quick command
    @Published var quickCommand = ""
    @Published var quickCommandDeviceId: String?
    @Published var quickCommandResult: TaskResult?
    @Published var isExecutingQuickCommand = false

    enum ServerStatus: String {
        case connected, disconnected, unknown
    }

    /// Combined health used by the top banner.
    /// - healthy:  server reachable AND all devices online
    /// - degraded: server reachable BUT one or more devices offline
    /// - down:     server unreachable (device status unknown)
    /// - unknown:  still checking
    enum SystemHealth {
        case healthy, degraded, down, unknown
    }

    var gpuDevices: [Device] { devices.filter { $0.hasGpu } }
    var onlineDevices: [Device] { devices.filter { $0.isOnline } }
    var offlineDevices: [Device] { devices.filter { !$0.isOnline } }
    var totalGPUs: Int { gpuDevices.reduce(0) { $0 + $1.gpuCount } }

    var systemHealth: SystemHealth {
        switch serverStatus {
        case .unknown: return .unknown
        case .disconnected: return .down
        case .connected: return offlineDevices.isEmpty ? .healthy : .degraded
        }
    }

    // GPU aggregate stats
    var avgGPUUtilization: Double {
        let allGPUs = gpuNodes.flatMap { $0.gpus ?? [] }
        guard !allGPUs.isEmpty else { return 0 }
        return allGPUs.reduce(0) { $0 + $1.utilizationPercent } / Double(allGPUs.count)
    }
    var totalVRAMUsedGB: Double {
        Double(gpuNodes.flatMap { $0.gpus ?? [] }.reduce(0) { $0 + $1.memoryUsedMB }) / 1024
    }
    var totalVRAMTotalGB: Double {
        Double(gpuNodes.flatMap { $0.gpus ?? [] }.reduce(0) { $0 + $1.memoryTotalMB }) / 1024
    }

    // Task stats
    var runningTasks: [NagaTask] { tasks.filter { $0.isRunning } }
    var recentTasks: [NagaTask] {
        tasks.sorted { ($0.completedAt ?? $0.createdAt) > ($1.completedAt ?? $1.createdAt) }
            .prefix(10)
            .map { $0 }
    }
    var runningOrchs: [Orch] { orchs.filter { $0.isRunning } }

    private let api = APIClient.shared
    private var pollTask: Task<Void, Never>?

    func load() async {
        isLoading = true
        error = nil
        await checkServerHealth()
        do {
            async let d = api.listDevices()
            async let c = api.listOrchs()
            devices = try await d
            orchs = try await c
        } catch {
            self.error = error.localizedDescription
        }
        // Non-blocking secondary fetches
        await loadGPU()
        await loadTasks()
        lastRefresh = Date()
        isLoading = false
    }

    func startPolling(interval: TimeInterval = 10) {
        pollTask?.cancel()
        pollTask = Task {
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(interval))
            }
        }
    }

    func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    func executeQuickCommand() async {
        guard !quickCommand.isEmpty, let deviceId = quickCommandDeviceId else { return }
        isExecutingQuickCommand = true
        do {
            quickCommandResult = try await api.executeOnDevice(id: deviceId, command: quickCommand)
        } catch {
            quickCommandResult = TaskResult(
                deviceId: deviceId, deviceName: "", gpu: "",
                output: "", error: error.localizedDescription, durationMs: 0
            )
        }
        isExecutingQuickCommand = false
    }

    // MARK: - Private

    private func checkServerHealth() async {
        do {
            let health = try await api.healthCheck()
            serverStatus = health.status == "healthy" ? .connected : .disconnected
            serverVersion = health.version
        } catch {
            serverStatus = .disconnected
            serverVersion = ""
        }
    }

    private func loadGPU() async {
        do {
            let response = try await api.getGPUMonitor()
            gpuNodes = response.nodes
        } catch {
            // GPU monitoring is optional
        }
    }

    private func loadTasks() async {
        do {
            tasks = try await api.listTasks()
        } catch {
            // Task list is optional
        }
    }
}
