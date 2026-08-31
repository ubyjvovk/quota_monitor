import assert from "node:assert/strict"
import { readFileSync } from "node:fs"

const modelSource = readFileSync(new URL("Model.js", import.meta.url), "utf8")
const Model = new Function(`${modelSource}
return {
  parseSnapshot,
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

// T-0040 names a shared demo fixture that is absent from this checkout. Keep
// these two fixture-derived contract cases explicit and runnable until the PM
// decides whether adding the missing QuotaKit file is in scope.
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
