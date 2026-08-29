# QuotaMon core

QuotaMon is the portable Go core for QuotaKit. It normalises provider usage
into one snapshot contract that command-line and graphical frontends can
decode without knowing provider-specific response formats. The module uses the
Go standard library only and does not require cgo.

## Layout

- `cmd/quotamon/` contains the command-line entry point.
- `internal/snapshot/` defines the normalised model and JSON contract.
- `internal/source/` defines provider sources and prioritised errors.
- `internal/jsonx/` contains explicit-path helpers for loose provider JSON.
- `internal/fixtures/` loads the conformance fixtures shared with QuotaKit.

## Build and test

Run `make build` to create the host binary at `bin/quotamon`. Run `make test`
to execute `go vet ./...` followed by `go test ./...`. Run `make matrix` to
cross-compile Darwin arm64, Linux amd64, and Linux arm64 binaries with cgo
disabled.

Provider status uses the v2 object shape: `{"state":"ok"}` or a
`needsSetup`/`failed` state with a `message`. QuotaKit emits this form and still
accepts its legacy synthesized enum representation so persisted snapshots
remain readable.

All timestamps emitted in snapshot JSON are RFC 3339 UTC with no fractional
seconds. Decoding remains permissive and accepts RFC 3339 fractional seconds
and numeric offsets, but the no-fraction output rule is required because
Swift's ISO-8601 snapshot decoder rejects fractional seconds.
