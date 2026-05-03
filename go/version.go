// SPDX-License-Identifier: MIT
package geyserlite

// DefaultVersion is the geyserlite release the auto-download path fetches
// when [Options.Version] is empty. CI's release workflow updates this
// constant in the same commit that publishes the release tag, so a given
// pinned `go.mod` always corresponds to a deterministic native asset.
//
// Override via [Options.Version] for explicit pinning.
const DefaultVersion = "v0.1.2"

// DefaultDownloadBase is the URL prefix the auto-download path resolves
// asset names against. Overrideable via [Options.Mirror].
const DefaultDownloadBase = "https://github.com/minekube/geyserlite/releases/download"
