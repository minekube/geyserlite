// SPDX-License-Identifier: MIT
package geyserlite

import "os"

// FlyGlobalServices returns "fly-global-services" if running on a Fly.io
// machine and "0.0.0.0" otherwise. Fly's UDP edge NATs external traffic to
// this special hostname inside the container, and binding 0.0.0.0 silently
// fails to receive externally-routed UDP — see docs/troubleshooting.md.
//
// The detection is via $FLY_APP_NAME, which Fly always sets in machines.
func FlyGlobalServices() string {
	if os.Getenv("FLY_APP_NAME") != "" {
		return "fly-global-services"
	}
	return "0.0.0.0"
}

// DefaultJVMArgs returns the tuned argument list applied to
// [ModeSubprocess]. Useful for Options.JVMArgs when you want to start
// from defaults and tweak.
//
// -Xmx is intentionally omitted: the geyserlite ELF bakes its own
// `-R:MaxHeapSize` at native-image time (currently 256m, see
// build/flags.sh). Setting -Xmx here would override the build-time
// pin with a runtime value, and a too-tight override silently OOMs
// during Geyser bootstrap. Operators who need a different cap should
// pass their own -Xmx via Options.JVMArgs.
func DefaultJVMArgs() []string {
	return []string{
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
	}
}
