// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "SoundFlow",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "SoundFlow", targets: ["SoundFlowMac"]),
        .executable(name: "soundflow-probe", targets: ["SoundFlowProbe"]),
    ],
    targets: [
        .target(name: "SoundFlowDSP", publicHeadersPath: "include"),
        .target(name: "SoundFlowCore", dependencies: ["SoundFlowDSP"]),
        .executableTarget(name: "SoundFlowMac", dependencies: ["SoundFlowCore", "SoundFlowDSP"]),
        .executableTarget(name: "SoundFlowProbe", dependencies: ["SoundFlowCore", "SoundFlowDSP"]),
        .testTarget(name: "SoundFlowCoreTests", dependencies: ["SoundFlowCore", "SoundFlowDSP"], path: "tests/SoundFlowCoreTests"),
    ]
)
