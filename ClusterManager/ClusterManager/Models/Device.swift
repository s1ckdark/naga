import Foundation

struct Device: Codable, Identifiable {
    let id: String
    let name: String
    let hostname: String
    let ipAddresses: [String]
    let tailscaleIp: String
    let os: String
    let status: String
    let isExternal: Bool
    let tags: [String]?
    let user: String
    let lastSeen: Date
    let sshEnabled: Bool
    let hasGpu: Bool
    let gpuModel: String?
    let gpuCount: Int

    var isOnline: Bool { status == "online" }
    var displayName: String { name.isEmpty ? hostname : name }
    var shortName: String {
        if let dot = hostname.firstIndex(of: ".") {
            return String(hostname[..<dot])
        }
        return hostname
    }
}
