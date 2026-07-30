// swift-tools-version:5.5
import PackageDescription

let package = Package(
    name: "Workspace",
    products: [
        .library(name: "Core", targets: ["Core"]),
        .executable(name: "App", targets: ["App"]),
    ],
    targets: [
        .target(name: "Core", path: "Core/Sources/Core"),
        .target(name: "App", dependencies: ["Core"], path: "App/Sources/App"),
    ]
)
