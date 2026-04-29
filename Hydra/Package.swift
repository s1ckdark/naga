// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "Hydra",
    platforms: [
        .macOS(.v14),
        .iOS(.v17)
    ],
    products: [
        .executable(name: "Hydra", targets: ["Hydra"]),
    ],
    targets: [
        .executableTarget(
            name: "Hydra",
            path: "Hydra",
            resources: [
                .process("Assets.xcassets")
            ]
        ),
        .testTarget(
            name: "HydraTests",
            dependencies: ["Hydra"],
            path: "Tests/HydraTests"
        ),
    ]
)
