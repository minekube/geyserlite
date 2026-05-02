<!-- Short PR title in imperative mood. -->

## What this changes

<!-- One paragraph. -->

## Why

<!-- The motivation; link an issue if there is one. -->

## How to verify

```sh
# Commands a reviewer can paste.
```

## Memory impact

<!-- Required if this touches build/, JVMArgs defaults, or anything that
could shift RSS. Include before/after numbers from `task build:native`
+ `task probe`. Otherwise: "n/a — code-only change". -->

## Roadmap alignment

<!-- Which ROADMAP phase / open question does this address? -->

## Checklist

- [ ] `task lint test` is green locally
- [ ] If `build/**` changed: synthetic probe still gets a pong
- [ ] Updated docs in `docs/` if a public API or behavior changed
- [ ] Conventional commit message (`go:`, `rust:`, `build:`, etc.)
