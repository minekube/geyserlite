//! custom_config — override the 256-MB-tuned defaults for a beefier
//! host: bigger heap, more Netty workers, tighter restart backoff.
//! Uses [`Mode::Subprocess`] for crash isolation under heavy load.
//!
//! ```sh
//! cargo run --example custom_config
//! ```

use std::time::Duration;

use geyserlite::{AuthType, Mode, Options, RestartPolicy, Server};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt::init();

    let jvm_args = vec![
        "-Xmx256m".into(),
        "-XX:MaxHeapFree=16m".into(),
        "-XX:+CollectYoungGenerationSeparately".into(),
        "-Dio.netty.maxDirectMemory=64m".into(),
        "-XX:MaxDirectMemorySize=64m".into(),
        "-Dio.netty.allocator.type=pooled".into(),
        "-Dio.netty.allocator.numHeapArenas=4".into(),
        "-Dio.netty.allocator.numDirectArenas=4".into(),
        "-Dio.netty.eventLoopThreads=8".into(),
        "-Dlog4j2.disableJmx=true".into(),
    ];

    Server::new(Options {
        upstream: "127.0.0.1:25567".into(),
        auth_type: AuthType::Floodgate,
        floodgate_key: geyserlite::generate_floodgate_key(),
        jvm_args: Some(jvm_args),
        restart_policy: Some(RestartPolicy {
            min_backoff: Duration::from_millis(500),
            max_backoff: Duration::from_secs(30),
            max_retries: 0,
        }),
        shutdown_timeout: Some(Duration::from_secs(60)),
        mode: Mode::Subprocess,
        ..Default::default()
    })?
    .start()
    .await?;
    Ok(())
}
