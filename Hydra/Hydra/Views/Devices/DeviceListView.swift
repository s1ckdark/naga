import SwiftUI
#if canImport(AppKit)
import AppKit
#endif

struct DeviceListView: View {
    @EnvironmentObject var dashboardVM: DashboardViewModel
    @ObservedObject private var prefs = DevicePreferences.shared
    @State private var selectedDevice: Device?
    @State private var searchText = ""
    @State private var isEditing = false

    var filteredDevices: [Device] {
        let ordered = prefs.apply(to: dashboardVM.devices, id: \.id)
        if searchText.isEmpty { return ordered }
        return ordered.filter {
            $0.hostname.localizedCaseInsensitiveContains(searchText) ||
            $0.name.localizedCaseInsensitiveContains(searchText) ||
            $0.tailscaleIp.contains(searchText)
        }
    }

    var body: some View {
        NavigationSplitView {
            Group {
                if isEditing {
                    DeviceEditList(prefs: prefs, devices: dashboardVM.devices)
                } else {
                    List(filteredDevices, selection: $selectedDevice) { device in
                        DeviceRowView(device: device)
                            .tag(device)
                    }
                    .searchable(text: $searchText, prompt: "Search devices")
                }
            }
            .navigationTitle("Devices")
            .navigationSplitViewColumnWidth(min: 280, ideal: 320, max: 400)
            .toolbar {
                ToolbarItem {
                    Button(action: { withAnimation { isEditing.toggle() } }) {
                        Image(systemName: isEditing ? "checkmark.circle.fill" : "list.bullet.indent")
                    }
                    .help(isEditing ? "Done editing" : "Edit device order & visibility")
                }
                ToolbarItem {
                    Button(action: { Task { await dashboardVM.load() } }) {
                        Image(systemName: "arrow.clockwise")
                    }
                    .disabled(isEditing)
                }
            }
            .onChange(of: dashboardVM.devices) {
                prefs.merge(deviceIds: dashboardVM.devices.map(\.id))
            }
            .onAppear {
                prefs.merge(deviceIds: dashboardVM.devices.map(\.id))
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

// MARK: - Edit List

struct DeviceEditList: View {
    @ObservedObject var prefs: DevicePreferences
    let devices: [Device]

    private func device(for entry: DevicePreferences.Entry) -> Device? {
        devices.first { $0.id == entry.deviceId }
    }

    var body: some View {
        List {
            ForEach(prefs.entries) { entry in
                if let device = device(for: entry) {
                    HStack(spacing: 10) {
                        // Visibility toggle
                        Button {
                            withAnimation { prefs.setVisible(entry.deviceId, visible: !entry.visible) }
                        } label: {
                            Image(systemName: entry.visible ? "eye.fill" : "eye.slash")
                                .foregroundStyle(entry.visible ? .primary : .tertiary)
                        }
                        .buttonStyle(.plain)

                        // Device info
                        VStack(alignment: .leading, spacing: 2) {
                            HStack(spacing: 4) {
                                Circle()
                                    .fill(device.isOnline ? .green : .red)
                                    .frame(width: 6, height: 6)
                                Text(device.shortName)
                                    .fontWeight(.medium)
                                    .lineLimit(1)
                            }
                            Text(device.tailscaleIp)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        .opacity(entry.visible ? 1 : 0.4)

                        Spacer()

                        // Move buttons
                        VStack(spacing: 0) {
                            Button {
                                withAnimation { prefs.moveUp(entry.deviceId) }
                            } label: {
                                Image(systemName: "chevron.up")
                                    .font(.caption2)
                            }
                            .buttonStyle(.plain)

                            Button {
                                withAnimation { prefs.moveDown(entry.deviceId) }
                            } label: {
                                Image(systemName: "chevron.down")
                                    .font(.caption2)
                            }
                            .buttonStyle(.plain)
                        }
                        .foregroundStyle(.secondary)
                    }
                    .padding(.vertical, 2)
                }
            }
            .onMove { source, destination in
                prefs.move(fromOffsets: source, toOffset: destination)
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

    // SSH recovery state. The banner is gated on showSSHBanner, which is only
    // raised when the metrics endpoint reports a backend SSH error (m.hasError),
    // never on transport errors or string-matched English phrases.
    @State private var sshErrorText: String?
    @State private var diagnosis: SSHDiagnosis?
    @State private var isDiagnosing = false
    @State private var showFingerprintAlert = false
    @State private var recoveryMessage: String?
    @State private var showSSHBanner = false

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

                // SSH recovery banner
                if showSSHBanner {
                    sshRecoveryBanner
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
            let m = try await APIClient.shared.getDeviceMetrics(id: device.id)
            metrics = m
            if m.hasError {
                sshErrorText = m.error
                showSSHBanner = true
            } else {
                sshErrorText = nil
                recoveryMessage = nil
                diagnosis = nil
                showSSHBanner = false
            }
        } catch {
            // Transport / API errors are not necessarily SSH failures; leave
            // the banner state untouched so a flapping connection does not
            // open a recovery flow that the user can not act on.
        }
    }

    @ViewBuilder
    private var sshRecoveryBanner: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 6) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(.orange)
                        .accessibilityHidden(true)
                    Text(diagnosis?.humanTitle ?? "SSH 연결 오류")
                        .font(.callout.bold())
                        .accessibilityLabel("SSH 연결 경고: \(diagnosis?.humanTitle ?? "SSH 연결 오류")")
                    Spacer()
                    if isDiagnosing {
                        ProgressView().controlSize(.small)
                    }
                    Button {
                        showSSHBanner = false
                    } label: {
                        Image(systemName: "xmark")
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel("SSH 복구 배너 닫기")
                }
                if let text = sshErrorText, !text.isEmpty {
                    Text(text)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(3)
                        .textSelection(.enabled)
                }
                if let msg = recoveryMessage {
                    Text(msg)
                        .font(.caption)
                        .foregroundStyle(.green)
                }
                HStack {
                    Button {
                        Task { await runRecoveryAction() }
                    } label: {
                        Label(recoveryActionTitle, systemImage: recoveryActionIcon)
                    }
                    .disabled(isDiagnosing)
                    Spacer()
                }
            }
            .padding(.vertical, 4)
        }
        .alert("새 호스트 키를 신뢰하시겠습니까?", isPresented: $showFingerprintAlert) {
            Button("취소", role: .cancel) {}
            Button("이 새 키 신뢰", role: .destructive) {
                Task { await acceptHostKey() }
            }
        } message: {
            Text(fingerprintAlertMessage)
        }
    }

    private var recoveryActionTitle: String {
        switch diagnosis?.category {
        case "host_key_mismatch": return "신뢰하고 업데이트"
        case "network_unreachable": return "재시도"
        case "auth_failed": return "로그인 정보 확인"
        case "key_file_missing": return "키 파일 위치 확인"
        case "tailscale": return "Tailscale 열기"
        case "ok": return "다시 진단"
        case .none: return "SSH 연결 진단"
        default: return "다시 진단"
        }
    }

    private var recoveryActionIcon: String {
        switch diagnosis?.category {
        case "host_key_mismatch": return "checkmark.shield"
        case "network_unreachable": return "arrow.clockwise"
        case "auth_failed": return "key"
        case "key_file_missing": return "doc.questionmark"
        case "tailscale": return "network"
        default: return "wrench.and.screwdriver"
        }
    }

    private func runRecoveryAction() async {
        switch diagnosis?.category {
        case "host_key_mismatch":
            if diagnosis?.hostKeyFingerprint != nil {
                showFingerprintAlert = true
            } else {
                await runDiagnose()
            }
        case "tailscale":
            openTailscaleApp()
        case "auth_failed":
            recoveryMessage = "SSH 키가 서버의 authorized_keys에 등록되어 있는지, 사용자 계정이 올바른지 확인하세요."
            await runDiagnose()
        case "key_file_missing":
            recoveryMessage = "Hydra가 사용하는 SSH 개인키 경로를 환경 설정에서 확인하세요."
            await runDiagnose()
        default:
            await runDiagnose()
        }
    }

    private func openTailscaleApp() {
        #if canImport(AppKit)
        if let url = URL(string: "tailscale://") {
            NSWorkspace.shared.open(url)
        }
        #endif
    }

    private var fingerprintAlertMessage: String {
        let fp = diagnosis?.hostKeyFingerprint ?? "(알 수 없음)"
        let host = diagnosis?.hostname ?? device.tailscaleIp
        return """
        \(host)의 호스트 키가 known_hosts에 저장된 값과 다릅니다. 서버를 재설치했을 수도, 중간자(MITM) 공격일 수도 있습니다.

        새 키 지문:
        \(fp)

        서버에서 직접 확인한 지문과 일치할 때만 신뢰하세요.
        """
    }

    private func runDiagnose() async {
        isDiagnosing = true
        recoveryMessage = nil
        defer { isDiagnosing = false }
        do {
            let d = try await APIClient.shared.diagnoseSSH(id: device.id)
            diagnosis = d
            if d.isOK {
                recoveryMessage = "SSH 연결이 정상입니다."
                sshErrorText = nil
                await fetchMetrics()
                return
            }
            if d.isHostKeyMismatch, d.hostKeyFingerprint != nil {
                showFingerprintAlert = true
            }
        } catch {
            recoveryMessage = "진단 실패: \(error.localizedDescription)"
        }
    }

    private func acceptHostKey() async {
        guard let fp = diagnosis?.hostKeyFingerprint else { return }
        isDiagnosing = true
        defer { isDiagnosing = false }
        do {
            _ = try await APIClient.shared.acceptSSHHostKey(id: device.id, fingerprint: fp)
            recoveryMessage = "호스트 키가 업데이트되었습니다. 재연결 중..."
            sshErrorText = nil
            diagnosis = nil
            await fetchMetrics()
        } catch {
            recoveryMessage = "키 저장 실패: \(error.localizedDescription)"
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
