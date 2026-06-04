// swift-tools-version: 5.10

import PackageDescription

let package = Package(
    name: "Pilot91",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .executable(name: "Pilot91", targets: ["Pilot91"])
    ],
    dependencies: [
        .package(url: "https://github.com/migueldeicaza/SwiftTerm.git", from: "1.13.0")
    ],
    targets: [
        .executableTarget(
            name: "Pilot91",
            dependencies: [
                .product(name: "SwiftTerm", package: "SwiftTerm")
            ],
            path: "Sources/Pilot91"
        ),
        .testTarget(
            name: "Pilot91Tests",
            dependencies: ["Pilot91"],
            path: "Tests/Pilot91Tests"
        )
    ]
)
