function parseSnapshot(raw) {
  try {
    var data = JSON.parse(String(raw || ""))
    if (!data || typeof data !== "object") return null
    if (!Array.isArray(data.providers)) data.providers = []
    return data
  } catch (e) {
    return null
  }
}

function parseManifest(raw) {
  var empty = { version: "", minQuotamon: "" }
  try {
    var data = JSON.parse(String(raw || ""))
    if (!data || typeof data !== "object" || typeof data.version !== "string") return empty
    var version = data.version.trim()
    if (version === "") return empty
    var requirement = data.dependencies && data.dependencies.quotamon
    var minQuotamon = typeof requirement === "string" ? requirement.trim() : ""
    if (minQuotamon.slice(0, 2) === ">=") minQuotamon = minQuotamon.slice(2).trim()
    else if (minQuotamon !== "") minQuotamon = ""
    return { version: version, minQuotamon: minQuotamon }
  } catch (e) {
    return empty
  }
}

function compareVersions(a, b) {
  if (!a || !b) return null
  var left = String(a).split(".")
  var right = String(b).split(".")
  for (var i = 0; i < left.length; i++) {
    if (!/^\d+$/.test(left[i])) return null
  }
  for (var j = 0; j < right.length; j++) {
    if (!/^\d+$/.test(right[j])) return null
  }
  var length = Math.max(left.length, right.length)
  for (var k = 0; k < length; k++) {
    var leftSegment = k < left.length ? Number(left[k]) : 0
    var rightSegment = k < right.length ? Number(right[k]) : 0
    if (leftSegment < rightSegment) return -1
    if (leftSegment > rightSegment) return 1
  }
  return 0
}

function coreVersion(snapshot) {
  if (!snapshot || snapshot.version === null || snapshot.version === undefined) return ""
  return String(snapshot.version).trim()
}

function aboutText(manifest, snapshot) {
  var version = manifest && manifest.version ? String(manifest.version).trim() : ""
  if (version === "") return ""
  var core = coreVersion(snapshot)
  return "Quota Monitor " + version + " · quotamon " + (core || "version unknown")
}

function versionWarning(manifest, snapshot) {
  if (snapshot === null || snapshot === undefined) return ""
  var minimum = manifest && manifest.minQuotamon ? String(manifest.minQuotamon).trim() : ""
  if (minimum === "") return ""
  var core = coreVersion(snapshot)
  if (core === "") {
    return "quotamon is older than this plugin needs (" + minimum + ") — update it and press Refresh"
  }
  var comparison = compareVersions(core, minimum)
  if (comparison === null || comparison >= 0) return ""
  return "quotamon " + core + " is older than this plugin needs (" + minimum + ") — update it and press Refresh"
}

function currentUsedPercent(window, nowMs) {
  if (!window) return null
  if (window.resetsAt) {
    var resetMs = Date.parse(window.resetsAt)
    if (isFinite(resetMs) && resetMs <= nowMs) return null
  }
  var percent = Number(window.usedPercent)
  return isFinite(percent) ? percent : null
}

function kindRank(kind) {
  if (kind === "session") return 0
  if (kind === "weekly") return 1
  if (kind === "monthly") return 2
  return 3
}

function sortedWindows(provider, nowMs) {
  var windows = provider && Array.isArray(provider.windows) ? provider.windows.slice() : []
  windows.sort(function(left, right) {
    var leftPercent = currentUsedPercent(left, nowMs)
    var rightPercent = currentUsedPercent(right, nowMs)
    var leftCurrent = leftPercent !== null
    var rightCurrent = rightPercent !== null
    if (leftCurrent !== rightCurrent) return leftCurrent ? -1 : 1
    if (leftCurrent && leftPercent !== rightPercent) return rightPercent - leftPercent
    return kindRank(left && left.kind) - kindRank(right && right.kind)
  })
  return windows
}

function tightestPercent(snapshot, nowMs) {
  var bestWindow = null
  var bestPercent = null
  var providers = snapshot && snapshot.providers ? snapshot.providers : []
  for (var i = 0; i < providers.length; i++) {
    var windows = providers[i] && providers[i].windows ? providers[i].windows : []
    for (var j = 0; j < windows.length; j++) {
      var window = windows[j]
      var percent = currentUsedPercent(window, nowMs)
      if (percent === null) continue
      if (bestWindow === null || percent > bestPercent ||
          (percent === bestPercent && kindRank(window && window.kind) < kindRank(bestWindow.kind))) {
        bestWindow = window
        bestPercent = percent
      }
    }
  }
  return bestPercent
}

function severity(percent) {
  if (percent === null || percent === undefined) return "unavailable"
  if (percent < 70) return "normal"
  if (percent < 90) return "warning"
  return "critical"
}

function formatPercent(percent) {
  if (percent === null || percent === undefined || !isFinite(Number(percent))) return "—"
  return Math.round(Number(percent)) + "%"
}

function formatCountdown(resetsAt, nowMs) {
  if (!resetsAt) return ""
  var resetMs = Date.parse(resetsAt)
  if (!isFinite(resetMs)) return ""
  var remaining = resetMs - nowMs
  if (remaining <= 0) return "window reset"
  var total = Math.floor(remaining / 1000)
  var days = Math.floor(total / (24 * 60 * 60))
  var hours = Math.floor((total % (24 * 60 * 60)) / (60 * 60))
  var minutes = Math.floor((total % (60 * 60)) / 60)
  if (days > 0) return "resets in " + days + "d " + hours + "h"
  if (hours > 0) return "resets in " + hours + "h " + minutes + "m"
  if (minutes > 0) return "resets in " + minutes + "m"
  return "resets in <1m"
}

function formatAge(observedAt, nowMs) {
  if (!observedAt) return ""
  var observedMs = Date.parse(observedAt)
  if (!isFinite(observedMs)) return ""
  var total = Math.max(0, Math.floor((nowMs - observedMs) / 1000))
  if (total < 5) return "just now"
  if (total < 60) return total + "s ago"
  if (total < 60 * 60) return Math.floor(total / 60) + "m ago"
  if (total < 24 * 60 * 60) return Math.floor(total / (60 * 60)) + "h ago"
  return Math.floor(total / (24 * 60 * 60)) + "d ago"
}

function hasValue(object, key) {
  return object && object[key] !== null && object[key] !== undefined
}

function isCurrencyOrWhitespace(character) {
  if (/\s/.test(character)) return true
  var code = character.charCodeAt(0)
  return code === 0x24 || (code >= 0xa2 && code <= 0xa5) || code === 0x58f ||
    code === 0x60b || (code >= 0x7fe && code <= 0x7ff) ||
    (code >= 0x9f2 && code <= 0x9f3) || code === 0x9fb || code === 0xaf1 ||
    code === 0xbf9 || code === 0xe3f || code === 0x17db ||
    (code >= 0x20a0 && code <= 0x20c0) || code === 0xa838 ||
    code === 0xfdfc || code === 0xfe69 || code === 0xff04 ||
    (code >= 0xffe0 && code <= 0xffe1) || (code >= 0xffe5 && code <= 0xffe6)
}

function disabledCreditBalanceIsEmptyOrZero(balance) {
  var text = String(balance)
  while (text.length > 0 && isCurrencyOrWhitespace(text.charAt(0))) text = text.slice(1)
  while (text.length > 0 && isCurrencyOrWhitespace(text.charAt(text.length - 1))) text = text.slice(0, -1)
  if (text === "") return true
  text = text.split(",").join(".")
  if (!/^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/.test(text)) return false
  var amount = Number(text)
  return isFinite(amount) && amount === 0
}

function tableCredits(credits) {
  if (credits.unlimited) {
    if (hasValue(credits, "balance")) return String(credits.balance)
    return "unlimited"
  }
  if (credits.enabled) {
    if (hasValue(credits, "balance")) return String(credits.balance) + " remaining"
    return "— remaining"
  }
  if (!hasValue(credits, "balance") || disabledCreditBalanceIsEmptyOrZero(credits.balance)) return null
  return String(credits.balance) + " (not enabled)"
}

function creditsText(credits) {
  if (!credits) return ""
  var parts = []
  if (hasValue(credits, "spend") && !credits.unlimited) {
    var balance = tableCredits(credits)
    if (balance !== null) parts.push("balance " + balance)
    parts.push("spend " + String(credits.spend))
    return parts.join(" · ")
  }
  var detail = tableCredits(credits)
  if (detail === null) return ""
  var label = credits.unlimited && hasValue(credits, "balance") ? "spend" : "credits"
  parts.push(label + " " + detail)
  return parts.join(" · ")
}

function originLabel(origin) {
  if (origin === "live") return "live"
  if (origin === "local") return "cached"
  return "unavailable"
}

function providerRows(snapshot, nowMs) {
  var providers = snapshot && snapshot.providers ? snapshot.providers : []
  var rows = []
  for (var i = 0; i < providers.length; i++) {
    var provider = providers[i] || {}
    var windows = []
    var list = sortedWindows(provider, nowMs)
    for (var j = 0; j < list.length; j++) {
      var window = list[j] || {}
      var percent = currentUsedPercent(window, nowMs)
      windows.push({
        id: String(window.id || j),
        label: String(window.label || "Usage"),
        kind: String(window.kind || ""),
        percent: percent,
        percentText: formatPercent(percent),
        resetText: formatCountdown(window.resetsAt, nowMs),
        alarming: severity(percent) === "critical"
      })
    }
    var status = provider.status || {}
    rows.push({
      id: String(provider.id || i),
      displayName: String(provider.displayName || provider.id || "Provider"),
      plan: String(provider.plan || ""),
      origin: originLabel(provider.origin),
      ageText: formatAge(provider.observedAt, nowMs),
      statusState: String(status.state || "ok"),
      statusMessage: String(status.message || ""),
      windows: windows,
      creditsText: creditsText(provider.credits)
    })
  }
  return rows
}

function providerMeta(row) {
  if (!row) return ""
  var parts = []
  if (row.plan) parts.push(row.plan)
  if (row.origin) parts.push(row.origin)
  if (row.ageText) parts.push(row.ageText)
  return parts.join(" · ")
}

function tightestForRow(row) {
  if (!row) return null
  var best = null
  var windows = row.windows || []
  for (var i = 0; i < windows.length; i++) {
    var window = windows[i]
    if (!window) continue
    var percent = window.percent
    if (percent === null || percent === undefined) continue
    if (best === null || percent > best.percent ||
        (percent === best.percent && kindRank(window.kind) < kindRank(best.kind)))
      best = window
  }
  return best
}

// One bar per provider in snapshot order, matching the panel and macOS glyph.
function iconBars(snapshot, nowMs) {
  var rows = providerRows(snapshot, nowMs)
  var bars = []
  for (var i = 0; i < rows.length; i++) {
    var tightest = tightestForRow(rows[i])
    var percent = tightest ? tightest.percent : null
    bars.push({
      id: rows[i].id,
      percent: percent,
      severity: severity(percent)
    })
  }
  return bars
}
