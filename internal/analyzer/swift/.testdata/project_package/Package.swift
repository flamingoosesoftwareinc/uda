// swift-tools-version:5.5
import PackageDescription

let package = Package(
    name: "ProjectPackage",
    targets: [
        .target(name: "Core"),
        .target(name: "App", dependencies: ["Core"]),
        .testTarget(name: "CoreTests", dependencies: ["Core"]),
    ]
)
