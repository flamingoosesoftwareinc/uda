// swift-tools-version:5.7
//===----------------------------------------------------------------------===//
//
// This source file is part of the Swift Argument Parser open source project
//
// Copyright (c) 2020 Apple Inc. and the Swift project authors
// Licensed under Apache License v2.0 with Runtime Library Exception
//
// See https://swift.org/LICENSE.txt for license information
//
//===----------------------------------------------------------------------===//

import PackageDescription

let package = Package(
  name: "swift-argument-parser",
  targets: [
    .target(
      name: "ArgumentParser",
      dependencies: ["ArgumentParserToolInfo"]),
    .target(
      name: "ArgumentParserTestHelpers",
      dependencies: ["ArgumentParser", "ArgumentParserToolInfo"]),
    .target(
      name: "ArgumentParserToolInfo"),
    .testTarget(
      name: "ArgumentParserToolInfoTests",
      dependencies: ["ArgumentParserToolInfo"]),
  ]
)
