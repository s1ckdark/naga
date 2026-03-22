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
                Text(device.shortName)
                    .fontWeight(.medium)
                Text(device.tailscaleIp)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            if device.hasGpu {
                Label("\(device.gpuCount)x \(device.gpuModel ?? "")", systemImage: "gpu")
                    .font(.caption)
                    .foregroundStyle(.purple)
            }

            Text(device.os)
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(.quaternary)
                .clipShape(Capsule())
        }
        .padding(.vertical, 2)
    }
}

struct DeviceDetailView: View {
    let device: Device
    @State private var command = ""
    @State private var result: TaskResult?
    @State private var isExecuting = false

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
