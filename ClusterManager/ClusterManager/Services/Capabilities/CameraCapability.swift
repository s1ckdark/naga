import Foundation
#if os(iOS)
import AVFoundation
import UIKit

class CameraCapability: DeviceCapability {
    let identifier = "camera"
    let displayName = "Camera"
    let capabilityDescription = "Capture photos using device camera"
    var isEnabled = false

    var isAvailable: Bool {
        UIImagePickerController.isSourceTypeAvailable(.camera)
    }

    func requestPermissions() async -> Bool {
        let status = AVCaptureDevice.authorizationStatus(for: .video)
        if status == .authorized { return true }
        return await AVCaptureDevice.requestAccess(for: .video)
    }

    func execute(payload: [String: Any]) async throws -> [String: Any] {
        // Placeholder - full camera capture requires UIKit integration
        return [
            "status": "camera_ready",
            "availableCameras": AVCaptureDevice.DiscoverySession(
                deviceTypes: [.builtInWideAngleCamera, .builtInUltraWideCamera, .builtInTelephotoCamera],
                mediaType: .video,
                position: .unspecified
            ).devices.map { $0.localizedName }
        ]
    }
}
#endif
