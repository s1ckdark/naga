import SwiftUI

#if os(iOS)
struct iOSContentView: View {
    @StateObject private var capabilityRegistry = CapabilityRegistry.shared

    var body: some View {
        TabView {
            iOSDashboardView()
                .tabItem {
                    Label("Dashboard", systemImage: "gauge")
                }

            DeviceListView()
                .tabItem {
                    Label("Devices", systemImage: "desktopcomputer")
                }

            iOSTaskListView()
                .tabItem {
                    Label("Tasks", systemImage: "list.bullet.clipboard")
                }

            iOSSettingsView()
                .tabItem {
                    Label("Settings", systemImage: "gear")
                }
        }
    }
}
#endif
