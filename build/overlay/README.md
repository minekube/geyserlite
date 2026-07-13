# build/overlay/

Files we **add** to the upstream Geyser source tree before building. Pure
additions — never overwrite or modify upstream files.

`apply-overlay.sh` does:

```sh
cp -R build/overlay/. build/.work/Geyser/
```

i.e., everything here is layered on top of the cloned Geyser tree at the
relative path it sits at here.

## What's in here

- `geyserlite-native/` — a new Gradle subproject. Depends on Geyser's
  `:standalone` module and exposes `@CEntryPoint`-annotated lifecycle
  methods (`geyser_init`, `geyser_run`, `geyser_shutdown`, etc.).
  GraalVM `native-image --shared` produces `libgeyserlite.so` from this.
- `core/src/main/` — optional Bedrock packet and raw UDP tracing handlers.
- `core/src/test/` — regression tests for overlay behavior and its patches.

Changes to existing upstream files cannot live in the overlay. Stable
intent-based mutations belong in `apply-overlay.sh`; contextual source changes
live in the numbered files documented by [`../patches/README.md`](../patches/README.md).
