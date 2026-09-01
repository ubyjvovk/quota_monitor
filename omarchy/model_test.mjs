import assert from "node:assert/strict"
import { readFileSync } from "node:fs"

const modelSource = readFileSync(new URL("Model.js", import.meta.url), "utf8")
const Model = new Function(`${modelSource}
return {
  parseSnapshot,
  parseManifest,
  compareVersions,
  coreVersion,
  pinnedVersion,
  coreUpdateVersion,
  aboutText,
  versionWarning,
  formatCountdown,
  formatAge,
  sortedWindows,
  tightestPercent,
  tightestForRow,
  creditsText,
  providerRows,
  iconBars
}`)()

const now = Date.parse("2026-01-02T00:00:00Z")
const after = milliseconds => new Date(now + milliseconds).toISOString()
const before = milliseconds => new Date(now - milliseconds).toISOString()

const manifestRaw = readFileSync(new URL("manifest.json", import.meta.url), "utf8")
const manifest = Model.parseManifest(manifestRaw)
assert.deepEqual(manifest, { version: "2026.9.1", minQuotamon: "2026.9.1" })
assert.deepEqual(Model.parseManifest("{"), { version: "", minQuotamon: "" })
assert.deepEqual(Model.parseManifest('{"version":"2026.9.1"}'), {
  version: "2026.9.1",
  minQuotamon: ""
})
assert.deepEqual(Model.parseManifest('{"dependencies":{"quotamon":">=2026.9.1"}}'), {
  version: "",
  minQuotamon: ""
})

assert.equal(Model.compareVersions("2026.9.1", "2026.9.2"), -1)
assert.equal(Model.compareVersions("2026.10.1", "2026.9.9"), 1)
assert.equal(Model.compareVersions("2026.9", "2026.9.0"), 0)
assert.equal(Model.compareVersions("dev", "2026.9.1"), null)

assert.equal(Model.coreVersion({ version: " 2026.9.1 " }), "2026.9.1")
assert.equal(Model.coreVersion({ providers: [] }), "")
assert.equal(Model.coreVersion(null), "")

assert.equal(Model.pinnedVersion({ version: "2026.9.2" }), "2026.9.2")
assert.equal(Model.pinnedVersion({}), "")
assert.equal(Model.pinnedVersion({ version: "v2026.9.2" }), "")

assert.equal(Model.coreUpdateVersion({ version: "2026.9.2" }, { version: "2026.9.1" }), "2026.9.2")
assert.equal(Model.coreUpdateVersion({ version: "2026.9.2" }, { version: "2026.9.2" }), "")
assert.equal(Model.coreUpdateVersion({ version: "2026.9.2" }, { version: "2026.9.3" }), "")
assert.equal(Model.coreUpdateVersion({ version: "2026.9.2" }, { providers: [] }), "")
assert.equal(Model.coreUpdateVersion({ version: "latest" }, { version: "2026.9.1" }), "")
assert.equal(Model.coreUpdateVersion({ version: "2026.10.1" }, { version: "2026.9.9" }), "2026.10.1")
assert.equal(Model.coreUpdateVersion({ version: "2026.9.2" }, { version: "dev" }), "")

assert.equal(
  Model.aboutText(manifest, { version: "2026.9.1" }),
  "Quota Monitor 2026.9.1 · quotamon 2026.9.1"
)
assert.equal(
  Model.aboutText(manifest, null),
  "Quota Monitor 2026.9.1 · quotamon version unknown"
)
assert.equal(Model.aboutText({ version: "", minQuotamon: "" }, { version: "2026.9.1" }), "")

assert.equal(Model.versionWarning(manifest, null), "")
assert.equal(
  Model.versionWarning(manifest, { providers: [] }),
  "quotamon is older than this plugin needs (2026.9.1) — use the Update button below"
)
assert.equal(
  Model.versionWarning(manifest, { version: "2026.8.3" }),
  "quotamon 2026.8.3 is older than this plugin needs (2026.9.1) — use the Update button below"
)
assert.equal(Model.versionWarning(manifest, { version: "2026.9.1" }), "")
assert.equal(Model.versionWarning(manifest, { version: "2026.10.0" }), "")
assert.equal(Model.versionWarning(manifest, { version: "dev" }), "")
assert.equal(Model.versionWarning({ version: "2026.9.1", minQuotamon: "" }, {}), "")

const providersOnlyRelease = { version: "2026.9.3", minQuotamon: "2026.9.1" }
const providersOnlyCore = { version: "2026.9.1" }
assert.equal(Model.coreUpdateVersion(providersOnlyRelease, providersOnlyCore), "2026.9.3")
assert.equal(Model.versionWarning(providersOnlyRelease, providersOnlyCore), "")

assert.equal(Model.formatCountdown(after(40 * 60 * 60 * 1000), now), "resets in 1d 16h")
assert.equal(Model.formatCountdown(after(2 * 60 * 60 * 1000 + 7 * 60 * 1000), now), "resets in 2h 7m")
assert.equal(Model.formatCountdown(after(12 * 60 * 1000), now), "resets in 12m")
assert.equal(Model.formatCountdown(after(59 * 1000), now), "resets in <1m")
assert.equal(Model.formatCountdown(before(1), now), "window reset")
assert.equal(Model.formatCountdown(null, now), "")

assert.equal(Model.formatAge(before(3 * 1000), now), "just now")
assert.equal(Model.formatAge(before(45 * 1000), now), "45s ago")
assert.equal(Model.formatAge(before(12 * 60 * 1000), now), "12m ago")
assert.equal(Model.formatAge(before(3 * 60 * 60 * 1000), now), "3h ago")
assert.equal(Model.formatAge(before(2 * 24 * 60 * 60 * 1000), now), "2d ago")

const ordered = Model.sortedWindows({
  windows: [
    { id: "past", kind: "session", usedPercent: 100, resetsAt: before(1000) },
    { id: "weekly-tie", kind: "weekly", usedPercent: 80 },
    { id: "monthly-high", kind: "monthly", usedPercent: 90 },
    { id: "session-tie", kind: "session", usedPercent: 80 }
  ]
}, now)
assert.deepEqual(ordered.map(window => window.id), ["monthly-high", "session-tie", "weekly-tie", "past"])

const tiedRow = {
  windows: [
    null,
    { id: "weekly", kind: "weekly", percent: 95 },
    { id: "session", kind: "session", percent: 95 }
  ]
}
assert.equal(Model.tightestForRow(tiedRow).id, "session")
assert.equal(Model.tightestPercent({ providers: [{ windows: [null] }] }, now), null)
assert.equal(Model.tightestPercent({
  providers: [{ windows: [
    { kind: "weekly", usedPercent: 95 },
    null,
    { kind: "session", usedPercent: 95 }
  ] }]
}, now), 95)

const bars = Model.iconBars({ providers: [
  { id: "first", windows: [{ kind: "session", usedPercent: 10 }] },
  { id: "second", windows: [{ kind: "session", usedPercent: 99 }] }
] }, now)
assert.deepEqual(bars.map(bar => bar.id), ["first", "second"])
assert.deepEqual(bars.map(bar => bar.percent), [10, 99])

// Mirrors `quotamon --demo --json` (fixture `quotamon-demo.json` on master); fix drift by regenerating this inline copy.
const demo = {
  providers: [
    { id: "claude", credits: { balance: "20.00", enabled: false, unlimited: false } },
    { id: "deepinfra", credits: {
      balance: "$10.03",
      spend: "$8.00 this month",
      enabled: true,
      unlimited: false
    } }
  ]
}
const claude = demo.providers.find(provider => provider.id === "claude")
const deepinfra = demo.providers.find(provider => provider.id === "deepinfra")
assert.equal(Model.creditsText(claude.credits), "credits 20.00 (not enabled)")
assert.equal(Model.creditsText(deepinfra.credits), "balance $10.03 remaining · spend $8.00 this month")
assert.equal(Model.creditsText({
  balance: "$7.75 this month",
  spend: "$7.75 this month",
  enabled: true,
  unlimited: true
}), "spend $7.75 this month")
assert.equal(Model.creditsText({ enabled: true, unlimited: true }), "credits unlimited")
assert.equal(Model.creditsText({ balance: "$0.00", enabled: false, unlimited: false }), "")
assert.equal(Model.creditsText({ balance: "€ 0,00", enabled: false, unlimited: false }), "")
assert.equal(Model.creditsText({ balance: "0x0", enabled: false, unlimited: false }), "credits 0x0 (not enabled)")

assert.equal(Model.parseSnapshot("not json"), null)
assert.deepEqual(Model.parseSnapshot("{}").providers, [])

const metadataRows = Model.providerRows({ providers: [
  { id: "cached", origin: "local", observedAt: before(45 * 1000), windows: [] },
  { id: "unknown", origin: "something-new", observedAt: before(3 * 1000), windows: [] }
] }, now)
assert.equal(metadataRows[0].origin, "cached")
assert.equal(metadataRows[0].ageText, "45s ago")
assert.equal(metadataRows[1].origin, "unavailable")
assert.equal(metadataRows[1].ageText, "just now")

console.log("omarchy model tests passed")
