//! fly — run on a Fly.io machine. Bedrock UDP must bind to
//! `fly-global-services` (Fly's UDP NAT target) and not `0.0.0.0`.
//! See `docs/troubleshooting.md`.
//!
//! ```sh
//! FLOODGATE_KEY_BASE64=$(base64 < key.bin) cargo run --example fly
//! ```

use base64::Engine;
use geyserlite::{AuthType, Options, Server};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt::init();

    let key_b64 = std::env::var("FLOODGATE_KEY_BASE64")
        .map_err(|_| anyhow::anyhow!("FLOODGATE_KEY_BASE64 not set — fly secrets set FLOODGATE_KEY_BASE64=$(base64 < key.bin)"))?;
    let key = base64::engine::general_purpose::STANDARD.decode(key_b64.trim())?;

    let listen = format!("{}:19132", geyserlite::fly_global_services());

    Server::new(Options {
        listen,
        upstream: "127.0.0.1:25567".into(), // co-located Gate
        auth_type: AuthType::Floodgate,
        floodgate_key: key,
        ..Default::default()
    })?
    .start()
    .await?;
    Ok(())
}
