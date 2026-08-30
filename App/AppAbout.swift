import AppKit

/// The standard macOS About panel, populated with the app version, a one-line
/// description, and a link to the project page. Menu-bar (accessory) apps must
/// activate first or the panel opens behind other windows.
enum AppAbout {

    @MainActor
    static func show() {
        NSApp.activate(ignoringOtherApps: true)

        let notes = NSMutableAttributedString(
            string: "See how much of your LLM subscription quota you have left — Claude, "
                + "ChatGPT, Grok, DeepInfra, and Kimi — in one place.\n\n",
            attributes: [
                .font: NSFont.systemFont(ofSize: 11),
                .foregroundColor: NSColor.secondaryLabelColor,
            ]
        )
        if let url = URL(string: "https://ubyjvovk.github.io") {
            notes.append(NSAttributedString(
                string: "ubyjvovk.github.io",
                attributes: [.link: url, .font: NSFont.systemFont(ofSize: 11)]
            ))
        }

        NSApp.orderFrontStandardAboutPanel(options: [
            .applicationName: "Quota Monitor",
            .credits: notes,
        ])
    }
}
