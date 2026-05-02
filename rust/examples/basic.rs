//! Minimal geyserlite usage.
//!
//! ```sh
//! cargo run --example basic
//! ```

use geyserlite::{AuthType, Options, Server};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt::init();

    let key = geyserlite::generate_floodgate_key();
    let server = Server::new(Options {
        listen: ":19132".into(),
        upstream: "127.0.0.1:25567".into(),
        auth_type: AuthType::Floodgate,
        floodgate_key: key,
        ..Default::default()
    })?;

    tracing::info!("starting geyserlite (in-process mode)");
    server.start().await?;
    Ok(())
}
