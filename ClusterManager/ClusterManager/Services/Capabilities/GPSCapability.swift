import Foundation
#if canImport(CoreLocation)
import CoreLocation
#endif

#if os(iOS)
class GPSCapability: NSObject, DeviceCapability, CLLocationManagerDelegate {
    let identifier = "gps"
    let displayName = "GPS Location"
    let capabilityDescription = "Provides device GPS coordinates"
    var isEnabled = false

    var isAvailable: Bool {
        CLLocationManager.locationServicesEnabled()
    }

    private let locationManager = CLLocationManager()
    private var locationContinuation: CheckedContinuation<CLLocation, Error>?

    override init() {
        super.init()
        locationManager.delegate = self
        locationManager.desiredAccuracy = kCLLocationAccuracyBest
    }

    func requestPermissions() async -> Bool {
        let status = locationManager.authorizationStatus
        if status == .authorizedWhenInUse || status == .authorizedAlways {
            return true
        }

        return await withCheckedContinuation { continuation in
            self.locationManager.requestWhenInUseAuthorization()
            // Simple approach: check after a short delay
            DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                let newStatus = self.locationManager.authorizationStatus
                continuation.resume(returning: newStatus == .authorizedWhenInUse || newStatus == .authorizedAlways)
            }
        }
    }

    func execute(payload: [String: Any]) async throws -> [String: Any] {
        let location = try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<CLLocation, Error>) in
            self.locationContinuation = continuation
            self.locationManager.requestLocation()
        }

        return [
            "latitude": location.coordinate.latitude,
            "longitude": location.coordinate.longitude,
            "altitude": location.altitude,
            "accuracy": location.horizontalAccuracy,
            "timestamp": ISO8601DateFormatter().string(from: location.timestamp)
        ]
    }

    // CLLocationManagerDelegate
    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        if let location = locations.last {
            locationContinuation?.resume(returning: location)
            locationContinuation = nil
        }
    }

    func locationManager(_ manager: CLLocationManager, didFailWithError error: Error) {
        locationContinuation?.resume(throwing: error)
        locationContinuation = nil
    }
}
#endif
