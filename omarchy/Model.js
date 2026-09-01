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

// Only a release CalVer from the manifest is safe to pass to the installer.
function pinnedVersion(manifest) {
  var version = manifest && typeof manifest.version === "string" ? manifest.version : ""
  return /^[0-9]{4}\.(1[0-2]|[1-9])\.[0-9]+$/.test(version) ? version : ""
}

// An update is offered for a release core that is behind the manifest pin. Note
// the asymmetry between an *absent* version and an *unparseable* one — they must
// not collapse into one branch. A non-null snapshot without a `version` key
// predates the 2026.9.1 key, i.e. it is an old release worth offering an update
// for; a version that is present but unparseable, like "dev", is a deliberate
// local build that must never be nagged to overwrite itself. No snapshot at all
// (null/undefined) is the Install button's territory, not ours.
function coreUpdateVersion(manifest, snapshot) {
  var version = pinnedVersion(manifest)
  if (version === "") return ""
  var core = coreVersion(snapshot)
  if (core === "") {
    return snapshot && typeof snapshot === "object" ? version : ""
  }
  var comparison = compareVersions(core, version)
  return comparison !== null && comparison < 0 ? version : ""
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
    return "quotamon is older than this plugin needs (" + minimum + ") — use the Update button below"
  }
  var comparison = compareVersions(core, minimum)
  if (comparison === null || comparison >= 0) return ""
  return "quotamon " + core + " is older than this plugin needs (" + minimum + ") — use the Update button below"
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

// The two-to-three-letter identity used by the Mac menu bar and Waybar.
function providerBadge(row) {
  var badges = {
    claude: "CL",
    codex: "GPT",
    grok: "GK",
    deepinfra: "DI",
    kimi: "KM",
    runinfra: "RI",
    openrouter: "OR",
    deepseek: "DS"
  }
  var id = row && row.id ? String(row.id) : ""
  if (badges[id]) return badges[id]
  var displayName = row && row.displayName ? String(row.displayName) : ""
  return displayName.slice(0, 2).toUpperCase()
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

// The compact label reuses resetText so its wording cannot drift from it.
function countdownText(window, nowMs) {
  if (!window) return ""
  var resetText = typeof window.resetText === "string"
    ? window.resetText
    : formatCountdown(window.resetsAt, nowMs)
  var prefix = "resets in "
  return resetText.slice(0, prefix.length) === prefix
    ? resetText.slice(prefix.length)
    : ""
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

function runeCount(value) {
  return Array.from(String(value)).length
}

function padTableCell(value, width) {
  var text = String(value)
  var padding = width - runeCount(text)
  return padding > 0 ? text + " ".repeat(padding) : text
}

function truncateAndPadTableCell(value, width) {
  var runes = Array.from(String(value))
  if (runes.length > width) return padTableCell(runes.slice(0, width - 1).join("") + "…", width)
  return padTableCell(value, width)
}

function tableCountdown(window, nowMs) {
  if (!window || !window.resetsAt) return "—"
  var formatted = formatCountdown(window.resetsAt, nowMs)
  if (formatted === "window reset") return "reset"
  var prefix = "resets in "
  return formatted.slice(0, prefix.length) === prefix ? formatted.slice(prefix.length) : "—"
}

function tableTone(percent) {
  var value = severity(percent)
  if (value === "warning") return "warning"
  if (value === "critical") return "critical"
  return "plain"
}

function appendConsoleSpan(spans, text, tone) {
  if (text === "") return
  var last = spans.length > 0 ? spans[spans.length - 1] : null
  if (last && last.tone === tone) last.text += text
  else spans.push({ text: text, tone: tone })
}

function consoleWindowLine(window, nowMs) {
  var used = currentUsedPercent(window, nowMs)
  var percent = formatPercent(used)
  var percentPadding = Math.max(0, 4 - runeCount(percent))
  var filled = used === null ? 0 : Math.min(20, Math.max(0, Math.round(used / 5)))
  var tone = tableTone(used)
  var spans = []

  appendConsoleSpan(spans, "  " + truncateAndPadTableCell(window.label, 9) + " ", "plain")
  appendConsoleSpan(spans, "█".repeat(filled), tone)
  appendConsoleSpan(spans, "░".repeat(20 - filled), "dim")
  appendConsoleSpan(spans, " " + " ".repeat(percentPadding), "plain")
  appendConsoleSpan(spans, percent, used === null ? "plain" : tone)
  appendConsoleSpan(spans, "  " + tableCountdown(window, nowMs), "plain")
  return { spans: spans }
}

function creditLines(credits) {
  if (hasValue(credits, "spend") && !credits.unlimited) {
    var lines = []
    var balance = tableCredits(credits)
    if (balance !== null) lines.push("  " + padTableCell("balance", 9) + " " + balance)
    lines.push("  " + padTableCell("spend", 9) + " " + String(credits.spend))
    return lines
  }
  var detail = tableCredits(credits)
  if (detail === null) return []
  var label = credits.unlimited && hasValue(credits, "balance") ? "spend" : "credits"
  return ["  " + padTableCell(label, 9) + " " + detail]
}

function consoleProviderLines(provider, nowMs) {
  provider = provider || {}
  var plan = hasValue(provider, "plan") ? String(provider.plan) : "—"
  var header = padTableCell(provider.displayName || "", 12) + " " +
    padTableCell(plan, 14) + " " + originLabel(provider.origin) + " · " +
    formatAge(provider.observedAt, nowMs)
  var lines = [{ spans: [{ text: header, tone: "plain" }] }]
  var windows = sortedWindows(provider, nowMs)
  for (var i = 0; i < windows.length; i++) lines.push(consoleWindowLine(windows[i] || {}, nowMs))

  if (provider.credits) {
    var credits = creditLines(provider.credits)
    for (var j = 0; j < credits.length; j++) {
      lines.push({ spans: [{ text: credits[j], tone: "plain" }] })
    }
  }
  var status = provider.status || {}
  if (String(status.state || "ok") !== "ok") {
    lines.push({ spans: [{ text: "  !  " + String(status.message || ""), tone: "critical" }] })
  }
  return lines
}

// Port of core/cmd/quotamon/table.go, matching ConsoleTable.swift.
// Returns [{ spans: [{ text, tone }] }]; a separator line has spans: [].
// tone ∈ "plain" | "dim" | "warning" | "critical"
function consoleLines(snapshot, nowMs) {
  var providers = snapshot && Array.isArray(snapshot.providers) ? snapshot.providers : []
  var lines = []
  for (var i = 0; i < providers.length; i++) {
    if (lines.length > 0) lines.push({ spans: [] })
    lines = lines.concat(consoleProviderLines(providers[i], nowMs))
  }
  return lines
}

// The spans of every line joined, lines joined by a newline.
function consoleText(snapshot, nowMs) {
  return consoleLines(snapshot, nowMs).map(function(line) {
    return line.spans.map(function(span) { return span.text }).join("")
  }).join("\n")
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
      var windowSeverity = severity(percent)
      windows.push({
        id: String(window.id || j),
        label: String(window.label || "Usage"),
        kind: String(window.kind || ""),
        percent: percent,
        percentText: formatPercent(percent),
        resetText: formatCountdown(window.resetsAt, nowMs),
        severity: windowSeverity,
        alarming: windowSeverity === "critical"
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
