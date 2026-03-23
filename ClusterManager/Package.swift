// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "Naga",
    platforms: [
        .macOS(.v14),
        .iOS(.v17)
    ],
    products: [
        .executable(name: "Naga", targets: ["Naga"]),
    ],
    targets: [
        .executableTarget(
            name: "Naga",
            path: "ClusterManager"
        ),
    ]
)
