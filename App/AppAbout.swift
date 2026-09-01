import AppKit

/// The standard macOS About panel, populated with both versions that matter, a
/// one-line description, and a link to the project page.
///
/// "Which version am I running" is two questions here: macOS fills in the app's
/// own version from the bundle, and the credits block adds the version of the
/// `quotamon` core inside `Contents/Resources` — the binary every number in this
/// app comes from. That version is obtained by *running* the core rather than
/// from a build-time constant, because the core is copied in by a pre-build
/// phase and a broken or hand-assembled bundle can carry one older than the app
/// around it. Reporting what is really there is the whole point of the line.
///
/// Menu-bar (accessory) apps must activate first or the panel opens behind other
/// windows.
enum AppAbout {

    /// The resolved `quotamon core …` line, cached so reopening About does not
    /// respawn the binary. Failures are cached with it: a bundle whose core
    /// could not be run once will not run it on the next open either.
    @MainActor
    private static var cachedCoreVersion: String?

    @MainActor
    static func show() {
        NSApp.activate(ignoringOtherApps: true)

        // Resolving the core version runs a subprocess, so the panel is ordered
        // front once the answer is in hand rather than blocking the menu action.
        Task { @MainActor in
            NSApp.orderFrontStandardAboutPanel(options: [
                .applicationName: "Quota Monitor",
                .credits: credits(coreVersionLine: await coreVersionLine()),
            ])
        }
    }

    /// The core-version line, followed by the description paragraph and the
    /// project link.
    @MainActor
    private static func credits(coreVersionLine: String) -> NSAttributedString {
        let secondary: [NSAttributedString.Key: Any] = [
            .font: NSFont.systemFont(ofSize: 11),
            .foregroundColor: NSColor.secondaryLabelColor,
        ]

        let notes = NSMutableAttributedString(
            string: coreVersionLine + "\n\n",
            attributes: secondary
        )
        notes.append(NSAttributedString(
            string: "See how much of your LLM subscription quota you have left — Claude, "
                + "ChatGPT, Grok, and other providers - all in one place.\n\n",
            attributes: secondary
        ))
        if let url = URL(string: "https://ubyjvovk.github.io") {
            notes.append(NSAttributedString(
                string: "ubyjvovk.github.io",
                attributes: [.link: url, .font: NSFont.systemFont(ofSize: 11)]
            ))
        }
        return notes
    }

    /// Asks the bundled core for its version, once per app launch.
    @MainActor
    private static func coreVersionLine() async -> String {
        if let cachedCoreVersion { return cachedCoreVersion }

        var line = "quotamon core: not bundled"
        if let runner = CoreBinary.runner {
            do {
                let version = try await runner.coreVersion()
                line = "quotamon core \(version)"
            } catch {
                // The user cannot act on a spawn failure here, and the panel is
                // not the place to explain one — say the version is unknown.
                line = "quotamon core: unavailable"
            }
        }

        cachedCoreVersion = line
        return line
    }
}
