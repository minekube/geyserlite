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

The single piece we *can't* express as pure addition is registering the
new subproject in upstream's `settings.gradle.kts`. That lives in
`../patches/0001-register-subproject.patch` as a one-line change.
