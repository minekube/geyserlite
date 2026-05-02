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
            buildArgs.addAll(
                "-H:ConfigurationFileDirectories=${rootProject.projectDir}/agent-config",
                "--no-fallback",
                "--enable-url-protocols=https,http",
                "--initialize-at-build-time=org.apache.logging.log4j,java.awt.Color",
                "--initialize-at-run-time=sun.awt.HeadlessToolkit,sun.awt.SunHints",
                "--strict-image-heap",
                "--static", "--libc=musl",
                "-march=x86-64-v2",
                "-O2",
                "-R:MaxHeapSize=64m",
                "-H:+UnlockExperimentalVMOptions",
                "-H:+RemoveSaturatedTypeFlows",
                "-H:-ReportExceptionStackTraces",
            )
        }
    }
}
