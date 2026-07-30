// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "NestedDeps",
    products: [
        .library(name: "NestedDeps", targets: ["NestedDeps"]),
    ],
    dependencies: [
        .package(url: "https://github.com/example/Baz.git", from: "1.0.0"),
    ],
    targets: [
        .target(name: "NestedDeps", dependencies: [.product(name: "Bar", package: "Baz")]),
        .testTarget(name: "NestedDepsTests", dependencies: ["NestedDeps"]),
    ]
)
