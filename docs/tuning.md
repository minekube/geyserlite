# Tuning

The shipped binary is tuned for a 256 MB Fly.io VM with one Bedrock player
and a co-located Go proxy. If you have different constraints — bigger heap
budget, many concurrent players, latency-critical — the knobs are below.

## Build-time flags

All in [`../build/flags.sh`](../build/flags.sh) with annotations. The big ones:

| Flag | Effect |
|---|---|
| `--strict-image-heap` | Tighter image-heap policy. ~5-10 MB less RSS. |
| `--static --libc=musl` | Single static ELF; no glibc dep. Slightly larger binary, removes runtime deps. |
| `-march=x86-64-v2` | CPU instruction baseline. v3 is faster but excludes some hosts. |
| `-O2` | Optimization level. `-Ob` is faster to build, smaller, but slower at runtime. |
| `-R:MaxHeapSize=64m` | Bake heap size into image. Override at runtime with `-Xmx`. |

PGO (Profile-Guided Optimization) is **not** in the CI build because it
requires capturing a profile from a live load run. Locally:

```sh
# 1. Build instrumented (adds ~3× to binary size, lots of profile counters):
native-image --pgo-instrument [other flags] -jar Geyser-Standalone.jar -o geyser-instr

# 2. Run + connect a real Bedrock client + play 30+ seconds. Disconnect.
./geyser-instr --nogui

# 3. Final build using the captured profile:
native-image --pgo=default.iprof [other flags] -jar Geyser-Standalone.jar -o geyserlite
```

PGO bought ~22 MB idle RSS and ~30 MB binary size in our measurements.

## Runtime flags

Used by the standalone ELF and (via `Options.JVMArgs`) by the Go/Rust
subprocess mode. Embedded mode bakes them in at build time.

```text
-Xmx64m
-XX:MaxHeapFree=4m
-XX:+CollectYoungGenerationSeparately
-XX:ActiveProcessorCount=1
-Dio.netty.maxDirectMemory=16777216
-XX:MaxDirectMemorySize=16m
-Dio.netty.allocator.type=unpooled
-Dio.netty.allocator.numHeapArenas=1
-Dio.netty.allocator.numDirectArenas=1
-Dio.netty.eventLoopThreads=2
-Dio.netty.recycler.maxCapacityPerThread=0
-Dio.netty.leakDetection.level=disabled
-Djava.util.concurrent.ForkJoinPool.common.parallelism=1
-Dlog4j2.disableJmx=true
```

Explanations:

| Flag | Effect |
|---|---|
| `-Xmx64m` | Hard cap on Java heap. Anything above triggers OOM. Default Substrate VM goes much higher. |
| `MaxHeapFree=4m` | How much free heap to keep allocated to the OS. Lower = more `mmap`/`munmap`, less RSS. |
| `CollectYoungGenerationSeparately` | Smaller GC pauses, smaller RSS during full GCs. |
| `ActiveProcessorCount=1` | Tell JVM/Substrate it has 1 CPU regardless of actual count. Reduces auto-sized thread pools. |
| `Dio.netty.maxDirectMemory=16777216` | 16 MB cap on Netty's off-heap buffers. Default is unlimited; uncapped Netty pre-reserves a lot. |
| `Dio.netty.allocator.type=unpooled` | No buffer pool. Slightly higher per-message allocation; way less idle RSS. |
| `Dio.netty.allocator.numHeapArenas=1` | One arena instead of one per thread. RSS savings. |
| `Dio.netty.eventLoopThreads=2` | Two Netty event loops (default = 2× cores). |
| `Dio.netty.recycler.maxCapacityPerThread=0` | Disable per-thread object recycler. Marginal RAM savings. |
| `Dio.netty.leakDetection.level=disabled` | Skip leak-tracking sampling. |
| `ForkJoinPool.common.parallelism=1` | One thread in the common pool. |
| `Dlog4j2.disableJmx=true` | No JMX MBean registration. |

## Different load profiles

| Profile | Adjustments |
|---|---|
| **Many concurrent players (10+)** | Raise `-Xmx96m` or `-Xmx128m`; raise `MaxDirectMemorySize=32m`; raise `eventLoopThreads=4`; consider G1 GC (`--gc=G1` at build time). |
| **Multi-core host** | Raise `ActiveProcessorCount`; raise `eventLoopThreads`; `ForkJoinPool` parallelism. |
| **Latency-critical** | `--gc=G1` at build; raise heap to reduce GC frequency; profile with PGO. |
| **Smallest possible binary** | Add `-Ob` (build mode = smallest); accept slower runtime. |

## Measuring

```sh
# Total RSS / HWM:
cat /proc/$(pgrep geyserlite)/status | grep -E 'VmRSS|VmHWM'

# Live sampling:
while true; do awk '/VmRSS/{print strftime("%H:%M:%S"), $2/1024 "MB"}' /proc/$(pgrep geyserlite)/status; sleep 2; done

# Per-mapping breakdown:
pmap -X $(pgrep geyserlite) | sort -k7 -n -r | head -20
```

Memory budgets we expect to hit:

| Scenario | RSS |
|---|---|
| Idle, just booted | ~80 MB |
| Steady-state, 1 player loaded in | ~95 MB |
| Peak HWM under chunk-load burst | ~120 MB |

The full optimization journey is in [`../ROADMAP.md`](../ROADMAP.md).
