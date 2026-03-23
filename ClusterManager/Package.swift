// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "ClusterManager",
    platforms: [
        .macOS(.v14),
        .iOS(.v17)
    ],
    products: [
        .executable(name: "ClusterManager", targets: ["ClusterManager"]),
    ],
    targets: [
        .executableTarget(
            name: "ClusterManager",
            path: "ClusterManager"
        ),
    ]
)
