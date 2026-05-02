//! healthcheck — expose `Server::healthy()` over HTTP for orchestrators.
//! Uses tokio's stdlib-only TCP listener (no axum / hyper / etc.) so
//! the example stays single-file.
//!
//! ```sh
//! cargo run --example healthcheck
//! ```

use std::sync::Arc;

use geyserlite::{AuthType, Options, Server};
use tokio::io::AsyncWriteExt;
use tokio::net::TcpListener;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt::init();

    let server = Arc::new(Server::new(Options {
        upstream: "127.0.0.1:25567".into(),
        auth_type: AuthType::Floodgate,
        floodgate_key: geyserlite::generate_floodgate_key(),
        ..Default::default()
    })?);

    // Run Geyser in the background.
    let s = server.clone();
    let geyser = tokio::spawn(async move { s.start().await });

    // Tiny HTTP server: 200 if Healthy(), 503 otherwise.
    let listener = TcpListener::bind("0.0.0.0:8086").await?;
    tracing::info!("healthcheck listening on :8086");
    let s = server.clone();
    let http = tokio::spawn(async move {
        loop {
            let (mut sock, _) = match listener.accept().await {
                Ok(x) => x,
                Err(_) => continue,
            };
            let healthy = s.healthy();
            let body = if healthy { "ok\n" } else { "not ready\n" };
            let status = if healthy {
                "200 OK"
            } else {
                "503 Service Unavailable"
            };
            let resp = format!(
                "HTTP/1.1 {status}\r\nContent-Length: {}\r\nContent-Type: text/plain\r\n\r\n{body}",
                body.len()
            );
            let _ = sock.write_all(resp.as_bytes()).await;
        }
    });

    tokio::select! {
        res = geyser => { tracing::info!("geyser exited: {res:?}"); }
        _ = tokio::signal::ctrl_c() => {
            tracing::info!("ctrl-c");
            server.stop().await;
        }
    }
    http.abort();
    Ok(())
}
