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
  property bool openedFromHotkey: false
  readonly property var barIdentity: hostWidget || root

  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color urgent: bar ? bar.urgent : Color.urgent
  readonly property color dim: Qt.darker(foreground, 1.55)
  readonly property color track: Style.selectedFillFor(foreground, Color.accent)
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family

  property var snapshot: null
  property string lastError: ""
  property bool refreshing: false
  property bool pendingFresh: false
  property double nowMs: Date.now()

  readonly property string binary: String(setting("exec", "quotamon"))
  readonly property int refreshIntervalSec: Math.max(30, Number(setting("refreshIntervalSec", 300)))
  readonly property var rows: Model.providerRows(snapshot, nowMs)
  readonly property var iconBars: Model.iconBars(snapshot, nowMs)
  readonly property var headlinePercent: Model.tightestPercent(snapshot, nowMs)
  readonly property string severity: Model.severity(headlinePercent)
  readonly property bool alarming: severity === "critical"
  readonly property string label: "󰓅"

  function clamp(value, lo, hi) { return Math.max(lo, Math.min(hi, value)) }

  function open() {
    openedFromHotkey = false
    setCenterHoverRevealSuppressed(false)
    root.controller.show()
  }

  function openFromHotkey() {
    openedFromHotkey = true
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

  function fetchCommand(fresh) {
    var flags = fresh ? " --json --fresh" : " --json"
    return "PATH=\"$HOME/.local/bin:$PATH\" " + root.binary + flags
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
    refreshing = true
    fetchProc.command = ["bash", "-lc", fetchCommand(fresh === true)]
    fetchProc.running = true
  }

  function applyOutput(raw) {
    var parsed = Model.parseSnapshot(raw)
    if (parsed) {
      snapshot = parsed
      lastError = ""
      return
    }
    var text = String(raw || "").trim()
    if (text !== "") lastError = text
  }

  onOpenedChanged: if (opened) {
    nowMs = Date.now()
    Qt.callLater(function() { keyCatcher.forceActiveFocus() })
  }

  Process {
    id: fetchProc
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyOutput(text)
    }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        var err = String(text || "").trim()
        if (err !== "") root.lastError = err
      }
    }
    onExited: function() {
      root.refreshing = false
      if (root.pendingFresh) {
        root.pendingFresh = false
        Qt.callLater(function() { root.refreshNow(true) })
      }
    }
  }

  Timer {
    interval: root.refreshIntervalSec * 1000
    running: true
    repeat: true
    triggeredOnStart: true
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
    centerOnBar: true
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

          Repeater {
            model: root.rows

            Column {
              required property var modelData
              width: column.width
              spacing: Style.space(10)

              PanelHero {
                width: parent.width
                title: modelData.displayName
                meta: Model.providerMeta(modelData)
                foreground: root.foreground
                fontFamily: root.fontFamily
                iconComponent: Component {
                  Text {
                    text: "󰓅"
                    color: root.foreground
                    font.family: root.fontFamily
                    font.pixelSize: Style.font.display
                  }
                }
              }

              Text {
                visible: modelData.statusState !== "ok" && modelData.statusMessage !== ""
                width: parent.width
                text: modelData.statusMessage
                color: root.urgent
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                wrapMode: Text.WordWrap
              }

              Repeater {
                model: modelData.windows

                WindowRow {
                  required property var modelData
                  width: column.width
                  window: modelData
                }
              }

              Text {
                visible: modelData.creditsText !== ""
                width: parent.width
                text: modelData.creditsText
                color: root.dim
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
              }
            }
          }

          Text {
            visible: root.rows.length === 0
            width: parent.width
            topPadding: Style.space(12)
            text: root.lastError !== ""
              ? root.lastError
              : "No quota readings yet.\nRun `quotamon setup` if this is the first time."
            color: root.dim
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.WordWrap
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
        }
      }
    }
  }

  component WindowRow: Column {
    id: windowRow
    property var window: null
    spacing: Style.space(6)

    Item {
      width: parent.width
      implicitHeight: Math.max(label.implicitHeight, value.implicitHeight)

      Text {
        id: label
        text: windowRow.window ? windowRow.window.label : ""
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.body
        elide: Text.ElideRight
        anchors.left: parent.left
        anchors.right: value.left
        anchors.rightMargin: Style.spacing.sm
        anchors.verticalCenter: parent.verticalCenter
      }

      Text {
        id: value
        text: windowRow.window ? windowRow.window.percentText : "—"
        color: windowRow.window && windowRow.window.alarming ? root.urgent : root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
      }
    }

    Item {
      width: parent.width
      implicitHeight: Math.max(Style.space(4), Math.round(Style.spacing.controlHeight * 0.14))
      visible: windowRow.window && windowRow.window.percent !== null

      Rectangle {
        id: meterTrack
        anchors.fill: parent
        radius: height / 2
        color: root.track
      }

      Rectangle {
        anchors.left: meterTrack.left
        anchors.verticalCenter: meterTrack.verticalCenter
        height: meterTrack.height
        radius: meterTrack.radius
        width: meterTrack.width * root.clamp((windowRow.window && windowRow.window.percent !== null ? windowRow.window.percent : 0) / 100, 0, 1)
        color: windowRow.window && windowRow.window.alarming ? root.urgent : root.foreground
      }
    }

    Text {
      visible: text !== ""
      width: parent.width
      text: windowRow.window ? windowRow.window.resetText : ""
      color: root.dim
      font.family: root.fontFamily
      font.pixelSize: Style.font.caption
    }
  }
}
