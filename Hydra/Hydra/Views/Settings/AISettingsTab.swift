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

            Section {
                Button {
                    Task { await testConnection() }
                } label: {
                    HStack {
                        Image(systemName: "bolt.horizontal.circle")
                        Text("Test Connection")
                    }
                }
                .disabled(testStatus.isTesting || !hasCredentials)

                if let status = testStatus {
                    switch status {
                    case .testing:
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text("Testing…").font(.caption)
                        }
                    case .success(let msg):
                        Label(msg, systemImage: "checkmark.circle.fill")
                            .foregroundStyle(.green).font(.caption)
                    case .error(let msg):
                        Label(msg, systemImage: "xmark.circle.fill")
                            .foregroundStyle(.red).font(.caption)
                    }
                }
            } header: {
                Text("② Verify")
            }

            Section {
                DisclosureGroup("Advanced: per-role overrides", isExpanded: $showAdvanced) {
                    RoleOverrideView(title: "Head Selection", role: "head")
                    RoleOverrideView(title: "Task Scheduling", role: "schedule")
                    RoleOverrideView(title: "Capacity Estimation", role: "capacity")
                }
            } header: {
                Text("③ Advanced (optional)")
            }

            Section {
                HStack {
                    Button("Save Locally") { saveLocally() }
                        .disabled(!connectionVerified)

                    Spacer()

                    Button("Save & Push to Server") {
                        Task { await pushToServer() }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(!connectionVerified || saveStatus.isSaving)
                }

                if !connectionVerified {
                    Text("Test the connection first before saving.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                if let status = saveStatus {
                    switch status {
                    case .saving:
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text("Pushing to server…").font(.caption)
                        }
                    case .savedLocally:
                        Label("Saved to Keychain", systemImage: "checkmark.circle.fill")
                            .foregroundStyle(.green).font(.caption)
                    case .pushedToServer:
                        Label("Saved locally & pushed to server", systemImage: "checkmark.circle.fill")
                            .foregroundStyle(.green).font(.caption)
                    case .error(let msg):
                        Label(msg, systemImage: "xmark.circle.fill")
                            .foregroundStyle(.red).font(.caption)
                    }
                }
            } header: {
                Text("④ Save")
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

    private var hasCredentials: Bool {
        if authMethod == .apiKey { return !apiKey.isEmpty }
        return !endpoint.isEmpty
    }

    private func testConnection() async {
        withAnimation { testStatus = .testing }

        let urlString: String
        var headers: [String: String] = [:]
        switch provider {
        case "claude":
            urlString = "https://api.anthropic.com/v1/models"
            headers["x-api-key"] = apiKey
            headers["anthropic-version"] = "2023-06-01"
        case "openai":
            urlString = "https://api.openai.com/v1/models"
            headers["Authorization"] = "Bearer \(apiKey)"
        case "zai":
            urlString = "https://api.z.ai/v1/models"
            headers["Authorization"] = "Bearer \(apiKey)"
        case "ollama":
            urlString = endpoint.trimmingCharacters(in: .whitespaces) + "/api/tags"
        case "lmstudio", "openai_compatible":
            urlString = endpoint.trimmingCharacters(in: .whitespaces) + "/v1/models"
        default:
            withAnimation { testStatus = .error("Unknown provider: \(provider)") }
            return
        }

        guard let url = URL(string: urlString) else {
            withAnimation { testStatus = .error("Invalid endpoint URL") }
            return
        }
        var req = URLRequest(url: url, timeoutInterval: 15)
        for (k, v) in headers { req.setValue(v, forHTTPHeaderField: k) }

        do {
            let (_, response) = try await URLSession.shared.data(for: req)
            guard let http = response as? HTTPURLResponse else {
                withAnimation { testStatus = .error("No response") }
                return
            }
            if (200...299).contains(http.statusCode) {
                withAnimation {
                    connectionVerified = true
                    testStatus = .success("Connected to \(provider)")
                }
            } else {
                withAnimation { testStatus = .error("\(provider) returned HTTP \(http.statusCode)") }
            }
        } catch {
            withAnimation { testStatus = .error("Connection failed: \(error.localizedDescription)") }
        }
    }

    private func saveLocally() {
        if authMethod == .apiKey {
            store.set(.aiDefaultAPIKey, value: apiKey)
        } else {
            store.set(.aiDefaultAPIKey, value: "")
        }
        withAnimation { saveStatus = .savedLocally }
        DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
            withAnimation { saveStatus = nil }
        }
    }

    private func pushToServer() async {
        withAnimation { saveStatus = .saving }
        saveLocally()

        var defaultPayload: [String: String] = [
            "provider": provider,
            "model":    model,
        ]
        if authMethod == .apiKey {
            defaultPayload["api_key"] = apiKey
        } else {
            defaultPayload["endpoint"] = endpoint
        }

        let body: [String: Any] = ["default": defaultPayload]

        do {
            let url = URL(string: serverURL)!.appendingPathComponent("api/config/ai")
            var request = URLRequest(url: url, timeoutInterval: 15)
            request.httpMethod = "PUT"
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")

            let serverKey = store.get(.serverAPIKey)
            if !serverKey.isEmpty {
                request.setValue("Bearer \(serverKey)", forHTTPHeaderField: "Authorization")
            }

            request.httpBody = try JSONSerialization.data(withJSONObject: body)
            let (_, response) = try await URLSession.shared.data(for: request)
            guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
                let code = (response as? HTTPURLResponse)?.statusCode ?? 0
                withAnimation { saveStatus = .error("Server returned \(code)") }
                return
            }

            withAnimation { saveStatus = .pushedToServer }
            DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                withAnimation { saveStatus = nil }
            }
        } catch {
            withAnimation { saveStatus = .error(error.localizedDescription) }
        }
    }
}

private extension Optional where Wrapped == AISettingsTab.TestStatus {
    var isTesting: Bool {
        if case .testing = self { return true }
        return false
    }
}

private extension Optional where Wrapped == AISettingsTab.SaveStatus {
    var isSaving: Bool {
        if case .saving = self { return true }
        return false
    }
}

private struct RoleOverrideView: View {
    let title: String
    let role: String

    @AppStorage private var useDefault: Bool
    @AppStorage private var provider: String
    @AppStorage private var endpoint: String
    @AppStorage private var model: String

    init(title: String, role: String) {
        self.title = title
        self.role = role
        self._useDefault = AppStorage(wrappedValue: true, "aiRole_\(role)_useDefault")
        self._provider   = AppStorage(wrappedValue: "",   "aiRole_\(role)_provider")
        self._endpoint   = AppStorage(wrappedValue: "",   "aiRole_\(role)_endpoint")
        self._model      = AppStorage(wrappedValue: "",   "aiRole_\(role)_model")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Toggle(title, isOn: Binding(
                get: { useDefault },
                set: { useDefault = $0 }
            ))
            .toggleStyle(.switch)
            .font(.headline)

            if !useDefault {
                HStack {
                    Text("Provider")
                    Spacer()
                    TextField("claude, openai, ollama…", text: $provider)
                        .textFieldStyle(.roundedBorder)
                        .frame(maxWidth: 220)
                }
                HStack {
                    Text("Endpoint / Key")
                    Spacer()
                    SecureField("api key or endpoint URL", text: $endpoint)
                        .textFieldStyle(.roundedBorder)
                        .frame(maxWidth: 220)
                }
                HStack {
                    Text("Model")
                    Spacer()
                    TextField("(optional)", text: $model)
                        .textFieldStyle(.roundedBorder)
                        .frame(maxWidth: 220)
                }
                Text("Overrides are stored locally. Push to server to apply.")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            } else {
                Text("Uses the default provider above.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }
}
#endif
