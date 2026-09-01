import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model

Panel {
  id: root
  moduleName: "quotamon"
  ipcTarget: "quotamon"
  manageIpc: false

  property var anchorItem: null
  property var hostWidget: null
  readonly property var barIdentity: hostWidget || root

  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color urgent: bar ? bar.urgent : root.toneColor("critical")
  readonly property color dim: root.toneColor("dim")
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family

  property var snapshot: null
  property string lastError: ""
  property bool refreshing: false
  property bool pendingFresh: false
  property bool timedOut: false
  property bool installTimedOut: false
  property double nowMs: Date.now()
  property var manifest: Model.parseManifest("")

  readonly property string manifestUrl: String(Qt.resolvedUrl("manifest.json"))

  // Qt.resolvedUrl anchors the shipped script to this QML file, independent of
  // the shell's working directory. Process needs a filesystem path, so decode
  // the local file URL and strip only its fixed file:// scheme.
  readonly property string installScript: decodeURIComponent(
    String(Qt.resolvedUrl("fetch-quotamon.sh")).replace(/^file:\/\//, "")
  )

  readonly property string binary: {
    var value = String(setting("exec", "quotamon")).trim()
    return value === "" ? "quotamon" : value
  }
  readonly property int refreshIntervalSec: {
    var value = Number(setting("refreshIntervalSec", 300))
    return isFinite(value) ? Math.min(3600, Math.max(30, Math.round(value))) : 300
  }
  readonly property var rows: Model.providerRows(snapshot, nowMs)
  readonly property var iconBars: Model.iconBars(snapshot, nowMs)
  readonly property var headlinePercent: Model.tightestPercent(snapshot, nowMs)
  readonly property string severity: Model.severity(headlinePercent)
  readonly property bool alarming: severity === "critical"
  function clamp(value, lo, hi) { return Math.max(lo, Math.min(hi, value)) }
  function toneColor(tone) {
    if (tone === "critical") return Color.urgent
    if (tone === "warning") return Color.accent
    if (tone === "dim") return Color.muted
    return root.foreground
  }

  function loadManifest() {
    var request = new XMLHttpRequest()
    request.onreadystatechange = function() {
      if (request.readyState !== XMLHttpRequest.DONE)
        return
      root.manifest = request.status === 0 || request.status === 200
        ? Model.parseManifest(request.responseText)
        : Model.parseManifest("")
    }
    request.open("GET", root.manifestUrl)
    request.send()
  }

  Component.onCompleted: loadManifest()

  function openFromHotkey() {
    root.controller.show()
    Qt.callLater(function() {
      if (root.opened) setCenterHoverRevealSuppressed(true)
    })
  }

  function close() {
    setCenterHoverRevealSuppressed(false)
    root.controller.hide()
  }

  function toggle() {
    if (root.opened) root.close()
    else root.openFromHotkey()
  }

  function switchPanel(direction) {
    if (root.bar && typeof root.bar.switchPanelFrom === "function")
      return root.bar.switchPanelFrom(root.barIdentity, direction)
    return false
  }

  function setCenterHoverRevealSuppressed(value) {
    if (root.bar && "centerHoverRevealSuppressed" in root.bar)
      root.bar.centerHoverRevealSuppressed = value
  }

  function refresh() {
    refreshNow(true)
  }

  function refreshNow(fresh) {
    if (fetchProc.running) {
      pendingFresh = pendingFresh || fresh === true
      return
    }
    lastError = ""
    timedOut = false
    refreshing = true
    fetchProc.stdoutBuf = ""
    fetchProc.stderrBuf = ""
    fetchProc.currentFresh = fresh === true
    fetchProc.command = fetchProc.currentFresh
      ? [root.binary, "--json", "--fresh"]
      : [root.binary, "--json"]
    fetchProc.running = true
    // Arm the 30 s watchdog for this fetch; disarmed on normal exit below.
    watchdog.restart()
  }

  function installQuotamon() {
    if (installProc.running)
      return
    lastError = ""
    installTimedOut = false
    installProc.stdoutBuf = ""
    installProc.stderrBuf = ""
    // argv execution with this fixed, plugin-local path performs no shell
    // interpolation of settings or other user-controlled values.
    var command = ["bash", root.installScript]
    var pinned = Model.pinnedVersion(root.manifest)
    if (pinned !== "")
      command.push(pinned)
    installProc.command = command
    installProc.running = true
    installWatchdog.restart()
  }

  function applyOutput(raw) {
    var parsed = Model.parseSnapshot(raw)
    if (parsed) {
      snapshot = parsed
      lastError = ""
      return
    }
    lastError = "unparseable quotamon output: " + String(raw || "").slice(0, 200)
  }

  onOpenedChanged: {
    if (opened) {
      nowMs = Date.now()
      Qt.callLater(function() { keyCatcher.forceActiveFocus() })
    } else {
      setCenterHoverRevealSuppressed(false)
    }
  }

  onRowsChanged: {
    if (opened) {
      var preservedY = panelFlick.contentY
      Qt.callLater(function() {
        var maximumY = Math.max(0, panelFlick.contentHeight - panelFlick.height)
        panelFlick.contentY = root.clamp(preservedY, 0, maximumY)
      })
    }
  }

  Process {
    id: fetchProc
    property string stdoutBuf: ""
    property string stderrBuf: ""
    property bool currentFresh: false
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: fetchProc.stdoutBuf = String(text || "")
    }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: fetchProc.stderrBuf = String(text || "")
    }
    onExited: function(exitCode, exitStatus) {
      watchdog.stop()
      root.refreshing = false
      // If the watchdog already reported a hung fetch (timedOut), the kill
      // below surfaces here as a non-zero exit; don't overwrite its message.
      if (root.timedOut)
        return
      if (exitCode === 0)
        root.applyOutput(fetchProc.stdoutBuf)
      else
        root.lastError = fetchProc.stderrBuf.trim() || ("quotamon exited " + exitCode)

      var requeueFresh = root.pendingFresh && !fetchProc.currentFresh
      root.pendingFresh = false
      if (requeueFresh) {
        Qt.callLater(function() { root.refreshNow(true) })
      }
    }
  }

  Process {
    id: installProc
    property string stdoutBuf: ""
    property string stderrBuf: ""
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: installProc.stdoutBuf = String(text || "")
    }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: installProc.stderrBuf = String(text || "")
    }
    onExited: function(exitCode, exitStatus) {
      installWatchdog.stop()
      if (root.installTimedOut)
        return
      if (exitCode === 0) {
        root.refreshNow(true)
        return
      }
      var output = (installProc.stdoutBuf + "\n" + installProc.stderrBuf).trim()
      root.lastError = output === ""
        ? ("quotamon installation exited " + exitCode)
        : output.slice(-800)
    }
  }

  // Watchdog (F12): nothing else bounds a fetch — a child so hung it never
  // exits would leave refreshing=true and disable Refresh forever. After 30 s
  // we hard-kill the child (Quickshell Process exposes no SIGTERM method, so
  // running = false is the force-kill, matching how refreshNow starts it), keep
  // the last good snapshot, and report the timeout. Disarmed on normal onExited.
  Timer {
    id: watchdog
    interval: 30000
    onTriggered: {
      if (!fetchProc.running)
        return
      root.timedOut = true
      fetchProc.running = false
      root.refreshing = false
      root.lastError = "quotamon timed out after 30s"
    }
  }

  // Release downloads may legitimately take longer than a quota refresh, but
  // a stuck installer must not leave its button disabled forever.
  Timer {
    id: installWatchdog
    interval: 120000
    onTriggered: {
      if (!installProc.running)
        return
      root.installTimedOut = true
      installProc.running = false
      root.lastError = "quotamon installation timed out after 120s"
    }
  }

  Timer {
    interval: root.refreshIntervalSec * 1000
    running: true
    repeat: true
    onTriggered: root.refreshNow(false)
  }

  Timer {
    interval: 1000
    running: true
    onTriggered: root.refreshNow(false)
  }

  Timer {
    interval: 30000
    running: true
    repeat: true
    onTriggered: root.nowMs = Date.now()
  }

  IpcHandler {
    target: root.ipcTarget
    function open(): void { root.openFromHotkey() }
    function close(): void { root.close() }
    function show(): void { root.openFromHotkey() }
    function hide(): void { root.close() }
    function toggle(): void { root.toggle() }
    function refresh(): string { root.refresh(); return "ok" }
  }

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root.barIdentity
    bar: root.bar
    open: root.opened
    // Anchored popouts follow their icon's bar section; centring is the fallback for a missing anchor only.
    centerOnBar: root.anchorItem === null
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(380))
    contentHeight: panel.fittedContentHeight(column.implicitHeight, Style.space(640))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onActivateRequested: root.refresh()
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }
      onTextKey: function(t) { if (t === "r" || t === "R") root.refresh() }

      Flickable {
        id: panelFlick
        anchors.fill: parent
        contentWidth: width
        contentHeight: column.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        flickableDirection: Flickable.VerticalFlick
        interactive: contentHeight > height

        Column {
          id: column
          width: panelFlick.width
          spacing: Style.space(12)

          Text {
            visible: root.lastError !== ""
            width: parent.width
            text: root.lastError
            color: root.toneColor("critical")
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Text {
            id: consoleLineMeasure
            visible: false
            text: "M"
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
          }

          // Model.consoleLines is the only source of layout for this console table.
          Column {
            id: consoleTable
            width: column.width
            spacing: 0

            Repeater {
              model: Model.consoleLines(root.snapshot, root.nowMs)

              Item {
                id: consoleLine
                required property var modelData
                readonly property var spans: modelData.spans
                width: consoleTable.width
                height: spans.length === 0
                  ? consoleLineMeasure.implicitHeight
                  : consoleRow.implicitHeight

                Row {
                  id: consoleRow
                  spacing: 0

                  Repeater {
                    model: consoleLine.spans

                    Text {
                      required property var modelData
                      text: modelData.text
                      color: root.toneColor(modelData.tone)
                      font.family: root.fontFamily
                      font.pixelSize: Style.font.body
                    }
                  }
                }
              }
            }
          }

          Text {
            visible: root.rows.length === 0
            width: parent.width
            topPadding: Style.space(12)
            text: "No quota readings yet.\nRun `quotamon setup` if this is the first time."
            color: root.toneColor("dim")
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
            horizontalAlignment: Text.AlignHCenter
          }

          Button {
            visible: root.rows.length === 0
            width: parent.width
            text: installProc.running ? "Installing…" : "Install quotamon"
            iconText: "󰇚"
            iconSpinning: installProc.running
            bordered: true
            enabled: !installProc.running
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: root.installQuotamon()
          }

          PanelSeparator {
            foreground: root.foreground
          }

          Button {
            width: parent.width
            text: root.refreshing ? "Refreshing…" : "Refresh"
            iconText: "󰑐"
            iconSpinning: root.refreshing
            bordered: true
            enabled: !root.refreshing
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: root.refresh()
          }

          Text {
            visible: text !== ""
            width: parent.width
            text: Model.versionWarning(root.manifest, root.snapshot)
            color: root.toneColor("critical")
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Button {
            visible: Model.coreUpdateVersion(root.manifest, root.snapshot) !== ""
            width: parent.width
            text: installProc.running ? "Updating…" : "Update quotamon to " + Model.coreUpdateVersion(root.manifest, root.snapshot)
            iconText: "󰇚"
            iconSpinning: installProc.running
            bordered: true
            enabled: !installProc.running
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: root.installQuotamon()
          }

          Text {
            visible: text !== ""
            width: parent.width
            text: Model.aboutText(root.manifest, root.snapshot)
            color: root.toneColor("dim")
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.WordWrap
          }
        }
      }
    }
  }

}
