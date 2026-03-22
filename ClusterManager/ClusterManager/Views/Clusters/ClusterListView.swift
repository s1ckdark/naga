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

                // Worker Processes (live)
                if !vm.workerStatuses.isEmpty {
                    GroupBox("Worker Processes (live · 5s)") {
                        ForEach(vm.workerStatuses) { worker in
                            VStack(alignment: .leading, spacing: 4) {
                                HStack {
                                    Circle()
                                        .fill(worker.hasError ? .red : .green)
                                        .frame(width: 6, height: 6)
                                    Text(worker.shortName)
                                        .font(.caption.bold())
                                    if let gpu = worker.gpu {
                                        Text(gpu)
                                            .font(.caption2)
                                            .foregroundStyle(.purple)
                                    }
                                    Spacer()
                                    Text("\((worker.processes ?? []).count) processes")
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                }

                                if worker.hasError {
                                    Text(worker.error ?? "")
                                        .font(.caption2)
                                        .foregroundStyle(.red)
                                } else {
                                    // GPU processes
                                    if !worker.gpuProcesses.isEmpty {
                                        ForEach(worker.gpuProcesses) { proc in
                                            HStack(spacing: 4) {
                                                Image(systemName: "gpu")
                                                    .font(.caption2)
                                                    .foregroundStyle(.purple)
                                                Text(proc.command)
                                                    .font(.system(.caption2, design: .monospaced))
                                                    .lineLimit(1)
                                                Spacer()
                                                if let vram = proc.vramMB {
                                                    Text("\(vram)MB")
                                                        .font(.system(.caption2, design: .monospaced))
                                                        .foregroundStyle(.purple)
                                                }
                                                Text("PID \(proc.pid)")
                                                    .font(.caption2)
                                                    .foregroundStyle(.secondary)
                                            }
                                            .padding(.leading, 12)
                                        }
                                    }

                                    // Top CPU processes
                                    ForEach(worker.cpuProcesses.prefix(3)) { proc in
                                        HStack(spacing: 4) {
                                            Image(systemName: "cpu")
                                                .font(.caption2)
                                                .foregroundStyle(.blue)
                                            Text(proc.command)
                                                .font(.system(.caption2, design: .monospaced))
                                                .lineLimit(1)
                                            Spacer()
                                            Text(String(format: "%.1f%%", proc.cpuPercent))
                                                .font(.system(.caption2, design: .monospaced))
                                                .foregroundStyle(proc.cpuPercent > 50 ? .red : .secondary)
                                        }
                                        .padding(.leading, 12)
                                    }
                                }

                                if worker.id != vm.workerStatuses.last?.id {
                                    Divider()
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
        .onDisappear { vm.stopProcessPolling() }
    }
}

extension Cluster: Hashable {
    static func == (lhs: Cluster, rhs: Cluster) -> Bool { lhs.id == rhs.id }
    func hash(into hasher: inout Hasher) { hasher.combine(id) }
}
