//! floodgate — connect to a Gate / Floodgate-aware upstream with
//! shared AES-128 auth.
//!
//! ```sh
//! FLOODGATE_KEY_FILE=/secrets/key.bin cargo run --example floodgate
//! ```

use std::fs;

use geyserlite::{AuthType, Motd, Options, Server};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt::init();

    let key_path = std::env::var("FLOODGATE_KEY_FILE")
        .map_err(|_| anyhow::anyhow!("set FLOODGATE_KEY_FILE to a 16-byte AES key"))?;
    let key = fs::read(&key_path)?;
    if key.len() != 16 {
        anyhow::bail!(
            "Floodgate key must be 16 bytes (AES-128); got {}. The upstream Geyser README's openssl RSA example is wrong.",
            key.len()
        );
    }

    Server::new(Options {
        listen: ":19132".into(),
        upstream: "gate.internal:25567".into(),
        auth_type: AuthType::Floodgate,
        floodgate_key: key,
        motd: Motd {
            line1: "minekube".into(),
            line2: "cross-play".into(),
        },
        ..Default::default()
    })?
    .start()
    .await?;
    Ok(())
}
