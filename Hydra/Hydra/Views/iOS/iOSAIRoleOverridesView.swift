import SwiftUI

#if os(iOS)
struct iOSAIRoleOverridesView: View {
    var body: some View {
        Form {
            iOSRoleOverrideSection(title: "Head Selection", role: "head")
            iOSRoleOverrideSection(title: "Task Scheduling", role: "schedule")
            iOSRoleOverrideSection(title: "Capacity Estimation", role: "capacity")
        }
        .navigationTitle("Per-role Overrides")
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// One Section per role. The Toggle decides whether to inherit the default
/// provider; when off, the same provider/key/endpoint/model fields appear
/// inline. Storage keys (`aiRole_<role>_*`) match what macOS RoleOverrideView
/// uses, so per-role state is shared across platforms when both apps run on
/// the same iCloud account/UserDefaults domain.
private struct iOSRoleOverrideSection: View {
    let title: String
    let role: String

    @AppStorage private var useDefault: Bool
    @AppStorage private var provider: String
    @AppStorage private var apiKey: String
    @AppStorage private var endpoint: String
    @AppStorage private var model: String

    init(title: String, role: String) {
        self.title = title
        self.role = role
        self._useDefault = AppStorage(wrappedValue: true,  "aiRole_\(role)_useDefault")
        self._provider   = AppStorage(wrappedValue: "",    "aiRole_\(role)_provider")
        self._apiKey     = AppStorage(wrappedValue: "",    "aiRole_\(role)_apikey")
        self._endpoint   = AppStorage(wrappedValue: "",    "aiRole_\(role)_endpoint")
        self._model      = AppStorage(wrappedValue: "",    "aiRole_\(role)_model")
    }

    var body: some View {
        Section {
            Toggle("Use default provider", isOn: $useDefault)

            if !useDefault {
                Picker("Provider", selection: $provider) {
                    Text("(unset)").tag("")
                    ForEach(AIProviderConfig.allProviders, id: \.self) { id in
                        Text(AIProviderConfig.label(for: id)).tag(id)
                    }
                }
                .pickerStyle(.menu)

                if AIProviderConfig.isCloudProvider(provider) {
                    SecureField("API Key", text: $apiKey)
                        .textContentType(.password)
                } else if !provider.isEmpty {
                    TextField("Endpoint", text: $endpoint, prompt: Text("http://localhost:11434"))
                        .keyboardType(.URL)
                        .textContentType(.URL)
                        .autocapitalization(.none)
                        .disableAutocorrection(true)
                }

                if !provider.isEmpty {
                    TextField("Model (optional)", text: $model)
                        .autocapitalization(.none)
                        .disableAutocorrection(true)
                }
            }
        } header: {
            Text(title)
        } footer: {
            if useDefault {
                Text("Inherits the default provider configured above.")
                    .font(.caption2)
            }
        }
    }
}
#endif
