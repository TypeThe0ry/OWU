// swift-tools-version: 5.10

import PackageDescription

let package = Package(
    name: "OWUMacClient",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .library(name: "OWUCore", targets: ["OWUCore"]),
        .library(name: "OWUMacPlatform", targets: ["OWUMacPlatform"]),
        .executable(name: "OWU", targets: ["OWUApp"])
    ],
    targets: [
        .target(
            name: "OWUCore"
        ),
        .target(
            name: "OWUMacPlatform",
            dependencies: ["OWUCore"],
            linkerSettings: [
                .linkedFramework("Security", .when(platforms: [.macOS])),
                .linkedFramework("Network", .when(platforms: [.macOS]))
            ]
        ),
        .executableTarget(
            name: "OWUApp",
            dependencies: ["OWUCore", "OWUMacPlatform"]
        ),
        .testTarget(
            name: "OWUCoreTests",
            dependencies: ["OWUCore"]
        )
    ],
    swiftLanguageVersions: [.v5]
)
