# Agent Notes

This repository packages GeyserMC's Bedrock-to-Java translation as native
artifacts and embeddable Go/Rust libraries. Future agents should verify live
upstream state before changing compatibility, release, or docs behavior.

## Live Checks

Do not rely on cached knowledge for Minecraft, Geyser, Gate, Bedrock protocol
support, releases, or CI status. Check live sources when a task involves version
support, previews, releases, or repository metadata.

Useful checks:

```sh
gh repo view minekube/geyserlite --json defaultBranchRef,homepageUrl,url
gh release view --repo minekube/geyserlite --json tagName,publishedAt,url,assets
gh api repos/GeyserMC/Geyser/commits/master --jq '{sha:.sha,date:.commit.committer.date,message:.commit.message}'
VERSION="the-version-you-are-investigating"
gh pr list --repo GeyserMC/Geyser --state open --search "$VERSION OR protocol OR Minecraft" --json number,title,url,isDraft,updatedAt
```

For user-facing support claims, also check:

- Mojang/Minecraft release notes for the Java and Bedrock versions involved.
- GeyserMC supported versions and relevant Geyser pull requests or releases.
- Current Minekube Gate and geyserlite release state if a managed update chain
  is involved.

## Architecture Rules

`geyserlite` is Bedrock ingress before Gate:

```text
Bedrock player
  -> geyserlite
  -> Gate classic
  -> optional vialite
  -> backend server
```

It is responsible for Bedrock-to-Java translation and Floodgate-compatible
identity forwarding. It does not replace ViaVersion/ViaLite backend Java
protocol translation.

When a backend upgrades to a brand-new Java server version before Geyser has
official support, the stable production recommendation is to wait. ViaLite
behind Gate may bridge the Java backend protocol for early adopters if Geyser
can still connect to Gate, but it cannot fix unsupported Bedrock protocols or
Geyser translation gaps.

## Agent Workflow

Use an isolated worktree for feature work. For non-trivial changes, write down
the implementation plan before editing. For bugs or compatibility failures,
debug from evidence: reproduce, inspect logs, identify the failing boundary, and
then change code. Before opening or merging a PR, run fresh verification and get
a code review from a subagent or another reviewer when available.

Relevant workflow skills, when the agent runtime provides them:

- `superpowers:using-git-worktrees`
- `superpowers:systematic-debugging`
- `superpowers:writing-plans`
- `superpowers:verification-before-completion`
- `superpowers:requesting-code-review`

## Update Policy

- `build/geyser.version` pins the upstream Geyser source ref used by the native
  overlay.
- Normal updates should follow upstream Geyser stable/master through Renovate.
- Preview PR pins are exceptional. Document why the preview is needed, which
  upstream PR or artifact it matches, and how to return to the normal channel.
- Releases publish checksummed native artifacts, Go metadata, and Rust crate
  metadata. Gate consumes the release through its managed dependency update
  workflow after release assets exist.
- Keep release-chain changes explicit: GeyserLite release -> Gate managed
  dependency bump -> Gate release -> downstream consumers.

## Development Checks

Start with:

```sh
mise trust
mise install
task setup
```

Common checks:

```sh
task overlay:apply
task test
task lint
```

Use `task build:native` when native-image behavior or release assets are in
scope. It is slower and Docker-backed.

Before merging code changes, verify the affected Go/Rust tests and linting. For
Geyser source bumps or overlay changes, run `task overlay:apply`; for release
automation changes, verify workflow syntax and the release asset ordering.

## Documentation

Keep public operator docs on the Gate website under
`https://gate.minekube.com/geyserlite/`. This repo should keep implementation,
architecture, tuning, and troubleshooting details that are useful to
contributors and embedders.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
