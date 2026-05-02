// SPDX-License-Identifier: MIT
#![doc = include_str!("../README.md")]

//! See the [project ROADMAP](https://github.com/minekube/geyserlite/blob/main/ROADMAP.md)
//! for milestones.

mod backoff;
mod config;
mod embedded;
mod error;
mod fly;
mod floodgate;
mod locate;
mod options;
mod server;
mod subprocess;

pub use error::{Error, Result};
pub use floodgate::generate_floodgate_key;
pub use fly::{default_jvm_args, fly_global_services};
pub use options::{AuthType, Mode, Motd, Options, RestartPolicy};
pub use server::Server;
