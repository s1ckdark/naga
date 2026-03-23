import SwiftUI

#if os(iOS)
struct iOSTaskListView: View {
    @State private var tasks: [ServerTask] = []
    @State private var isLoading = false

    var body: some View {
        NavigationStack {
            Group {
                if tasks.isEmpty {
                    ContentUnavailableView(
                        "No Tasks",
                        systemImage: "tray",
                        description: Text("Tasks assigned to this device will appear here")
                    )
                } else {
                    List(tasks) { task in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text(task.type)
                                    .font(.headline)
                                Spacer()
                                Text(task.priority)
                                    .font(.caption)
                                    .padding(.horizontal, 8)
                                    .padding(.vertical, 2)
                                    .background(priorityColor(task.priority).opacity(0.2))
                                    .clipShape(Capsule())
                            }
                            Text("Required: \(task.requiredCapabilities.joined(separator: ", "))")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            .navigationTitle("Tasks")
            .refreshable {
                await loadTasks()
            }
        }
    }

    private func loadTasks() async {
        // TODO: Load from API
    }

    private func priorityColor(_ priority: String) -> Color {
        switch priority {
        case "urgent": return .red
        case "high": return .orange
        case "normal": return .blue
        case "low": return .gray
        default: return .gray
        }
    }
}
#endif
