// SPDX-License-Identifier: MIT

/// Returns `"fly-global-services"` on Fly.io, `"0.0.0.0"` elsewhere.
///
/// Fly's UDP edge NATs external traffic to this internal hostname inside the
/// container, so binding `0.0.0.0` silently fails to receive externally
/// routed UDP. See `docs/troubleshooting.md`.
pub fn fly_global_services() -> &'static str {
    if std::env::var("FLY_APP_NAME").is_ok() {
        "fly-global-services"
    } else {
        "0.0.0.0"
    }
}

/// The tuned runtime args applied to subprocess mode. Useful for
/// `Options.jvm_args` when you want to start from defaults and tweak.
///
/// `-Xmx` is intentionally omitted: the geyserlite ELF bakes its own
/// `-R:MaxHeapSize` at native-image time (currently 256m, see
/// `build/flags.sh`). Setting `-Xmx` here would override the build-time
/// pin with a runtime value, and a too-tight override silently OOMs
/// during Geyser bootstrap. Operators who need a different cap should
/// pass their own `-Xmx` via `Options.jvm_args`.
pub fn default_jvm_args() -> Vec<String> {
    [
        "-XX:MaxHeapFree=4m",
        "-XX:+CollectYoungGenerationSeparately",
        "-XX:ActiveProcessorCount=1",
        "-Dio.netty.maxDirectMemory=16777216",
        "-XX:MaxDirectMemorySize=16m",
        "-Dio.netty.allocator.type=unpooled",
        "-Dio.netty.allocator.numHeapArenas=1",
        "-Dio.netty.allocator.numDirectArenas=1",
        "-Dio.netty.eventLoopThreads=2",
        "-Dio.netty.recycler.maxCapacityPerThread=0",
        "-Dio.netty.leakDetection.level=disabled",
        "-Djava.util.concurrent.ForkJoinPool.common.parallelism=1",
        "-Dlog4j2.disableJmx=true",
    ]
    .iter()
    .map(|s| (*s).into())
    .collect()
}
