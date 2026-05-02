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

// DefaultJVMArgs returns the tuned argument list used by libgeyserlite.so
// at build time (and applied to ModeSubprocess). Useful for Options.JVMArgs
// when you want to start from defaults and tweak.
func DefaultJVMArgs() []string {
	return []string{
		"-Xmx64m",
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
	}
}
