# Changelog

## [0.2.0](https://github.com/minekube/geyserlite/compare/v0.1.3...v0.2.0) (2026-05-04)


### Features

* **libs:** ConfigOverrides — deep-merge any Geyser config knob (Go + Rust) ([09f8920](https://github.com/minekube/geyserlite/commit/09f8920b99e1ca9655b62ff51f9e2db9186b155d))


### Bug Fixes

* **ci:** cargo fmt + skip CHANGELOG.md in markdownlint ([8a04f3f](https://github.com/minekube/geyserlite/commit/8a04f3f176f30370ac118f9bf454781649b5abb8))
* **ci:** each integration job needs actions/checkout before the composite ([44de3e3](https://github.com/minekube/geyserlite/commit/44de3e3c5b2f3fe3f5441f3b5fe857d511f5c523))
* **release:** cargo publish --allow-dirty for the lockfile drift case ([6380360](https://github.com/minekube/geyserlite/commit/63803602fc27f298141d426d54dd1d14a1573d7d))

## [0.1.3](https://github.com/minekube/geyserlite/compare/v0.1.2...v0.1.3) (2026-05-04)


### Bug Fixes

* **build:** bump baked heap 192m → 256m ([eb2f654](https://github.com/minekube/geyserlite/commit/eb2f654ba5e2beed5dd0f213ebbbe91f1ddf250a))
* **libs:** drop -Xmx from DefaultJVMArgs (was forcing 64m onto a 256m image) ([29adea7](https://github.com/minekube/geyserlite/commit/29adea7546b95f269453389c1de5edeafe08206f))
