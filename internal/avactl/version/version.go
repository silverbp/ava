// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

// Package version holds avactl's build-time version information. All
// three vars are injected via -ldflags by `make avactl` (see the
// Makefile) — the defaults below are only what a plain `go build`/`go
// run`, without those flags, produces.
package version

import "fmt"

var (
	// Version is MAJOR.MINOR.PATCH: MAJOR.MINOR comes from the VERSION
	// file at the repo root, and PATCH is the repo's total commit count
	// at build time, so it advances automatically with every commit
	// rather than needing to be hand-maintained.
	Version   = "0.0.0-dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// String renders the one-line form used by `avactl --version`.
func String() string {
	return fmt.Sprintf("%s (%s, built %s)", Version, GitCommit, BuildDate)
}
