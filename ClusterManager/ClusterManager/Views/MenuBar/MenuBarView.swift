import SwiftUI

struct MenuBarView: View {
    @EnvironmentObject var vm: DashboardViewModel

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("GPU Cluster Manager")
                .font(.headline)

            Divider()

            HStack {
                Image(systemName: "desktopcomputer")
                Text("\(vm.onlineDevices.count)/\(vm.devices.count) online")
            }

            HStack {
                Image(systemName: "gpu")
                Text("\(vm.gpuDevices.count) GPU nodes (\(vm.totalGPUs) GPUs)")
            }

            HStack {
                Image(systemName: "server.rack")
                Text("\(vm.clusters.count) clusters")
            }

            Divider()

            Button("Open Dashboard") {
                NSApp.activate(ignoringOtherApps: true)
                if let window = NSApp.windows.first(where: { $0.title.contains("Cluster") || $0.isKeyWindow }) {
                    window.makeKeyAndOrderFront(nil)
                }
            }

            Button("Refresh") {
                Task { await vm.load() }
            }

            Divider()

            Button("Quit") {
                NSApp.terminate(nil)
            }
            .keyboardShortcut("q")
        }
        .padding(8)
        .task {
            await vm.load()
        }
    }
}
