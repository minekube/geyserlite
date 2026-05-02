// New Gradle subproject added to Geyser by build/apply-overlay.sh.
// Depends on Geyser's standalone module and produces libgeyserlite.so via
// GraalVM's native-image --shared.

plugins {
    id("geyser.platform-conventions")
    id("org.graalvm.buildtools.native") version "0.10.3"
}

dependencies {
    api(project(":standalone"))
}

graalvmNative {
    binaries {
        named("main") {
            imageName.set("libgeyserlite")
            sharedLibrary.set(true)
            mainClass.set("") // unused for shared mode
            useFatJar.set(true)
            // Note: --static is intentionally absent — it's mutually
            // exclusive with --shared (which the plugin sets via
            // sharedLibrary = true above).
            // -march is architecture-specific; the buildtime arch is
            // resolved from System.getProperty("os.arch") at config eval.
            val osArch = System.getProperty("os.arch", "x86_64")
            val march = when (osArch) {
                "amd64", "x86_64" -> "-march=x86-64-v2"
                "aarch64", "arm64" -> "-march=armv8-a"
                else -> "-march=compatibility"
            }
            buildArgs.addAll(
                "-H:ConfigurationFileDirectories=${rootProject.projectDir}/agent-config",
                "--no-fallback",
                "--enable-url-protocols=https,http",
                "--initialize-at-build-time=org.apache.logging.log4j,java.awt.Color",
                "--initialize-at-run-time=sun.awt.HeadlessToolkit,sun.awt.SunHints",
                "--strict-image-heap",
                "--libc=musl",
                march,
                "-O2",
                "-R:MaxHeapSize=64m",
                "-H:+UnlockExperimentalVMOptions",
                "-H:+RemoveSaturatedTypeFlows",
                "-H:-ReportExceptionStackTraces",
            )
        }
    }
}
