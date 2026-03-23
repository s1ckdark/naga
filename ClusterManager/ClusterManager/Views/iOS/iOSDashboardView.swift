import SwiftUI

#if os(iOS)
struct iOSDashboardView: View {
    @StateObject private var viewModel = DashboardViewModel()
    @StateObject private var wsClient: WebSocketClient
    @StateObject private var capabilityRegistry = CapabilityRegistry.shared

    init() {
        // TODO: Get device ID from Tailscale or generate
        let deviceId = UIDevice.current.identifierForVendor?.uuidString ?? "ios-unknown"
        _wsClient = StateObject(wrappedValue: WebSocketClient(deviceId: deviceId))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 16) {
                    // Connection status
                    HStack {
                        Circle()
                            .fill(wsClient.isConnected ? .green : .red)
                            .frame(width: 8, height: 8)
                        Text(wsClient.isConnected ? "Connected" : "Disconnected")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer()
                    }
                    .padding(.horizontal)

                    // Stats cards
                    LazyVGrid(columns: [
                        GridItem(.flexible()),
                        GridItem(.flexible())
                    ], spacing: 12) {
                        StatCard(title: "Devices", value: "\(viewModel.devices.count)", icon: "desktopcomputer", color: .blue)
                        StatCard(title: "Clusters", value: "\(viewModel.clusters.count)", icon: "server.rack", color: .green)
                        StatCard(title: "Capabilities", value: "\(capabilityRegistry.enabledCapabilities.count)", icon: "cpu", color: .purple)
                        StatCard(title: "Pending Tasks", value: "0", icon: "clock", color: .orange)
                    }
                    .padding(.horizontal)

                    // Active capabilities
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Active Capabilities")
                            .font(.headline)
                            .padding(.horizontal)

                        if capabilityRegistry.enabledCapabilities.isEmpty {
                            Text("No capabilities enabled. Go to Settings to enable.")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .padding(.horizontal)
                        } else {
                            ForEach(capabilityRegistry.enabledIdentifiers(), id: \.self) { cap in
                                HStack {
                                    Image(systemName: "checkmark.circle.fill")
                                        .foregroundStyle(.green)
                                    Text(cap)
                                    Spacer()
                                }
                                .padding(.horizontal)
                            }
                        }
                    }
                }
                .padding(.vertical)
            }
            .navigationTitle("Naga")
            .task {
                await viewModel.loadDashboard()
            }
        }
    }
}

struct StatCard: View {
    let title: String
    let value: String
    let icon: String
    let color: Color

    var body: some View {
        VStack(spacing: 8) {
            Image(systemName: icon)
                .font(.title2)
                .foregroundStyle(color)
            Text(value)
                .font(.title)
                .fontWeight(.bold)
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
        .padding()
        .background(.regularMaterial)
        .clipShape(RoundedRectangle(cornerRadius: 12))
    }
}
#endif
