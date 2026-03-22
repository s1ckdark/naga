import Foundation

@MainActor
class ClusterViewModel: ObservableObject {
    @Published var clusters: [Cluster] = []
    @Published var selectedCluster: Cluster?
    @Published var health: ClusterHealth?
    @Published var executeResult: ExecuteResponse?
    @Published var workerStatuses: [WorkerStatus] = []
    @Published var isLoading = false
    @Published var isExecuting = false
    @Published var error: String?

    private let api = APIClient.shared
    private var processPollTask: Task<Void, Never>?

    func loadClusters() async {
        isLoading = true
        do {
            clusters = try await api.listClusters()
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }

    func selectCluster(_ cluster: Cluster) async {
        selectedCluster = cluster
        do {
            health = try await api.getClusterHealth(id: cluster.id)
        } catch {
            self.error = error.localizedDescription
        }
        startProcessPolling()
    }

    func startProcessPolling() {
        processPollTask?.cancel()
        guard let cluster = selectedCluster else { return }
        processPollTask = Task {
            while !Task.isCancelled {
                await fetchProcesses(clusterId: cluster.id)
                try? await Task.sleep(for: .seconds(5))
            }
        }
    }

    func stopProcessPolling() {
        processPollTask?.cancel()
        processPollTask = nil
    }

    private func fetchProcesses(clusterId: String) async {
        do {
            let response = try await api.getClusterProcesses(id: clusterId)
            workerStatuses = response.workers
        } catch {
            // silently retry
        }
    }

    func execute(command: String, timeout: Int = 30) async {
        guard let cluster = selectedCluster else { return }
        isExecuting = true
        executeResult = nil
        do {
            executeResult = try await api.executeOnCluster(id: cluster.id, command: command, timeout: timeout)
        } catch {
            self.error = error.localizedDescription
        }
        isExecuting = false
    }

    func deleteCluster(id: String) async {
        do {
            try await api.deleteCluster(id: id, force: true)
            clusters.removeAll { $0.id == id }
            if selectedCluster?.id == id { selectedCluster = nil }
        } catch {
            self.error = error.localizedDescription
        }
    }
}
