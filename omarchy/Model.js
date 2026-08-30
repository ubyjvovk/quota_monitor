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

function currentUsedPercent(window, nowMs) {
  if (!window) return null
  if (window.resetsAt) {
    var resetMs = Date.parse(window.resetsAt)
    if (isFinite(resetMs) && resetMs <= nowMs) return null
  }
  var percent = Number(window.usedPercent)
  return isFinite(percent) ? percent : null
}

function tightestPercent(snapshot, nowMs) {
  var best = null
  var providers = snapshot && snapshot.providers ? snapshot.providers : []
  for (var i = 0; i < providers.length; i++) {
    var windows = providers[i].windows || []
    for (var j = 0; j < windows.length; j++) {
      var percent = currentUsedPercent(windows[j], nowMs)
      if (percent === null) continue
      if (best === null || percent > best) best = percent
    }
  }
  return best
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
  var minutes = Math.floor(remaining / 60000)
  var hours = Math.floor(minutes / 60)
  var days = Math.floor(hours / 24)
  if (days > 0) return "resets in " + days + "d " + (hours % 24) + "h"
  if (hours > 0) return "resets in " + hours + "h " + (minutes % 60) + "m"
  return "resets in " + Math.max(1, minutes) + "m"
}

function formatAge(observedAt, nowMs) {
  if (!observedAt) return ""
  var observedMs = Date.parse(observedAt)
  if (!isFinite(observedMs)) return ""
  var ago = nowMs - observedMs
  if (ago < 15000) return "just now"
  var minutes = Math.floor(ago / 60000)
  if (minutes < 1) return "just now"
  if (minutes < 60) return minutes + "m ago"
  var hours = Math.floor(minutes / 60)
  if (hours < 24) return hours + "h ago"
  return Math.floor(hours / 24) + "d ago"
}

function creditsText(credits) {
  if (!credits) return ""
  var parts = []
  if (credits.balance) {
    var line = String(credits.balance)
    if (credits.enabled === false) line += " (not enabled)"
    parts.push(line)
  }
  if (credits.spend) parts.push(String(credits.spend))
  return parts.join(" · ")
}

function providerRows(snapshot, nowMs) {
  var providers = snapshot && snapshot.providers ? snapshot.providers : []
  var rows = []
  for (var i = 0; i < providers.length; i++) {
    var provider = providers[i] || {}
    var windows = []
    var list = provider.windows || []
    for (var j = 0; j < list.length; j++) {
      var window = list[j] || {}
      var percent = currentUsedPercent(window, nowMs)
      windows.push({
        id: String(window.id || j),
        label: String(window.label || "Usage"),
        percent: percent,
        percentText: formatPercent(percent),
        resetText: formatCountdown(window.resetsAt, nowMs),
        alarming: percent !== null && percent >= 90
      })
    }
    var status = provider.status || {}
    rows.push({
      id: String(provider.id || i),
      displayName: String(provider.displayName || provider.id || "Provider"),
      plan: String(provider.plan || ""),
      origin: String(provider.origin || ""),
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
    var percent = windows[i].percent
    if (percent === null || percent === undefined) continue
    if (best === null || percent > best) best = percent
  }
  return best
}

// One bar per provider, tightest current window, most constrained first.
// Matches the macOS menu-bar glyph in quota_monitor.
function iconBars(snapshot, nowMs) {
  var rows = providerRows(snapshot, nowMs)
  var bars = []
  for (var i = 0; i < rows.length; i++) {
    var percent = tightestForRow(rows[i])
    bars.push({
      id: rows[i].id,
      percent: percent,
      severity: severity(percent)
    })
  }
  bars.sort(function(a, b) {
    var ap = a.percent === null || a.percent === undefined ? -1 : a.percent
    var bp = b.percent === null || b.percent === undefined ? -1 : b.percent
    return bp - ap
  })
  return bars
}
