import SwiftUI

#if os(macOS)
struct AISettingsTab: View {
    @AppStorage("serverURL") private var serverURL = "http://localhost:8080"
    @AppStorage("aiDefaultProvider") private var provider: String = "claude"
    @AppStorage("aiDefaultEndpoint") private var endpoint: String = ""
    @AppStorage("aiDefaultModel") private var model: String = ""

    @State private var authMethod: AuthMethod = .apiKey
    @State private var apiKey: String = ""
    @State private var connectionVerified = false
    @State private var testStatus: TestStatus?
    @State private var saveStatus: SaveStatus?
    @State private var showAdvanced = false

    private let store = CredentialStore.shared

    enum AuthMethod: String, CaseIterable {
        case apiKey = "API Key"
        case localAPI = "Local API"
    }

    enum TestStatus {
        case testing
        case success(String)
        case error(String)
    }

    enum SaveStatus {
        case saving
        case savedLocally
        case pushedToServer
        case error(String)
    }

    private var cloudProviders: [String] { ["claude", "openai", "zai"] }
    private var localProviders: [String] { ["ollama", "lmstudio", "openai_compatible"] }
    private var currentProviders: [String] {
        authMethod == .apiKey ? cloudProviders : localProviders
    }

    var body: some View {
        Form {
            Section {
                Picker("Auth Method", selection: $authMethod) {
                    ForEach(AuthMethod.allCases, id: \.self) { method in
                        Text(method.rawValue).tag(method)
                    }
                }
                .pickerStyle(.segmented)
                .onChange(of: authMethod) {
                    // Reset provider to first available when switching modes
                    if !currentProviders.contains(provider) {
                        provider = currentProviders.first ?? ""
                    }
                    credentialsChanged()
                }

                Picker("Provider", selection: $provider) {
                    ForEach(currentProviders, id: \.self) { p in
                        Text(p).tag(p)
                    }
                }
                .onChange(of: provider) { credentialsChanged() }

                if authMethod == .apiKey {
                    SecureField("API Key", text: $apiKey)
                        .textFieldStyle(.roundedBorder)
                        .onChange(of: apiKey) { credentialsChanged() }
                } else {
                    TextField("Endpoint", text: $endpoint, prompt: Text("http://localhost:11434"))
                        .textFieldStyle(.roundedBorder)
                        .onChange(of: endpoint) { credentialsChanged() }
                }

                TextField("Model (optional)", text: $model)
                    .textFieldStyle(.roundedBorder)
                    .onChange(of: model) { credentialsChanged() }
            } header: {
                Text("① AI Provider (Default)")
            }

            // Placeholder for Verify/Save sections added in later tasks
            Section {
                Text("Test and Save will be wired up next.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .onAppear {
            apiKey = store.get(.aiDefaultAPIKey)
            if !endpoint.isEmpty { authMethod = .localAPI }
        }
    }

    private func credentialsChanged() {
        connectionVerified = false
        testStatus = nil
        saveStatus = nil
    }
}
#endif
