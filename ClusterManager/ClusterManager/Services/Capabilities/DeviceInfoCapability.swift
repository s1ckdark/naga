import Foundation
#if os(iOS)
import UIKit
#endif

class DeviceInfoCapability: DeviceCapability {
    let identifier = "device_info"
    let displayName = "Device Info"
    let capabilityDescription = "Provides device hardware and software information"
    var isEnabled = false
    var isAvailable: Bool { true }

    func requestPermissions() async -> Bool { true }

    func execute(payload: [String: Any]) async throws -> [String: Any] {
        var info: [String: Any] = [
            "os": osName(),
            "osVersion": osVersion(),
            "timestamp": ISO8601DateFormatter().string(from: Date())
        ]

        #if os(iOS)
        let device = await UIDevice.current
        info["model"] = await device.model
        info["name"] = await device.name
        info["batteryLevel"] = await device.batteryLevel
        info["batteryState"] = await batteryStateString(device.batteryState)
        #elseif os(macOS)
        info["model"] = Host.current().localizedName ?? "Mac"
        #endif

        info["processorCount"] = ProcessInfo.processInfo.processorCount
        info["physicalMemory"] = ProcessInfo.processInfo.physicalMemory
        info["thermalState"] = thermalStateString(ProcessInfo.processInfo.thermalState)

        return info
    }

    private func osName() -> String {
        #if os(iOS)
        return "iOS"
        #elseif os(macOS)
        return "macOS"
        #else
        return "unknown"
        #endif
    }

    private func osVersion() -> String {
        let v = ProcessInfo.processInfo.operatingSystemVersion
        return "\(v.majorVersion).\(v.minorVersion).\(v.patchVersion)"
    }

    private func thermalStateString(_ state: ProcessInfo.ThermalState) -> String {
        switch state {
        case .nominal: return "nominal"
        case .fair: return "fair"
        case .serious: return "serious"
        case .critical: return "critical"
        @unknown default: return "unknown"
        }
    }

    #if os(iOS)
    @MainActor
    private func batteryStateString(_ state: UIDevice.BatteryState) -> String {
        switch state {
        case .unknown: return "unknown"
        case .unplugged: return "unplugged"
        case .charging: return "charging"
        case .full: return "full"
        @unknown default: return "unknown"
        }
    }
    #endif
}
