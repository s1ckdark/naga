import SwiftUI

@main
struct ClusterManagerApp: App {
    @StateObject private var dashboardVM = DashboardViewModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(dashboardVM)
        }
        .defaultSize(width: 1000, height: 700)

        MenuBarExtra("GPU Cluster", systemImage: "server.rack") {
            MenuBarView()
                .environmentObject(dashboardVM)
        }
    }
}
