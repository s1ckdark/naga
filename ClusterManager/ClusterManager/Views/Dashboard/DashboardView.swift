import SwiftUI

struct DashboardView: View {
    @EnvironmentObject var vm: DashboardViewModel

    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                // Summary cards
                HStack(spacing: 16) {
                    SummaryCard(
                        title: "Devices",
                        value: "\(vm.onlineDevices.count)/\(vm.devices.count)",
                        subtitle: "online",
                        icon: "desktopcomputer",
                        color: .blue
                    )
                    SummaryCard(
                        title: "GPU Nodes",
                        value: "\(vm.gpuDevices.count)",
                        subtitle: "\(vm.totalGPUs) GPUs total",
                        icon: "gpu",
                        color: .purple
                    )
                    SummaryCard(
                        title: "Clusters",
                        value: "\(vm.clusters.count)",
                        subtitle: "\(vm.clusters.filter { $0.isRunning }.count) running",
                        icon: "server.rack",
                        color: .green
                    )
                }

                // GPU device list
                if !vm.gpuDevices.isEmpty {
                    GroupBox("GPU Devices") {
                        ForEach(vm.gpuDevices) { device in
                            HStack {
                                Circle()
                                    .fill(device.isOnline ? .green : .red)
                                    .frame(width: 8, height: 8)
                                Text(device.shortName)
                                    .fontWeight(.medium)
                                Spacer()
                                Text("\(device.gpuCount)x \(device.gpuModel ?? "GPU")")
                                    .foregroundStyle(.purple)
                                    .font(.caption)
                                Text(device.tailscaleIp)
                                    .foregroundStyle(.secondary)
                                    .font(.caption)
                            }
                            .padding(.vertical, 2)
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
        .toolbar {
            ToolbarItem {
                Button(action: { Task { await vm.load() } }) {
                    Image(systemName: "arrow.clockwise")
                }
                .disabled(vm.isLoading)
            }
        }
        .navigationTitle("Dashboard")
    }
}

struct SummaryCard: View {
    let title: String
    let value: String
    let subtitle: String
    let icon: String
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Image(systemName: icon)
                    .foregroundStyle(color)
                Text(title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.system(size: 28, weight: .bold))
                .foregroundStyle(color)
            Text(subtitle)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.background)
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .shadow(color: .black.opacity(0.05), radius: 2, y: 1)
    }
}
