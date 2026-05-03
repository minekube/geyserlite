// New Gradle subproject added to Geyser by build/apply-overlay.sh.
// Depends on Geyser's standalone module and produces libgeyserlite.so via
// GraalVM's native-image --shared.

plugins {
    id("geyser.platform-conventions")
    id("org.graalvm.buildtools.native") version "0.10.3"
}

dependencies {
    api(project(":standalone"))

    // GraalVM SDK provides the @CEntryPoint annotation and the
    // CCharPointer / CTypeConversion C-type helpers GeyserBridge.java
    // imports. The native-image-community:21-ol9 image already has
    // these classes on the BUILD classpath (substratevm bundles them),
    // but javac at *compile time* doesn't see them unless we pull them
    // in via Maven. compileOnly is enough — the runtime JDK provides
    // the same classes; we just need the symbols at compile time.
    //
    // Version 23.1.x is the SDK series matching GraalVM 21 LTS; pin
    // the latest patch in that series.
    compileOnly("org.graalvm.sdk:graal-sdk:23.1.7")
}

graalvmNative {
    binaries {
        named("main") {
            imageName.set("libgeyserlite")
            sharedLibrary.set(true)
            // No mainClass for the shared-lib build — there's no
            // main() to invoke. native-image's analysis root is
            // instead provided by GeyserBridgeFeature (registered via
            // -H:Features below), which calls findClassByName on
            // GeyserBridge during analysis setup; that's what makes
            // its @CEntryPoint methods reachable so geyser_* symbols
            // end up in the .so + libgeyserlite.h.
            mainClass.set("")
            useFatJar.set(true)
            // Notes on what's NOT set here:
            //  --static    incompatible with --shared (set via
            //              sharedLibrary = true above).
            //  --libc=musl static musl libs are built without -fPIC, so
            //              the linker can't fold them into a shared
            //              object. The shared library uses the system
            //              glibc instead — its consumer's libc is
            //              already loaded anyway, so there's no benefit
            //              to bundling our own.
            // -march is architecture-specific; the buildtime arch comes
            // from os.arch at config eval.
            val osArch = System.getProperty("os.arch", "x86_64")
            val march = when (osArch) {
                "amd64", "x86_64" -> "-march=x86-64-v2"
                "aarch64", "arm64" -> "-march=armv8-a"
                else -> "-march=compatibility"
            }
            // Mirror flags.sh's NI_FLAGS_COMMON (the ELF build) — kept
            // here in sync with that file. Anything left out vs the ELF
            // breaks differently: a missing init-at-build-time package
            // surfaces as a runtime ServiceLoader / NoSuchMethod error,
            // a missing IncludeResources entry shows up as Geyser
            // "resource not found" at startup. Both are silent in the
            // build itself — only the actual host load catches them.
            //
            // What's intentionally NOT mirrored from flags.sh:
            //   --static / --libc=musl  incompatible with --shared
            //   GeyserBridgeFeature     shared-lib only (analysis root)
            buildArgs.addAll(
                "-H:ConfigurationFileDirectories=${rootProject.projectDir}/agent-config",
                "-H:Features=com.minekube.geyserlite.bridge.GeyserBridgeFeature",
                """-H:IncludeResources=^(custom-skulls\.yml|permissions\.yml|.+\.json|.+\.properties|.+\.lang|languages/.+|mappings/.+|bedrock/.+|assets/.+|.+\.mcpack)$""",
                "--no-fallback",
                "--enable-url-protocols=https,http",
                "--initialize-at-build-time=" + listOf(
                    "org.apache.logging.log4j",
                    "net.minecrell.terminalconsoleappender",
                    "org.jline",
                    "org.fusesource.jansi",
                    "org.yaml.snakeyaml",
                    "java.awt.Color",
                    "com.sun.jna",
                    "com.minekube.geyserlite.bridge.GeyserBridge",
                ).joinToString(","),
                "--initialize-at-run-time=sun.awt.HeadlessToolkit,sun.awt.SunHints",
                "--strict-image-heap",
                march,
                "-O2",
                "-R:MaxHeapSize=192m",
                "-H:+UnlockExperimentalVMOptions",
                "-H:+RemoveSaturatedTypeFlows",
                "-H:-ReportExceptionStackTraces",
            )
        }
    }
}
