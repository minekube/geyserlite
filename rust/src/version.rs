// SPDX-License-Identifier: MIT

/// The geyserlite release the auto-download path fetches when
/// `Options::version` is empty. CI's release workflow updates this
/// constant in the same commit that publishes the release tag.
pub const DEFAULT_VERSION: &str = "v0.3.13"; // x-release-please-version

/// The URL prefix the auto-download path resolves asset names against.
/// Override via `Options::mirror`.
pub const DEFAULT_DOWNLOAD_BASE: &str = "https://github.com/minekube/geyserlite/releases/download";
