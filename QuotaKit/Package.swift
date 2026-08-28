// swift-tools-version:6.0
import PackageDescription

let package = Package(
    name: "QuotaKit",
    platforms: [.macOS(.v14), .iOS(.v17)],
    products: [
        .library(name: "QuotaKit", targets: ["QuotaKit"]),
        .executable(name: "quotactl", targets: ["quotactl"]),
    ],
    targets: [
        .target(name: "QuotaKit"),
        .executableTarget(name: "quotactl", dependencies: ["QuotaKit"]),
        .testTarget(
            name: "QuotaKitTests",
            dependencies: ["QuotaKit"],
            resources: [.copy("Fixtures")]
        ),
    ]
)
