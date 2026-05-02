//! basic — minimum viable geyserlite. Subprocess mode against an
//! offline-mode Java backend.
//!
//! ```sh
//! cargo run --example basic
//! ```

use geyserlite::{AuthType, Options, Server};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt::init();

    Server::new(Options {
        listen: ":19132".into(),
        upstream: "127.0.0.1:25565".into(),
        auth_type: AuthType::Offline,
        ..Default::default()
    })?
    .start()
    .await?;
    Ok(())
}
