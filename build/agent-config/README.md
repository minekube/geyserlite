# build/agent-config/

GraalVM tracing-agent reflection / JNI / serialization metadata, captured
from a real Geyser+Bedrock-client login session. Required for
`native-image` to know which classes Gson, Netty, Floodgate, etc.
reflect on.

## Files

- `reflect-config.json` — every reflective `Class.forName`, `Method.invoke`, etc.
- `jni-config.json` — JNI access patterns
- `proxy-config.json` — dynamic proxy interfaces
- `resource-config.json` — getResource lookups
- `serialization-config.json` — Java serialization classes
- `predefined-classes-config.json` — runtime class definitions

## Why these are committed

GraalVM's static analyzer is blind to reflection. Without these, the
native binary either omits classes that get reflectively loaded (runtime
crash), or includes them but lacks the metadata to instantiate them.

Capturing this metadata requires a **live login session** with a real
Bedrock client — CI can't reproduce that. So we commit it.

## Refreshing

When Geyser changes its reflection surface (rare — usually only major
Bedrock protocol bumps), re-capture:

```sh
# 1. Build the standalone JAR locally
git clone --recurse-submodules https://github.com/GeyserMC/Geyser.git /tmp/Geyser
cd /tmp/Geyser
./gradlew :standalone:shadowJar

# 2. Run with the GraalVM tracing agent attached, MERGING into the existing config
$GRAALVM_HOME/bin/java \
  -agentlib:native-image-agent=config-merge-dir=$GEYSERLITE_REPO/build/agent-config \
  -jar bootstrap/standalone/build/libs/Geyser-Standalone.jar --nogui

# 3. In another terminal: connect from a Bedrock client to the agent run.
#    Walk around, break a block, place a block, open inventory, fly somewhere far.
#    The more variety, the more reflection paths the agent observes.

# 4. Stop the JVM with SIGTERM (Ctrl-C in foreground). The agent flushes new
#    entries on shutdown.

# 5. Review the diff in $GEYSERLITE_REPO/build/agent-config/, commit.
```

## Patching `unsafeAllocated`

Gson uses `Unsafe.allocateInstance` for classes without a no-arg constructor.
The tracing agent records the reflective call but doesn't always tag the type
as `unsafeAllocated`. The Dockerfile patches this automatically by walking
`reflect-config.json` and adding `unsafeAllocated: true` to every non-JDK
entry. See `build/Dockerfile`.

## Geyser annotation metadata

Geyser also generates resource files for annotation-based registries such as
packet translators, block entity translators, collision remappers, and sound
translators. `AnnotationUtils` reads those files and calls `Class.forName` for
every listed class during startup.

The Dockerfile runs `build/augment-annotation-reflect-config.py` after building
the pinned `Geyser-Standalone.jar`. That script merges the generated class lists
from the JAR into `reflect-config.json` for both native-image targets, so normal
upstream Geyser bumps do not require manually editing this captured agent config
just because a translator class was added or renamed.
