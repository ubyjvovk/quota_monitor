import Foundation

/// The single source of truth for which providers exist and in what order.
///
/// Both `QuotaEngine` and `quotactl` build their provider list from here, so a
/// provider added later appears in the app and the console tool without a
/// second edit.
public enum ProviderCatalog {

    /// Every configured provider, Claude then Codex.
    ///
    /// - Parameter isLiveEnabled: per-provider-id switch; default enables all.
    public static func all(
        isLiveEnabled: (String) -> Bool = { _ in true }
    ) -> [HybridProvider] {
        [
            HybridProvider(
                providerID: Claude.providerID,
                displayName: Claude.displayName,
                local: ClaudeLocalSource(),
                live: ClaudeLiveSource(),
                liveEnabled: isLiveEnabled(Claude.providerID)
            ),
            HybridProvider(
                providerID: Codex.providerID,
                displayName: Codex.displayName,
                local: CodexLocalSource(),
                live: Codex.liveSourceIfConfigured,
                liveEnabled: isLiveEnabled(Codex.providerID)
            ),
        ]
    }
}
