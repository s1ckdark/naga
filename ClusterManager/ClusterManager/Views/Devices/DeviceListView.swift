import SwiftUI

struct DeviceListView: View {
    @EnvironmentObject var dashboardVM: DashboardViewModel
    @State private var selectedDevice: Device?
    @State private var searchText = ""

    var filteredDevices: [Device] {
        if searchText.isEmpty { return dashboardVM.devices }
        return dashboardVM.devices.filter {
            $0.hostname.localizedCaseInsensitiveContains(searchText) ||
            $0.name.localizedCaseInsensitiveContains(searchText) ||
            $0.tailscaleIp.contains(searchText)
        }
    }

    var body: some View {
        NavigationSplitView {
            List(filteredDevices, selection: $selectedDevice) { device in
                DeviceRowView(device: device)
                    .tag(device)
            }
            .searchable(text: $searchText, prompt: "Search devices")
            .navigationTitle("Devices")
            .navigationSplitViewColumnWidth(min: 280, ideal: 320, max: 400)
            .toolbar {
                ToolbarItem {
                    Button(action: { Task { await dashboardVM.load() } }) {
                        Image(systemName: "arrow.clockwise")
                    }
                }
            }
        } detail: {
            if let device = selectedDevice {
                DeviceDetailView(device: device)
            } else {
                Text("Select a device")
                    .foregroundStyle(.secondary)
            }
        }
    }
}

struct DeviceRowView: View {
    let device: Device

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(device.isOnline ? .green : .red)
                .frame(width: 8, height: 8)

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    Text(device.shortName)
                        .fontWeight(.medium)
                        .lineLimit(1)
                    Text(device.os)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 4)
                        .padding(.vertical, 1)
                        .background(.quaternary)
                        .clipShape(Capsule())
                }
                HStack(spacing: 4) {
                    Text(device.tailscaleIp)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    if device.hasGpu {
                        Text("\(device.gpuCount)x \(device.gpuModel ?? "")")
                            .font(.caption)
                            .foregroundStyle(.purple)
                    }
                }
            }
        }
        .padding(.vertical, 2)
    }
}

struct DeviceDetailView: View {
    let device: Device
    @State private var command = ""
    @State private var result: TaskResult?
    @State private var isExecuting = false
    @State private var gpuStatus: GPUNodeStatus?
    @State private var metrics: DeviceMetrics?
    @State private var pollTask: Task<Void, Never>?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                // Header
                HStack {
                    VStack(alignment: .leading) {
                        Text(device.displayName)
                            .font(.title2.bold())
                        Text(device.hostname)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    StatusBadge(isOnline: device.isOnline)
                }

                // Info grid
                LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 12) {
                    InfoField(label: "Tailscale IP", value: device.tailscaleIp)
                    InfoField(label: "OS", value: device.os)
                    InfoField(label: "User", value: device.user)
                    InfoField(label: "SSH", value: device.sshEnabled ? "Enabled" : "Disabled")
                    if device.hasGpu {
                        InfoField(label: "GPU", value: "\(device.gpuCount)x \(device.gpuModel ?? "Unknown")")
                    }
                }

                // Live System Status
                if device.isOnline && device.sshEnabled {
                    GroupBox("System Status (live)") {
                        if let m = metrics {
                            if m.hasError {
                                Text(m.error ?? "")
                                    .font(.caption)
                                    .foregroundStyle(.red)
                            } else {
                                VStack(spacing: 10) {
                                    // CPU
                                    HStack {
                                        Label("CPU", systemImage: "cpu")
                                            .font(.caption)
                                            .frame(width: 70, alignment: .leading)
                                        ProgressView(value: m.cpu.usagePercent, total: 100)
                                            .tint(m.cpu.usagePercent > 80 ? .red : m.cpu.usagePercent > 50 ? .orange : .green)
                                        Text(String(format: "%.0f%%", m.cpu.usagePercent))
                                            .font(.system(.caption, design: .monospaced))
                                            .frame(width: 35, alignment: .trailing)
                                    }
                                    HStack {
                                        Text(m.cpu.modelName)
                                            .font(.caption2)
                                            .foregroundStyle(.secondary)
                                            .lineLimit(1)
                                        Spacer()
                                        Text("Load: \(String(format: "%.1f %.1f %.1f", m.cpu.loadAvg1, m.cpu.loadAvg5, m.cpu.loadAvg15))")
                                            .font(.system(.caption2, design: .monospaced))
                                            .foregroundStyle(.secondary)
                                    }

                                    // Memory
                                    HStack {
                                        Label("RAM", systemImage: "memorychip")
                                            .font(.caption)
                                            .frame(width: 70, alignment: .leading)
                                        ProgressView(value: m.memory.usagePercent, total: 100)
                                            .tint(m.memory.usagePercent > 80 ? .red : m.memory.usagePercent > 50 ? .orange : .blue)
                                        Text("\(m.memory.usedGB)/\(m.memory.totalGB)")
                                            .font(.system(.caption, design: .monospaced))
                                            .frame(width: 75, alignment: .trailing)
                                    }

                                    // Disk
                                    if let parts = m.disk.partitions?.prefix(3) {
                                        ForEach(Array(parts)) { p in
                                            HStack {
                                                Label(p.mountPoint, systemImage: "internaldrive")
                                                    .font(.caption)
                                                    .frame(width: 70, alignment: .leading)
                                                    .lineLimit(1)
                                                ProgressView(value: p.usagePercent, total: 100)
                                                    .tint(p.usagePercent > 90 ? .red : p.usagePercent > 70 ? .orange : .gray)
                                                Text("\(p.usedGB)/\(p.totalGB)")
                                                    .font(.system(.caption, design: .monospaced))
                                                    .frame(width: 75, alignment: .trailing)
                                            }
                                        }
                                    }
                                }
                            }
                        } else {
                            ProgressView("Loading system metrics...")
                                .font(.caption)
                        }
                    }
                }

                // Live GPU Status
                if device.hasGpu {
                    GroupBox("GPU Status (live)") {
                        if let status = gpuStatus, let gpus = status.gpus {
                            ForEach(gpus) { gpu in
                                VStack(spacing: 8) {
                                    // GPU Utilization
                                    HStack {
                                        Label("Core", systemImage: "gpu")
                                            .font(.caption)
                                            .frame(width: 70, alignment: .leading)
                                        ProgressView(value: gpu.utilizationPercent, total: 100)
                                            .tint(gpu.utilizationPercent > 80 ? .red : gpu.utilizationPercent > 50 ? .orange : .green)
                                        Text(String(format: "%.0f%%", gpu.utilizationPercent))
                                            .font(.system(.caption, design: .monospaced))
                                            .fontWeight(.bold)
                                            .foregroundStyle(gpu.utilizationPercent > 80 ? .red : gpu.utilizationPercent > 50 ? .orange : .green)
                                            .frame(width: 35, alignment: .trailing)
                                    }

                                    // VRAM
                                    HStack {
                                        Label("VRAM", systemImage: "memorychip")
                                            .font(.caption)
                                            .frame(width: 70, alignment: .leading)
                                        ProgressView(value: gpu.memoryPercent, total: 100)
                                            .tint(gpu.memoryPercent > 80 ? .red : gpu.memoryPercent > 50 ? .orange : .purple)
                                        Text(String(format: "%.0f%%", gpu.memoryPercent))
                                            .font(.system(.caption, design: .monospaced))
                                            .frame(width: 35, alignment: .trailing)
                                    }
                                    HStack {
                                        Spacer()
                                        Text("\(gpu.memoryUsedMB)MB / \(gpu.memoryTotalMB)MB")
                                            .font(.system(.caption2, design: .monospaced))
                                            .foregroundStyle(.secondary)
                                    }

                                    // Temperature & Power
                                    HStack {
                                        Label("\(gpu.temperatureC)°C", systemImage: "thermometer")
                                            .font(.caption)
                                            .foregroundStyle(gpu.temperatureC > 80 ? .red : gpu.temperatureC > 60 ? .orange : .secondary)
                                        Spacer()
                                        Label(String(format: "%.0fW / %.0fW", gpu.powerDrawW, gpu.powerLimitW), systemImage: "bolt")
                                            .font(.caption)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                            }
                        } else if let status = gpuStatus, status.hasError {
                            Text(status.error ?? "Unknown error")
                                .font(.caption)
                                .foregroundStyle(.red)
                        } else {
                            ProgressView("Loading GPU data...")
                                .font(.caption)
                        }
                    }
                }

                // Execute command
                if device.isOnline && device.sshEnabled {
                    GroupBox("Execute Command") {
                        VStack(alignment: .leading, spacing: 8) {
                            TextField("Command...", text: $command)
                                .textFieldStyle(.roundedBorder)
                                .font(.system(.body, design: .monospaced))

                            Button(action: {
                                Task { await executeCommand() }
                            }) {
                                Label(isExecuting ? "Running..." : "Execute", systemImage: "play.fill")
                            }
                            .disabled(command.isEmpty || isExecuting)

                            if let result = result {
                                VStack(alignment: .leading, spacing: 4) {
                                    HStack {
                                        Text(result.hasError ? "Error" : "Output")
                                            .font(.caption.bold())
                                        Spacer()
                                        Text(String(format: "%.0fms", result.durationMs))
                                            .font(.caption)
                                            .foregroundStyle(.secondary)
                                    }
                                    Text(result.hasError ? (result.error ?? "") : result.output)
                                        .font(.system(.caption, design: .monospaced))
                                        .foregroundStyle(result.hasError ? .red : .primary)
                                        .padding(8)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                        .background(.quaternary)
                                        .clipShape(RoundedRectangle(cornerRadius: 4))
                                }
                            }
                        }
                    }
                }
            }
            .padding()
        }
        .navigationTitle(device.shortName)
        .onAppear { startGPUPolling() }
        .onDisappear { pollTask?.cancel() }
        .onChange(of: device) { _, _ in
            pollTask?.cancel()
            gpuStatus = nil
            metrics = nil
            startGPUPolling()
        }
    }

    private func startGPUPolling() {
        guard device.isOnline && device.sshEnabled else { return }
        pollTask = Task {
            while !Task.isCancelled {
                await fetchMetrics()
                if device.hasGpu {
                    await fetchGPUStatus()
                }
                try? await Task.sleep(for: .seconds(5))
            }
        }
    }

    private func fetchMetrics() async {
        do {
            metrics = try await APIClient.shared.getDeviceMetrics(id: device.id)
        } catch {
            // silently retry next cycle
        }
    }

    private func fetchGPUStatus() async {
        do {
            let response = try await APIClient.shared.getGPUMonitor()
            gpuStatus = response.nodes.first { $0.deviceId == device.id }
        } catch {
            // silently retry next cycle
        }
    }

    private func executeCommand() async {
        isExecuting = true
        do {
            result = try await APIClient.shared.executeOnDevice(id: device.id, command: command)
        } catch {
            result = TaskResult(deviceId: device.id, deviceName: device.displayName, gpu: "", output: "", error: error.localizedDescription, durationMs: 0)
        }
        isExecuting = false
    }
}

struct InfoField: View {
    let label: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .fontWeight(.medium)
        }
    }
}

struct StatusBadge: View {
    let isOnline: Bool

    var body: some View {
        Text(isOnline ? "Online" : "Offline")
            .font(.caption.bold())
            .foregroundStyle(isOnline ? .green : .red)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(isOnline ? Color.green.opacity(0.1) : Color.red.opacity(0.1))
            .clipShape(Capsule())
    }
}

extension Device: Hashable {
    static func == (lhs: Device, rhs: Device) -> Bool { lhs.id == rhs.id }
    func hash(into hasher: inout Hasher) { hasher.combine(id) }
}
