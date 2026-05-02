// SPDX-License-Identifier: MIT
#![doc = include_str!("../README.md")]

//! See the [project ROADMAP](https://github.com/minekube/geyserlite/blob/main/ROADMAP.md)
//! for milestones.

mod backoff;
mod config;
#[cfg(feature = "download")]
mod download;
#[cfg(feature = "embed")]
mod embed;
mod embedded;
mod error;
mod fly;
mod floodgate;
mod locate;
mod options;
#[cfg(any(feature = "embed", feature = "download"))]
mod paths;
mod server;
mod subprocess;
mod version;

pub use error::{Error, Result};
pub use floodgate::generate_floodgate_key;
pub use fly::{default_jvm_args, fly_global_services};
pub use options::{AuthType, Mode, Motd, Options, RestartPolicy};
pub use server::Server;
pub use version::{DEFAULT_DOWNLOAD_BASE, DEFAULT_VERSION};
