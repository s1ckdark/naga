// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "ClusterManager",
    platforms: [.macOS(.v14)],
    targets: [
        .executableTarget(
            name: "ClusterManager",
            path: "ClusterManager"
        ),
    ]
)
