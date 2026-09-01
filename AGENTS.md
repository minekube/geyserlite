# Agent Notes

## Live Checks

Do not rely on cached knowledge for Minecraft, Geyser, Gate, Bedrock protocol
support, releases, or CI status. Check live sources when a task involves version
support, previews, releases, or repository metadata. The `gh` recipes and the
support-claim checklist are in `.claude/skills/upstream-live-check/SKILL.md`.

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

Verified Bedrock ingress is verifier-origin data. The Go surfaces in
`go/ingress.go` accept it only through the frozen embedded callback or
authenticated subprocess framing from `go.minekube.com/connect/geyserliteabi`;
never add a caller-controlled verified flag or an alternate construction path.
Handoff mismatches are fail-closed.

Production guidance for a backend that outruns official Geyser support is in
`.claude/skills/upstream-live-check/SKILL.md`.

## Agent Workflow

Relevant workflow skills, when the agent runtime provides them:

- `superpowers:using-git-worktrees`
- `superpowers:systematic-debugging`
- `superpowers:writing-plans`
- `superpowers:verification-before-completion`
- `superpowers:requesting-code-review`

## Update Policy

- The `build/geyser.version` pin, the Renovate update channel, and the
  preview-pin documentation rule are in `build/CLAUDE.md`.
- The release chain (GeyserLite release -> Gate managed dependency bump -> Gate
  release -> downstream consumers) is in `.claude/skills/release/SKILL.md`.

## Development Checks

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
