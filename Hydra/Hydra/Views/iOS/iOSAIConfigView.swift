import SwiftUI

#if os(iOS)
struct iOSAIConfigView: View {
    @AppStorage("serverURL") private var serverURL = "http://localhost:8080"
    @AppStorage("aiDefaultProvider") private var provider: String = "claude"
    @AppStorage("aiDefaultEndpoint") private var endpoint: String = ""
    @AppStorage("aiDefaultModel") private var model: String = ""

    @State private var apiKey: String = ""
    @State private var connectionVerified = false
    @State private var testStatus: TestStatus?
    @State private var saveStatus: SaveStatus?

    private let store = CredentialStore.shared

    enum TestStatus {
        case testing
        case success(String)
        case error(String)
    }

    enum SaveStatus {
        case saving
        case saved
        case error(String)
    }

    private var isCloudProvider: Bool { AIProviderConfig.isCloudProvider(provider) }
    private var hasCredentials: Bool {
        if isCloudProvider { return !apiKey.isEmpty }
        return !endpoint.isEmpty
    }

    var body: some View {
        Form {
            providerSection
            verifySection
            advancedSection
            saveSection
        }
        .navigationTitle("AI Provider")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear {
            apiKey = store.get(.aiDefaultAPIKey)
        }
    }

    // MARK: - ① Provider (Default)

    @ViewBuilder
    private var providerSection: some View {
        Section {
            Picker("Provider", selection: $provider) {
                ForEach(AIProviderConfig.allProviders, id: \.self) { id in
                    Text(AIProviderConfig.label(for: id)).tag(id)
                }
            }
            .pickerStyle(.menu)
            .onChange(of: provider) { credentialsChanged() }

            if isCloudProvider {
                SecureField("API Key", text: $apiKey)
                    .textContentType(.password)
                    .onChange(of: apiKey) { credentialsChanged() }
            } else {
                TextField("Endpoint", text: $endpoint, prompt: Text("http://localhost:11434"))
                    .keyboardType(.URL)
                    .textContentType(.URL)
                    .autocapitalization(.none)
                    .onChange(of: endpoint) { credentialsChanged() }
            }

            TextField("Model (optional)", text: $model)
                .autocapitalization(.none)
                .onChange(of: model) { credentialsChanged() }
        } header: {
            Text("AI Provider (Default)")
        }
    }

    // MARK: - ② Verify

    @ViewBuilder
    private var verifySection: some View {
        Section {
            Button {
                Task { await testConnection() }
            } label: {
                HStack {
                    Image(systemName: "bolt.horizontal.circle")
                    Text("Test Connection")
                }
            }
            .disabled(!hasCredentials || testStatus.isTesting)

            if let status = testStatus {
                switch status {
                case .testing:
                    HStack(spacing: 8) {
                        ProgressView().controlSize(.small)
                        Text("Testing…").font(.caption)
                    }
                case .success(let msg):
                    Label(msg, systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                        .font(.caption)
                case .error(let msg):
                    Label(msg, systemImage: "xmark.circle.fill")
                        .foregroundStyle(.red)
                        .font(.caption)
                }
            }
        } header: {
            Text("Verify")
        }
    }

    // MARK: - ③ Advanced (drill-down)

    @ViewBuilder
    private var advancedSection: some View {
        Section {
            NavigationLink("Per-role overrides") {
                iOSAIRoleOverridesView()
            }
        } footer: {
            Text("Override the default provider for specific roles (Head Selection, Task Scheduling, Capacity Estimation).")
                .font(.caption)
        }
    }

    // MARK: - ④ Save & Push

    @ViewBuilder
    private var saveSection: some View {
        Section {
            Button("Save & Push to Server") {
                Task { await pushToServer() }
            }
            .disabled(!connectionVerified || saveStatus.isSaving)
        } header: {
            Text("Save")
        } footer: {
            if !connectionVerified {
                Text("Test the connection first before saving.")
                    .font(.caption)
            }
            if let status = saveStatus {
                switch status {
                case .saving:
                    HStack(spacing: 8) {
                        ProgressView().controlSize(.small)
                        Text("Pushing to server…").font(.caption)
                    }
                case .saved:
                    Label("Pushed to server", systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.green).font(.caption)
                case .error(let msg):
                    Label(msg, systemImage: "xmark.circle.fill")
                        .foregroundStyle(.red).font(.caption)
                }
            }
        }
    }

    // MARK: - Actions

    private func credentialsChanged() {
        connectionVerified = false
        testStatus = nil
        saveStatus = nil
    }

    private func testConnection() async {
        withAnimation { testStatus = .testing }

        guard let req = AIProviderConfig.testConnectionRequest(provider: provider, apiKey: apiKey, endpoint: endpoint) else {
            withAnimation { testStatus = .error("Invalid provider or endpoint") }
            return
        }

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

    private func pushToServer() async {
        withAnimation { saveStatus = .saving }
        // Persist API key to Keychain inline so saveStatus stays .saving
        // for the duration of the network round-trip.
        store.set(.aiDefaultAPIKey, value: isCloudProvider ? apiKey : "")

        var defaultPayload: [String: String] = [
            "provider": provider,
            "model":    model,
        ]
        if isCloudProvider {
            defaultPayload["api_key"] = apiKey
        } else {
            defaultPayload["endpoint"] = endpoint
        }

        var body: [String: Any] = ["default": defaultPayload]
        // Read role overrides from UserDefaults (matching aiRole_<role>_*
        // keys populated by iOSAIRoleOverridesView). When useDefault is
        // true (or unset), skip; otherwise include a per-role override
        // block. This iOS view writes the cloud API key to _apikey; the
        // macOS push path reads _apikey first (PR #1 follow-up wired the
        // dual-read), so values round-trip across platforms.
        let defaults = UserDefaults.standard
        let roleKeys = [
            ("head_selection", "head"),
            ("task_scheduling", "schedule"),
            ("capacity_estimation", "capacity"),
        ]
        for (jsonKey, slug) in roleKeys {
            let raw = defaults.object(forKey: "aiRole_\(slug)_useDefault")
            let useDefault = (raw as? Bool) ?? true
            if useDefault { continue }
            let roleProvider = defaults.string(forKey: "aiRole_\(slug)_provider") ?? ""
            let roleAPIKey   = defaults.string(forKey: "aiRole_\(slug)_apikey") ?? ""
            let roleEndpoint = defaults.string(forKey: "aiRole_\(slug)_endpoint") ?? ""
            let roleModel    = defaults.string(forKey: "aiRole_\(slug)_model") ?? ""
            if roleProvider.isEmpty { continue }
            var override: [String: String] = ["provider": roleProvider, "model": roleModel]
            if AIProviderConfig.isCloudProvider(roleProvider) {
                override["api_key"] = roleAPIKey
            } else {
                override["endpoint"] = roleEndpoint
            }
            body[jsonKey] = override
        }

        guard let baseURL = URL(string: serverURL) else {
            withAnimation { saveStatus = .error("Invalid server URL: \(serverURL)") }
            return
        }

        do {
            let url = baseURL.appendingPathComponent("api/config/ai")
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
            withAnimation { saveStatus = .saved }
        } catch {
            withAnimation { saveStatus = .error(error.localizedDescription) }
        }
    }
}

private extension Optional where Wrapped == iOSAIConfigView.TestStatus {
    var isTesting: Bool {
        if case .testing = self { return true }
        return false
    }
}

private extension Optional where Wrapped == iOSAIConfigView.SaveStatus {
    var isSaving: Bool {
        if case .saving = self { return true }
        return false
    }
}
#endif
