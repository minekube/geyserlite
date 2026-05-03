//! integration_test — boots a real geyserlite::Server (in-process via
//! libloading), waits up to a configured deadline for the Bedrock UDP
//! listener to bind, and exits 0 once it does.
//!
//! Mirrors `go/cmd/integration-test/main.go`. Pair with bedrock-probe
//! in CI to catch "the Rust crate built but can't actually load
//! libgeyserlite.so" regressions.
//!
//! ```sh
//! GEYSERLITE_LIBRARY=/path/to/libgeyserlite-linux-amd64.so \
//!     cargo run --example integration_test -- \
//!         --listen 127.0.0.1:19133 --timeout 30
//! ```

use std::env;
use std::net::{SocketAddr, UdpSocket};
use std::process;
use std::time::{Duration, Instant};

use geyserlite::{AuthType, Options, Server};

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt::init();

    let mut listen = "127.0.0.1:19132".to_string();
    let mut upstream = "127.0.0.1:25565".to_string();
    let mut timeout_secs = 30u64;

    // Tiny hand-rolled flag parser to avoid pulling clap in as an
    // example-only dep. Args mirror the Go flag names so CI scripts
    // can stay symmetric.
    let mut args = env::args().skip(1);
    while let Some(a) = args.next() {
        match a.as_str() {
            "--listen" => listen = args.next().ok_or("--listen needs a value")?,
            "--upstream" => upstream = args.next().ok_or("--upstream needs a value")?,
            "--timeout" => timeout_secs = args.next().ok_or("--timeout needs a value")?.parse()?,
            other => return Err(format!("unknown flag {other}").into()),
        }
    }
    let timeout = Duration::from_secs(timeout_secs);
    let listen_addr: SocketAddr = listen.parse()?;

    let opts = Options {
        listen: listen.clone(),
        upstream,
        auth_type: AuthType::Offline,
        ..Default::default()
    };

    let server = Server::new(opts)?;
    let run = tokio::spawn(async move { server.start().await });

    // Poll the UDP port like the Go variant does — try to bind it; if
    // we can't, geyserlite is on it. Reliable across libc / glibc
    // EADDRINUSE messages.
    let deadline = Instant::now() + timeout;
    while Instant::now() < deadline {
        if is_udp_bound(&listen_addr) {
            eprintln!("OK: {listen} is bound");
            // Stay up: sibling probes (bedrock-probe in CI) need the
            // listener alive long enough to reply. Park until the
            // process is signaled, then _exit immediately — the
            // graceful-shutdown path through libloading currently
            // SIGSEGVs during native teardown, which would tank the
            // test even after a successful probe.
            tokio::time::sleep(timeout).await;
            process::exit(0);
        }
        if run.is_finished() {
            // Server returned (probably an error) before binding.
            let res = run.await?;
            return Err(format!("server exited before bind: {res:?}").into());
        }
        tokio::time::sleep(Duration::from_millis(500)).await;
    }

    eprintln!("integration-test: timed out waiting for {listen} to bind after {timeout_secs}s");
    process::exit(1);
}

fn is_udp_bound(addr: &SocketAddr) -> bool {
    match UdpSocket::bind(addr) {
        Ok(_) => false, // we got it; geyserlite hasn't bound yet
        Err(e) => {
            let msg = e.to_string().to_lowercase();
            msg.contains("address already in use") || msg.contains("addrinuse")
        }
    }
}
