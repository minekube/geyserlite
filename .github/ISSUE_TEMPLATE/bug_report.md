---
name: Bug report
about: Something isn't working
title: ''
labels: bug
---

## What happened

<!-- One-line summary. -->

## What you expected

<!-- One line. -->

## Reproduction

```sh
# Steps a maintainer can paste verbatim. Smaller is better.
```

## Environment

- geyserlite version (`go.minekube.com/geyserlite` or `geyserlite` crate):
- mode: `Embedded` / `Subprocess` / running the ELF directly
- OS / arch: e.g. `linux/amd64`, kernel `6.1`
- Where it's running: bare metal / Fly / Docker / Kubernetes / other
- Upstream Java side: Gate vX.Y / Paper / Velocity / etc.

## Logs

<details>
<summary>geyserlite logs</summary>

```
paste here — redact Floodgate keys, IPs, player names if sensitive
```

</details>

<details>
<summary>upstream / host logs</summary>

```

```

</details>

## Synthetic probe output

```sh
go run go.minekube.com/geyserlite/cmd/bedrock-probe -json <addr>
```

```
paste output (or "times out", "no UDP socket", etc.)
```
