import SwiftUI

@main
struct ClusterManagerApp: App {
    @StateObject private var dashboardVM = DashboardViewModel()

    var body: some Scene {
        #if os(iOS)
        WindowGroup {
            iOSContentView()
                .onAppear {
                    setupCapabilities()
                }
        }
        #else
        WindowGroup {
            ContentView()
                .environmentObject(dashboardVM)
                .onAppear {
                    setupCapabilities()
                }
        }
        .defaultSize(width: 1000, height: 700)

        MenuBarExtra("GPU Cluster", systemImage: "server.rack") {
            MenuBarView()
                .environmentObject(dashboardVM)
        }
        .menuBarExtraStyle(.window)
        #endif
    }

    private func setupCapabilities() {
        let registry = CapabilityRegistry.shared
        #if os(iOS)
        registry.register(GPSCapability())
        registry.register(CameraCapability())
        #endif
        registry.register(DeviceInfoCapability())
    }
}
