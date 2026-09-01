// Package version carries the CalVer release string stamped into the binary
// at build time.
package version

// Value is the release this binary was built from, in CalVer YYYY.M.MICRO
// form. It defaults to "dev" and is overwritten at link time by
// core/Makefile with the contents of the repo-root VERSION file, so a
// `go build ./...` with no ldflags is honestly labelled rather than
// falsely stamped.
var Value = "dev"
