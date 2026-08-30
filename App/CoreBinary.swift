import Foundation
import QuotaKit

/// The `quotamon` executable that ships inside the app bundle.
///
/// Every number the app shows comes from this binary — the app has no fetchers
/// of its own. The build copies a (universal, where both slices exist) `quotamon`
/// into `Contents/Resources`; if that step did not run, `url` is nil and the UI
/// says so rather than silently showing nothing.
enum CoreBinary {

    /// Location of the bundled core, or nil when the build did not embed it.
    static let url: URL? = Bundle.main.url(forResource: "quotamon", withExtension: nil)

    /// A runner over the bundled core, or nil when it is missing.
    static var runner: QuotamonRunner? {
        guard let url else { return nil }
        return .bundled(executableURL: url)
    }

    /// What to tell the user when the binary is absent — a build problem, not a
    /// provider problem, so the message points at the build.
    static let missingMessage = "quotamon is missing from the app bundle — rebuild with ./scripts/build.sh"
}
