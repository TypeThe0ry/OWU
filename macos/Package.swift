// swift-tools-version: 5.10

import PackageDescription

let package = Package(
    name: "PermitAccessClient",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .library(name: "PermitCore", targets: ["PermitCore"]),
        .library(name: "PermitMacPlatform", targets: ["PermitMacPlatform"]),
        .executable(name: "PermitAccessClient", targets: ["PermitApp"])
    ],
    targets: [
        .target(
            name: "PermitCore"
        ),
        .target(
            name: "PermitMacPlatform",
            dependencies: ["PermitCore"],
            linkerSettings: [
                .linkedFramework("Security", .when(platforms: [.macOS])),
                .linkedFramework("Network", .when(platforms: [.macOS]))
            ]
        ),
        .executableTarget(
            name: "PermitApp",
            dependencies: ["PermitCore", "PermitMacPlatform"]
        ),
        .testTarget(
            name: "PermitCoreTests",
            dependencies: ["PermitCore"]
        )
    ],
    swiftLanguageVersions: [.v5]
)
