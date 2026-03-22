import SwiftUI

struct ContentView: View {
    @EnvironmentObject var dashboardVM: DashboardViewModel

    var body: some View {
        TabView {
            DashboardView()
                .tabItem { Label("Dashboard", systemImage: "gauge") }

            DeviceListView()
                .tabItem { Label("Devices", systemImage: "desktopcomputer") }

            ClusterListView()
                .tabItem { Label("Clusters", systemImage: "server.rack") }
        }
        .task {
            await dashboardVM.load()
        }
    }
}
