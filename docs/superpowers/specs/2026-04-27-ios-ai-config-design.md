# iOS AI Settings Tab — Design

## Context

The macOS Hydra GUI gained an "AI" Settings tab in PR #1 that lets an admin configure the Hydra server's AI provider (Claude / OpenAI / Z.AI / Ollama / LM Studio / OpenAI-compatible) plus per-role overrides. The iOS app currently has no equivalent — `iOSSettingsView` exposes only Server URL, API Key, Capabilities, Device, and About sections. This spec brings macOS feature parity to iOS via a drill-down `iOSAIConfigView`, addressing the use case of an operator needing to rotate or disable the server's AI auth from a phone (e.g., responding to a leaked-key incident while away from a desktop).

Out-of-scope alternative ("iOS-local AI": on-device inference for capability decisions via Apple's Foundation Models SDK) is acknowledged and deferred to a separate brainstorm cycle.

## Goals

- Drill-down navigation: `iOSSettingsView → iOSAIConfigView → iOSAIRoleOverridesView`. iOS-natural pattern; main Settings stays uncluttered.
- Full feature parity with macOS: Provider picker, API Key / Endpoint, Test Connection, Save & Push, per-role overrides (Head Selection / Task Scheduling / Capacity Estimation).
- Shared `AIProviderConfig` utility module so both platforms call the same provider-label, ping-URL, and request-validation logic — no drift between iOS and macOS.
- iOS UX touches: `.pickerStyle(.menu)`, `.keyboardType(.URL)`, `.textContentType(.password)` for Password Autofill awareness, Section footers for inline help text.

## Non-Goals

- iOS-local AI / on-device inference (B alternative). Separate cycle.
- iPad split-view-specific layouts. NavigationStack auto-adapts; we don't custom-design.
- Lock Screen widgets / Siri Shortcuts intents. Future cycle.
- iOS UI test target. Manual verification only.
- Localization. English strings; LocalizedStringKey conversion is a later sweep.
- `always_consult` toggle in iOS UI. Server PUT preserves it when omitted (PR #1 already), and the operational case for changing it from a phone is rare. Future cycle if requested.
- Apple Watch companion.

## Architecture overview

### Navigation tree

```
iOSContentView
└── iOSSettingsView (Form, NavigationStack)
    ├── Section "Server"           (existing — unchanged)
    ├── Section "AI"                ← NEW
    │   └── NavigationLink "AI Provider" → iOSAIConfigView
    │                                       ├── ① Provider (Default)
    │                                       ├── ② Verify (Test Connection)
    │                                       ├── ③ Advanced
    │                                       │   └── NavigationLink → iOSAIRoleOverridesView
    │                                       │                         ├── Section: Head Selection
    │                                       │                         ├── Section: Task Scheduling
    │                                       │                         └── Section: Capacity Estimation
    │                                       └── ④ Save & Push to Server
    ├── Section "Capabilities"     (existing — unchanged)
    ├── Section "Device"           (existing — unchanged)
    └── Section "About"            (existing — unchanged)
```

### File structure

**New:**
- `Hydra/Hydra/Services/AIProviderConfig.swift` — cross-platform utility with provider labels, cloud/local sets, test-connection request builder, response validator.
- `Hydra/Hydra/Views/iOS/iOSAIConfigView.swift` — drill-down 1: 4 sections (Provider / Verify / Advanced link / Save).
- `Hydra/Hydra/Views/iOS/iOSAIRoleOverridesView.swift` — drill-down 2: 3 role sections, each with Use-default toggle + collapsible provider fields.
- Test file (cross-platform): `Hydra/Tests/AIProviderConfigTests.swift` if SPM test target exists, or `Hydra/HydraTests/AIProviderConfigTests.swift` for Xcode test target. Implementation phase confirms which target the project uses.

**Modified:**
- `Hydra/Hydra/Views/iOS/iOSSettingsView.swift` — add new `Section("AI")` with the `NavigationLink` to `iOSAIConfigView`.
- `Hydra/Hydra/Views/Settings/AISettingsTab.swift` (macOS) — refactor to call the new `AIProviderConfig` utility for provider label, cloud/local checks, and test-connection request building. Behaviour unchanged; ~50 lines moved out of the view file into the utility.

**Reused (no change):**
- `Hydra/Hydra/Services/APIClient.swift` — `registerCapabilities` already exists; new `getAIConfig` / `updateAIConfig` wrappers added in the implementation plan but their schema mirrors macOS pushToServer's body.
- `Hydra/Hydra/Services/CredentialStore.swift` — Keychain keys `aiDefaultAPIKey`, `aiHeadAPIKey`, `aiScheduleAPIKey`, `aiCapacityAPIKey` are platform-agnostic (`Security.framework`).

## UI breakdown

### iOSAIConfigView

```swift
Section("AI Provider (Default)") {
    Picker("Provider", selection: $provider) {
        ForEach(AIProviderConfig.allProviders, id: \.self) { id in
            Text(AIProviderConfig.label(for: id)).tag(id)
        }
    }
    .pickerStyle(.menu)

    if AIProviderConfig.isCloudProvider(provider) {
        SecureField("API Key", text: $apiKey)
            .textContentType(.password)
    } else {
        TextField("Endpoint", text: $endpoint, prompt: Text("http://localhost:11434"))
            .keyboardType(.URL)
            .textContentType(.URL)
            .autocapitalization(.none)
    }

    TextField("Model (optional)", text: $model)
        .autocapitalization(.none)
}

Section("Verify") {
    Button {
        Task { await testConnection() }
    } label: {
        HStack {
            Image(systemName: "bolt.horizontal.circle")
            Text("Test Connection")
        }
    }
    .disabled(!hasCredentials || testStatus.isTesting)

    // status banner: testing / success / error
}

Section {
    NavigationLink("Per-role overrides") {
        iOSAIRoleOverridesView()
    }
} footer: {
    Text("Override the default provider for specific roles (Head Selection, Task Scheduling, Capacity Estimation).")
        .font(.caption)
}

Section {
    Button("Save & Push to Server") {
        Task { await pushToServer() }
    }
    .disabled(!connectionVerified || saveStatus.isSaving)
} footer: {
    if !connectionVerified {
        Text("Test the connection first before saving.")
    }
    // status banner: saving / pushedToServer / error
}
```

iOS-only UX details:

- `.pickerStyle(.menu)` — taps open a menu; better than wheel for 6 items.
- `.keyboardType(.URL)` + `.autocapitalization(.none)` — no auto-capitalization on Endpoint, slash key in keyboard.
- `.textContentType(.password)` — 1Password / Keychain Autofill integration on the SecureField.
- Single "Save & Push" button (no separate "Save Locally") — iOS use case is always server-bound; persisting to Keychain happens inline inside `pushToServer`.

### iOSAIRoleOverridesView

```swift
Form {
    RoleSection(title: "Head Selection", role: "head")
    RoleSection(title: "Task Scheduling", role: "schedule")
    RoleSection(title: "Capacity Estimation", role: "capacity")
}
.navigationTitle("Per-role Overrides")
```

Each `RoleSection` is a `Section` containing `Toggle("Use default")` and, when off, the same Provider/Key/Endpoint/Model fields as in the parent view. Override values stored in `@AppStorage("aiRole_\(role)_*")` keys, mirroring the macOS `RoleOverrideView` pattern.

## Shared utility — `AIProviderConfig`

Cross-platform Swift file (no `#if`). Pure functions only — no `@State`, no SwiftUI dependencies.

```swift
import Foundation

enum AIProviderConfig {
    static let allProviders: [String] = ["claude", "openai", "zai", "ollama", "lmstudio", "openai_compatible"]
    static let cloudProviders: Set<String> = ["claude", "openai", "zai"]
    static let localProviders: Set<String> = ["ollama", "lmstudio", "openai_compatible"]

    static func isCloudProvider(_ id: String) -> Bool {
        cloudProviders.contains(id)
    }

    /// Display label combining provider id with its group hint.
    static func label(for id: String) -> String {
        switch id {
        case "claude":             return "Claude (cloud)"
        case "openai":             return "OpenAI (cloud)"
        case "zai":                return "Z.AI (cloud)"
        case "ollama":             return "Ollama (local)"
        case "lmstudio":           return "LM Studio (local)"
        case "openai_compatible":  return "OpenAI-compatible (local)"
        default:                   return id
        }
    }

    /// Builds the URLRequest used to verify provider connectivity.
    /// Returns nil for an unknown provider or invalid URL.
    static func testConnectionRequest(provider: String, apiKey: String, endpoint: String) -> URLRequest? {
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
            return nil
        }
        guard let url = URL(string: urlString) else { return nil }
        var req = URLRequest(url: url, timeoutInterval: 15)
        for (k, v) in headers { req.setValue(v, forHTTPHeaderField: k) }
        return req
    }
}
```

Both `iOSAIConfigView.testConnection()` and macOS `AISettingsTab.testConnection()` call `AIProviderConfig.testConnectionRequest(...)` to build the request. The view-level code stays as State + URLSession for the actual call.

The macOS view's existing inline `cloudProviders`/`localProviders` static sets, `label(for:)` method, and the URL/header switch are deleted in favour of the utility. Behaviour identical; ~50 lines move out of the view file.

## Failure & lifecycle

- Server URL is stored in `@AppStorage("serverURL")` (existing). Force-unwrap of `URL(string:)` is guarded — the same pattern the macOS PR's round-4 fix introduced. Invalid URL surfaces in the save-status banner; no crash.
- Test Connection failures (4xx/5xx, timeout, unreachable) populate the Verify section's error banner. The button re-enables for retry. No state outside the view is touched on test failure.
- Push to server failures render in the Save section's error banner. Server-side state unchanged on failure (server's PR #1 round-3 fix rolls back in-memory mutation if `config.Save` errored).
- Per-role override changes that have no `Provider` value send no override field — the server's PR #1 round-5 fix preserves omitted overrides on the server side.

## Testing

### Unit tests (`AIProviderConfigTests`)

| Test | Validates |
|---|---|
| `testProviderLabel_KnownIDs` | All 6 provider ids return the documented `(cloud)` / `(local)` labels |
| `testProviderLabel_UnknownIDFallback` | Unknown id passes through unchanged |
| `testIsCloudProvider_TrueForCloud` | `claude`, `openai`, `zai` → true |
| `testIsCloudProvider_FalseForLocal` | `ollama`, `lmstudio`, `openai_compatible` → false |
| `testTestConnectionRequest_ClaudeHeaders` | Built request has `x-api-key` and `anthropic-version` |
| `testTestConnectionRequest_OpenAIBearerAuth` | OpenAI/Z.AI use `Authorization: Bearer …` |
| `testTestConnectionRequest_OllamaURL` | Endpoint + `/api/tags`; whitespace trimmed |
| `testTestConnectionRequest_LMStudioURL` | Endpoint + `/v1/models` |
| `testTestConnectionRequest_NilForUnknownProvider` | Unsupported id returns nil |

### SwiftUI view tests — out of scope

ViewInspector or UI test target adds infra cost. Manual verification only for the views.

### Manual verification (iOS Simulator)

1. Open Hydra in iOS Simulator → Settings tab.
2. Tap "AI Provider" — drill-down occurs.
3. Switch Provider Claude → Ollama — field flips from API Key to Endpoint.
4. Empty Endpoint → Test Connection button disabled.
5. Enter `http://localhost:11434` → Test Connection — confirms via banner.
6. Tap "Per-role overrides" — drill-down 2 occurs.
7. Toggle "Use default" off for Head Selection → enter override → back → Save & Push.
8. `curl http://127.0.0.1:8080/api/config/ai` returns: `default` updated, `head_selection` populated, secrets masked.
9. Type `localhost:8080` (no scheme) into Server URL → Save & Push → expect "Invalid server URL" banner, no crash.
10. SecureField Password Autofill: with 1Password installed, tap API Key field → confirm autofill suggestion appears.

## Files touched

### New
- `Hydra/Hydra/Services/AIProviderConfig.swift`
- `Hydra/Hydra/Views/iOS/iOSAIConfigView.swift`
- `Hydra/Hydra/Views/iOS/iOSAIRoleOverridesView.swift`
- Tests file (path determined by implementation plan)

### Modified
- `Hydra/Hydra/Views/iOS/iOSSettingsView.swift` — add Section "AI" with NavigationLink
- `Hydra/Hydra/Views/Settings/AISettingsTab.swift` — replace inline label/provider sets/request builder with `AIProviderConfig` calls

## Reuse / existing utilities

- `APIClient` actor in `Hydra/Hydra/Services/APIClient.swift` — `registerCapabilities` is a working precedent for how to add a typed JSON POST. iOS's `pushToServer` either calls a new `APIClient.updateAIConfig(...)` or uses the same direct URLSession pattern as macOS.
- `CredentialStore` Keychain-backed singleton — already has the four `ai*APIKey` cases from PR #1. No new entries needed.
- macOS `AISettingsTab` — pattern reference for the `testStatus` / `saveStatus` enum-based banner UX. iOS view replicates the structure but with iOS-style sections.

## Migration / rollout

- Net-additive: existing iOS views unchanged except `iOSSettingsView` gaining one new `Section`. Users on prior builds see no change until they update.
- Server side requires no change — `GET/PUT /api/config/ai` already supports the partial-update preserve semantics needed for iOS to do role-by-role edits.
- Keychain entries shared with macOS app (different bundle ids), so a macOS-set key does not auto-flow to iOS. Each platform pushes its own copy to the server, and the server is the source of truth for actual scheduler state.
