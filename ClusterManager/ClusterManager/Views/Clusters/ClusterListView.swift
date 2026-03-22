import SwiftUI

struct ClusterListView: View {
    @StateObject private var vm = ClusterViewModel()
    @State private var selectedCluster: Cluster?
    @State private var command = ""

    var body: some View {
        NavigationSplitView {
            List(vm.clusters, selection: $selectedCluster) { cluster in
                ClusterRowView(cluster: cluster)
                    .tag(cluster)
                    .contextMenu {
                        Button("Delete", role: .destructive) {
                            Task { await vm.deleteCluster(id: cluster.id) }
                        }
                    }
            }
            .navigationTitle("Clusters")
            .toolbar {
                ToolbarItem {
                    Button(action: { Task { await vm.loadClusters() } }) {
                        Image(systemName: "arrow.clockwise")
                    }
                }
            }
            .task {
                await vm.loadClusters()
            }
            .onChange(of: selectedCluster) { _, newValue in
                if let cluster = newValue {
                    Task { await vm.selectCluster(cluster) }
                }
            }
        } detail: {
            if let cluster = vm.selectedCluster {
                ClusterDetailView(cluster: cluster, vm: vm)
            } else {
                Text("Select a cluster")
                    .foregroundStyle(.secondary)
            }
        }
    }
}

struct ClusterRowView: View {
    let cluster: Cluster

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(cluster.name)
                    .fontWeight(.medium)
                Text("\(cluster.workerCount) workers")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Text(cluster.status)
                .font(.caption.bold())
                .foregroundStyle(statusColor)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(statusColor.opacity(0.1))
                .clipShape(Capsule())
        }
    }

    var statusColor: Color {
        switch cluster.status {
        case "running": return .green
        case "starting": return .yellow
        case "error": return .red
        default: return .gray
        }
    }
}

struct ClusterDetailView: View {
    let cluster: Cluster
    @ObservedObject var vm: ClusterViewModel
    @State private var command = "nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total --format=csv,noheader"

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                // Header
                HStack {
                    Text(cluster.name)
                        .font(.title2.bold())
                    Spacer()
                    Text(cluster.status)
                        .font(.caption.bold())
                        .foregroundStyle(cluster.isRunning ? .green : .gray)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(cluster.isRunning ? Color.green.opacity(0.1) : Color.gray.opacity(0.1))
                        .clipShape(Capsule())
                }

                // Info
                LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 12) {
                    InfoField(label: "Head Node", value: cluster.headNodeId)
                    InfoField(label: "Workers", value: "\(cluster.workerCount)")
                }

                // Health
                if let health = vm.health {
                    GroupBox("Node Health") {
                        ForEach(health.nodes) { node in
                            HStack {
                                Circle()
                                    .fill(node.healthy ? .green : .red)
                                    .frame(width: 8, height: 8)
                                Text(node.nodeId)
                                    .font(.caption.monospaced())
                                Text(node.role)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                Spacer()
                                if let error = node.error, !error.isEmpty {
                                    Text(error)
                                        .font(.caption)
                                        .foregroundStyle(.red)
                                }
                            }
                        }
                    }
                }

                // Execute
                GroupBox("Distributed Execution") {
                    VStack(alignment: .leading, spacing: 8) {
                        TextField("Command...", text: $command)
                            .textFieldStyle(.roundedBorder)
                            .font(.system(.body, design: .monospaced))

                        Button(action: {
                            Task { await vm.execute(command: command) }
                        }) {
                            Label(vm.isExecuting ? "Running..." : "Run on All Workers", systemImage: "play.fill")
                        }
                        .disabled(command.isEmpty || vm.isExecuting)

                        if let result = vm.executeResult {
                            Text("Results from \(result.worker_count) workers")
                                .font(.caption.bold())

                            ForEach(result.results) { r in
                                VStack(alignment: .leading, spacing: 4) {
                                    HStack {
                                        Text(r.deviceName)
                                            .font(.caption.bold())
                                        if !r.gpu.isEmpty {
                                            Text(r.gpu)
                                                .font(.caption)
                                                .foregroundStyle(.purple)
                                        }
                                        Spacer()
                                        Text(String(format: "%.0fms", r.durationMs))
                                            .font(.caption)
                                            .foregroundStyle(.secondary)
                                    }
                                    Text(r.hasError ? (r.error ?? "") : r.output)
                                        .font(.system(.caption, design: .monospaced))
                                        .foregroundStyle(r.hasError ? .red : .primary)
                                        .padding(6)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                        .background(.quaternary)
                                        .clipShape(RoundedRectangle(cornerRadius: 4))
                                }
                            }
                        }
                    }
                }

                if let error = vm.error {
                    Text(error)
                        .foregroundStyle(.red)
                        .font(.caption)
                }
            }
            .padding()
        }
    }
}

extension Cluster: Hashable {
    static func == (lhs: Cluster, rhs: Cluster) -> Bool { lhs.id == rhs.id }
    func hash(into hasher: inout Hasher) { hasher.combine(id) }
}
