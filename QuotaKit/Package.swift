// swift-tools-version:6.0
import PackageDescription

let package = Package(
    name: "QuotaKit",
    platforms: [.macOS(.v14), .iOS(.v17)],
    products: [
        .library(name: "QuotaKit", targets: ["QuotaKit"]),
    ],
    targets: [
        .target(name: "QuotaKit"),
        .testTarget(
            name: "QuotaKitTests",
            dependencies: ["QuotaKit"],
            resources: [.copy("Fixtures")]
        ),
    ]
)
