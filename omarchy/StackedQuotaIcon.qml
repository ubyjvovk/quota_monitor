import QtQuick
import qs.Commons

// Status-bar glyph from quota_monitor: one thin capsule per provider,
// stacked, filled to that provider's tightest window and coloured by
// severity. An empty track means no current reading.
Item {
  id: root

  property var bars: []
  property color trackColor: Qt.rgba(Color.foreground.r, Color.foreground.g, Color.foreground.b, 0.22)
  property color normalColor: "#879A39"
  property color warningColor: "#d0772b"
  property color criticalColor: Color.urgent

  readonly property var modelBars: {
    var list = root.bars || []
    return list.length > 0 ? list : [{ percent: null, severity: "unavailable" }]
  }

  function fillColor(severity) {
    if (severity === "critical") return root.criticalColor
    if (severity === "warning") return root.warningColor
    if (severity === "normal") return root.normalColor
    return "transparent"
  }

  function clamp01(value) {
    return Math.max(0, Math.min(1, value))
  }

  Column {
    id: column
    anchors.fill: parent
    spacing: root.modelBars.length > 1 ? 1 : 0

    Repeater {
      model: root.modelBars

      Item {
        required property var modelData
        required property int index

        width: column.width
        height: {
          var count = Math.max(1, root.modelBars.length)
          return Math.max(1, (column.height - column.spacing * (count - 1)) / count)
        }

        Rectangle {
          anchors.fill: parent
          radius: Math.min(height / 2, 1.5)
          color: root.trackColor
        }

        Rectangle {
          visible: modelData.percent !== null && modelData.percent !== undefined && modelData.percent > 0
          anchors.left: parent.left
          anchors.top: parent.top
          anchors.bottom: parent.bottom
          width: parent.width * root.clamp01(Number(modelData.percent) / 100)
          radius: Math.min(height / 2, 1.5)
          color: root.fillColor(modelData.severity)
        }
      }
    }
  }
}
