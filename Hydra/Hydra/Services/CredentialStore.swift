import Foundation
import Security

/// Keychain-backed credential store for sensitive values (API keys, secrets).
/// Non-sensitive settings (server URL, tailnet name) use UserDefaults via @AppStorage.
final class CredentialStore {
    static let shared = CredentialStore()

    private let service = "com.hydra.credentials"

    // MARK: - Keychain keys

    enum Key: String, CaseIterable {
        case serverAPIKey = "server_api_key"
        case tailscaleAPIKey = "tailscale_api_key"
        case tailscaleOAuthClientID = "tailscale_oauth_client_id"
        case tailscaleOAuthClientSecret = "tailscale_oauth_client_secret"
        case aiDefaultAPIKey = "ai_default_api_key"
        case aiHeadAPIKey = "ai_head_api_key"
        case aiScheduleAPIKey = "ai_schedule_api_key"
        case aiCapacityAPIKey = "ai_capacity_api_key"
    }

    // MARK: - Public API

    func get(_ key: Key) -> String {
        read(account: key.rawValue) ?? ""
    }

    func set(_ key: Key, value: String) {
        if value.isEmpty {
            delete(account: key.rawValue)
        } else {
            save(account: key.rawValue, value: value)
        }
    }

    func hasValue(_ key: Key) -> Bool {
        read(account: key.rawValue) != nil
    }

    /// Removes all stored credentials.
    func clearAll() {
        for key in Key.allCases {
            delete(account: key.rawValue)
        }
    }

    // MARK: - Keychain operations

    private func save(account: String, value: String) {
        guard let data = value.data(using: .utf8) else { return }

        // Delete existing item first
        delete(account: account)

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleWhenUnlocked,
        ]

        SecItemAdd(query as CFDictionary, nil)
    }

    private func read(account: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]

        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        guard status == errSecSuccess, let data = result as? Data else {
            return nil
        }
        return String(data: data, encoding: .utf8)
    }

    private func delete(account: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        SecItemDelete(query as CFDictionary)
    }
}
