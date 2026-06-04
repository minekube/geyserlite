# Changelog

## [0.2.4](https://github.com/minekube/geyserlite/compare/v0.2.3...v0.2.4) (2026-06-04)


### Bug Fixes

* **build:** suppress native Log4j status warning ([77d8d25](https://github.com/minekube/geyserlite/commit/77d8d25c75e47b89e6d1e3af3cc3a3febdb562bd))
* **build:** suppress native Log4j status warning ([b2d1e09](https://github.com/minekube/geyserlite/commit/b2d1e090dba5dd6a4ce60f149891937fc6b1ecd3))

## [0.2.3](https://github.com/minekube/geyserlite/compare/v0.2.2...v0.2.3) (2026-06-03)


### Bug Fixes

* **ci:** complete release PR automation loop ([2133002](https://github.com/minekube/geyserlite/commit/21330020a0088b04dbf34673b15d53b71227a4aa))
* **ci:** complete release PR automation loop ([33561b4](https://github.com/minekube/geyserlite/commit/33561b40e7324d5fba957a2a41ae1fec47984f40))

## [0.2.2](https://github.com/minekube/geyserlite/compare/v0.2.1...v0.2.2) (2026-06-03)


### Bug Fixes

* **build:** keep Geyser annotation classes reachable ([802b632](https://github.com/minekube/geyserlite/commit/802b6325299f8d9278de47ff233ad387570669e6))
* **build:** keep Geyser annotation classes reachable ([10a4633](https://github.com/minekube/geyserlite/commit/10a463374439dbc8c73b8dd2f05685a325421a55))
* **ci:** give shared native-image build enough heap ([1bd932e](https://github.com/minekube/geyserlite/commit/1bd932eece17fc098b6313e0e316eeda21eb7cec))
* **ci:** open Geyser Renovate PRs immediately ([a95f7c8](https://github.com/minekube/geyserlite/commit/a95f7c8a40af95724991aa1ec2bd06c0fda6294c))
* **ci:** run required lint check for build updates ([ae0d5aa](https://github.com/minekube/geyserlite/commit/ae0d5aa8db18b93426031a0cd41b8199679bab46))
* **deps:** bump Geyser to 53b5842 ([a45ad57](https://github.com/minekube/geyserlite/commit/a45ad57558995d0ba7d5ec3b70d5f8f7104ea33a))
* **deps:** bump Geyser to 53b5842 ([2d8426b](https://github.com/minekube/geyserlite/commit/2d8426b4eb12b812f111235e4549ba7d82db19ee))

## [0.2.1](https://github.com/minekube/geyserlite/compare/v0.2.0...v0.2.1) (2026-06-02)


### Bug Fixes

* **ci:** use one Renovate config ([611d12c](https://github.com/minekube/geyserlite/commit/611d12c81dbeabde6e9bdbda985adc45a16ee185))
* **go:** compile embedded loader on windows ([23925a0](https://github.com/minekube/geyserlite/commit/23925a017e9c80fccd8de89d12c398d31ebee5e0))
* **go:** compile embedded loader on windows ([af0a439](https://github.com/minekube/geyserlite/commit/af0a439c4f184b49473ad7fde674b74ac169d1b5))

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
