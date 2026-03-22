import SwiftUI

struct MenuBarView: View {
    @EnvironmentObject var vm: DashboardViewModel
    @StateObject private var gpuVM = GPUMonitorViewModel()

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("GPU Cluster Manager")
                .font(.headline)

            Divider()

            // GPU Summary
            HStack {
                Image(systemName: "gpu")
                    .foregroundStyle(.purple)
                Text(gpuVM.summaryText)
                    .font(.system(.caption, design: .monospaced))
            }

            // Per-node GPU status
            if !gpuVM.nodes.isEmpty {
                ForEach(gpuVM.nodes) { node in
                    if let gpus = node.gpus, !gpus.isEmpty {
                        ForEach(gpus) { gpu in
                            HStack(spacing: 6) {
                                Circle()
                                    .fill(utilizationColor(gpu.utilizationPercent))
                                    .frame(width: 6, height: 6)
                                Text(node.deviceName.components(separatedBy: ".").first ?? node.deviceName)
                                    .font(.caption)
                                    .frame(width: 70, alignment: .leading)
                                    .lineLimit(1)

                                // Utilization bar
                                GeometryReader { geo in
                                    ZStack(alignment: .leading) {
                                        RoundedRectangle(cornerRadius: 2)
                                            .fill(.quaternary)
                                        RoundedRectangle(cornerRadius: 2)
                                            .fill(utilizationColor(gpu.utilizationPercent))
                                            .frame(width: geo.size.width * gpu.utilizationPercent / 100)
                                    }
                                }
                                .frame(width: 50, height: 8)

                                Text(String(format: "%2.0f%%", gpu.utilizationPercent))
                                    .font(.system(.caption2, design: .monospaced))
                                    .frame(width: 30, alignment: .trailing)
                                Text("\(gpu.temperatureC)°")
                                    .font(.system(.caption2, design: .monospaced))
                                    .foregroundStyle(tempColor(gpu.temperatureC))
                                    .frame(width: 28, alignment: .trailing)
                            }
                        }
                    } else if node.hasError {
                        HStack {
                            Image(systemName: "exclamationmark.triangle")
                                .foregroundStyle(.red)
                                .font(.caption2)
                            Text(node.deviceName.components(separatedBy: ".").first ?? node.deviceName)
                                .font(.caption)
                            Spacer()
                            Text("error")
                                .font(.caption2)
                                .foregroundStyle(.red)
                        }
                    }
                }
            }

            Divider()

            HStack {
                Image(systemName: "desktopcomputer")
                Text("\(vm.onlineDevices.count)/\(vm.devices.count) online")
                    .font(.caption)
            }

            HStack {
                Image(systemName: "server.rack")
                Text("\(vm.clusters.count) clusters")
                    .font(.caption)
            }

            if let lastUpdate = gpuVM.lastUpdate {
                Text("Updated: \(lastUpdate.formatted(.dateTime.hour().minute().second()))")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            Divider()

            Button("Open Dashboard") {
                NSApp.activate(ignoringOtherApps: true)
                if let window = NSApp.windows.first(where: { !($0 is NSPanel) }) {
                    window.makeKeyAndOrderFront(nil)
                }
            }

            Button("Refresh Now") {
                Task {
                    await vm.load()
                    await gpuVM.refresh()
                }
            }

            Divider()

            Button("Quit") {
                NSApp.terminate(nil)
            }
            .keyboardShortcut("q")
        }
        .padding(8)
        .frame(width: 300)
        .onAppear {
            Task { await vm.load() }
            gpuVM.startPolling(interval: 10)
        }
        .onDisappear {
            gpuVM.stopPolling()
        }
    }

    func utilizationColor(_ percent: Double) -> Color {
        if percent > 80 { return .red }
        if percent > 50 { return .yellow }
        return .green
    }

    func tempColor(_ temp: Int) -> Color {
        if temp > 80 { return .red }
        if temp > 60 { return .orange }
        return .secondary
    }
}
