import Foundation

/// Represents a device capability that can be registered with the server
protocol DeviceCapability: AnyObject {
    /// Unique identifier for this capability (e.g., "gps", "camera", "sms")
    var identifier: String { get }

    /// Human-readable name
    var displayName: String { get }

    /// Description of what this capability provides
    var capabilityDescription: String { get }

    /// Whether this capability is currently enabled by the user
    var isEnabled: Bool { get set }

    /// Whether the device hardware supports this capability
    var isAvailable: Bool { get }

    /// Execute a task using this capability
    func execute(payload: [String: Any]) async throws -> [String: Any]

    /// Request necessary permissions
    func requestPermissions() async -> Bool
}

/// Registry that manages all device capabilities
@MainActor
class CapabilityRegistry: ObservableObject {
    static let shared = CapabilityRegistry()

    @Published private(set) var capabilities: [String: any DeviceCapability] = [:]
    @Published var enabledCapabilities: Set<String> = [] {
        didSet {
            UserDefaults.standard.set(Array(enabledCapabilities), forKey: "enabledCapabilities")
        }
    }

    private init() {
        // Load saved preferences
        if let saved = UserDefaults.standard.array(forKey: "enabledCapabilities") as? [String] {
            enabledCapabilities = Set(saved)
        }
    }

    /// Register a capability plugin
    func register(_ capability: any DeviceCapability) {
        capabilities[capability.identifier] = capability
        capability.isEnabled = enabledCapabilities.contains(capability.identifier)
    }

    /// Toggle a capability on/off
    func toggle(_ identifier: String) async -> Bool {
        guard let cap = capabilities[identifier] else { return false }

        if enabledCapabilities.contains(identifier) {
            enabledCapabilities.remove(identifier)
            cap.isEnabled = false
            return true
        } else {
            // Request permissions first
            let granted = await cap.requestPermissions()
            if granted {
                enabledCapabilities.insert(identifier)
                cap.isEnabled = true
                return true
            }
            return false
        }
    }

    /// Get list of enabled capability identifiers for server registration
    func enabledIdentifiers() -> [String] {
        capabilities.values
            .filter { $0.isEnabled && $0.isAvailable }
            .map { $0.identifier }
    }

    /// Execute a task on the appropriate capability
    func executeTask(type: String, payload: [String: Any]) async throws -> [String: Any] {
        guard let cap = capabilities[type] else {
            throw CapabilityError.notFound(type)
        }
        guard cap.isEnabled else {
            throw CapabilityError.disabled(type)
        }
        guard cap.isAvailable else {
            throw CapabilityError.unavailable(type)
        }
        return try await cap.execute(payload: payload)
    }
}

enum CapabilityError: LocalizedError {
    case notFound(String)
    case disabled(String)
    case unavailable(String)
    case permissionDenied(String)
    case executionFailed(String)

    var errorDescription: String? {
        switch self {
        case .notFound(let cap): return "Capability '\(cap)' not found"
        case .disabled(let cap): return "Capability '\(cap)' is disabled"
        case .unavailable(let cap): return "Capability '\(cap)' is not available on this device"
        case .permissionDenied(let cap): return "Permission denied for '\(cap)'"
        case .executionFailed(let msg): return "Execution failed: \(msg)"
        }
    }
}
