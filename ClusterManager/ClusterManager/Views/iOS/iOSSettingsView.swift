import SwiftUI

#if os(iOS)
struct iOSSettingsView: View {
    @StateObject private var capabilityRegistry = CapabilityRegistry.shared
    @AppStorage("serverURL") private var serverURL = "http://localhost:8080"
    @AppStorage("apiKey") private var apiKey = ""
    @State private var showingAPIKeyAlert = false

    var body: some View {
        NavigationStack {
            Form {
                Section("Server") {
                    TextField("Server URL", text: $serverURL)
                        .textContentType(.URL)
                        .autocapitalization(.none)

                    SecureField("API Key (for external access)", text: $apiKey)
                }

                Section("Capabilities") {
                    ForEach(Array(capabilityRegistry.capabilities.values), id: \.identifier) { cap in
                        HStack {
                            VStack(alignment: .leading) {
                                Text(cap.displayName)
                                    .font(.body)
                                Text(cap.capabilityDescription)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            Spacer()
                            if !cap.isAvailable {
                                Text("Unavailable")
                                    .font(.caption)
                                    .foregroundStyle(.red)
                            } else {
                                Toggle("", isOn: Binding(
                                    get: { capabilityRegistry.enabledCapabilities.contains(cap.identifier) },
                                    set: { _ in
                                        Task {
                                            await capabilityRegistry.toggle(cap.identifier)
                                        }
                                    }
                                ))
                            }
                        }
                    }
                }

                Section("Device") {
                    HStack {
                        Text("Device ID")
                        Spacer()
                        Text(UIDevice.current.identifierForVendor?.uuidString ?? "Unknown")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    HStack {
                        Text("OS")
                        Spacer()
                        Text("\(UIDevice.current.systemName) \(UIDevice.current.systemVersion)")
                            .foregroundStyle(.secondary)
                    }
                }

                Section("About") {
                    HStack {
                        Text("Version")
                        Spacer()
                        Text("0.1.0")
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .navigationTitle("Settings")
        }
    }
}
#endif
