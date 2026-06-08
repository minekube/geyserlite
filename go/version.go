// SPDX-License-Identifier: MIT
package geyserlite

// DefaultVersion is the geyserlite release the auto-download path fetches
// when [Options.Version] is empty. release-please bumps this in the
// same PR that bumps Cargo.toml + tags the new version, so a given
// pinned go.mod always corresponds to a deterministic native asset.
//
// Override via [Options.Version] for explicit pinning.
const DefaultVersion = "v0.2.7" // x-release-please-version

// DefaultDownloadBase is the URL prefix the auto-download path resolves
// asset names against. Overrideable via [Options.Mirror].
const DefaultDownloadBase = "https://github.com/minekube/geyserlite/releases/download"
