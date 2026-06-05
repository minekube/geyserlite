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
        "-Dio.netty.maxDirectMemory=67108864",
        "-XX:MaxDirectMemorySize=64m",
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

#[cfg(test)]
mod tests {
    use super::default_jvm_args;

    #[test]
    fn default_jvm_args_direct_memory_has_production_safe_floor() {
        const MIN_DIRECT_MEMORY_BYTES: usize = 64 * 1024 * 1024;

        let args = default_jvm_args();
        let netty_max_direct_memory = jvm_arg_value(&args, "-Dio.netty.maxDirectMemory=")
            .expect("missing -Dio.netty.maxDirectMemory");
        let netty_bytes = netty_max_direct_memory
            .parse::<usize>()
            .expect("-Dio.netty.maxDirectMemory must be bytes");
        assert!(
            netty_bytes >= MIN_DIRECT_MEMORY_BYTES,
            "-Dio.netty.maxDirectMemory = {netty_bytes}, want at least {MIN_DIRECT_MEMORY_BYTES}",
        );

        let max_direct_memory_size = jvm_arg_value(&args, "-XX:MaxDirectMemorySize=")
            .expect("missing -XX:MaxDirectMemorySize");
        let max_bytes = parse_memory_size(max_direct_memory_size)
            .expect("-XX:MaxDirectMemorySize must be a valid memory size");
        assert!(
            max_bytes >= MIN_DIRECT_MEMORY_BYTES,
            "-XX:MaxDirectMemorySize = {max_bytes}, want at least {MIN_DIRECT_MEMORY_BYTES}",
        );
    }

    fn jvm_arg_value<'a>(args: &'a [String], prefix: &str) -> Option<&'a str> {
        args.iter().find_map(|arg| arg.strip_prefix(prefix))
    }

    fn parse_memory_size(value: &str) -> Result<usize, std::num::ParseIntError> {
        let (number, multiplier) = match value.as_bytes().last().copied() {
            Some(b'm' | b'M') => (&value[..value.len() - 1], 1024 * 1024),
            Some(b'g' | b'G') => (&value[..value.len() - 1], 1024 * 1024 * 1024),
            _ => (value, 1),
        };
        number.parse::<usize>().map(|n| n * multiplier)
    }
}
