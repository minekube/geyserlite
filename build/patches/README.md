# build/patches/

Numbered `.patch` files applied to the upstream Geyser source tree by
`apply-overlay.sh` after `overlay/` is copied in.

## Conventions

- Files named `NNNN-short-description.patch` — applied in lexicographic order.
- Each patch should be **as small as possible** — touch fewer files, fewer lines.
- Each patch should be **rebase-safe** — `git apply --3way` should succeed even
  if context lines move slightly. Avoid relying on exact line numbers.
- Prefer adding new files via `overlay/` over modifying existing files via patches.
- Patch bytes must survive checkout unchanged. `.gitattributes` disables text
  conversion for this directory; `task test:patches` guards the invariant with
  a Windows-style checkout.

## Current patches

- `0002-bedrock-packet-trace-debug.patch` — installs the optional Bedrock packet
  and raw UDP trace handlers and dumps the packet trace on disconnect.
- `0003-suppress-empty-move-entity-delta.patch` — skips no-op movement packets
  while preserving on-ground state transitions.
- `0004-verified-ingress-subprocess.patch` — starts the authenticated subprocess
  ingress before Geyser accepts Bedrock sessions.
- `0005-awt-free-player-skin-png-decoding.patch` — decodes player skin PNGs with
  pure-Java `pngj` instead of AWT `ImageIO` (AWT cannot load in the static musl
  native image).
- `0006-awt-free-skinprovider-empty-image.patch` — removes the last AWT usage
  from `SkinProvider`'s class initializer (`EMPTY_SKIN_IMAGE`, added upstream
  in Geyser 860fc7102e). A static `BufferedImage` at class-init crashed the
  native image at startup (`Can't load library: awt`), which broke every
  GeyserLite release since v0.5.12. The empty-skin RGBA bytes were already
  generated into `EMPTY_SKIN`, so the extra `BufferedImage` was pure overhead;
  `downloadImage` now returns `null` for disallowed texture domains and
  `requestImage` raises instead of returning the AWT fallback.
- `0007-ground-converted-player-placement.patch` — grounds a converted player's
  placement (initial spawn, server switch, respawn) onto the world's surface
  instead of accepting an airborne one, and stops forwarding a sustained
  suspended-and-motionless Bedrock position. Upstream has no equivalent: a
  Bedrock client with no terrain under it (chunk still loading, or a placement
  carried over a server hand-over) reports a fixed airborne position with no
  vertical motion, which a Java client cannot produce. Paper's
  `ServerGamePacketListenerImpl` latches `clientIsFloating` and disconnects it
  with `multiplayer.disconnect.flying` after 81 ticks (~4.05 s). Exempt:
  vehicle, no-clip, flying, climbing, spectator/creative, water; when no
  surface can be proven below the feet the sample is forwarded unchanged, so a
  false negative cannot freeze a player. Reference implementation and lab A/B
  (Paper 26.3, `allow-flight=false`): kanban `t_1e9c9090`.
- `0009-bedrock-version-refusal-telemetry.patch` — makes a Bedrock version
  refusal countable. Both refusal branches of
  `UpstreamPacketHandler#setCorrectCodec` log one machine-readable line
  (`GEYSERLITE_REFUSAL refusal=… client_protocol=… max_supported_protocol=…
  bedrock_ingress=…`) before the unchanged player-visible disconnect. It adds no
  player identity (the client has not logged in yet) and does not touch the
  supported-version list, so `go/refusal.go` can turn it into one structured
  record plus the `geyserlite_bedrock_version_refused_total` counter.

- `0009-player-thread-termination.patch` — a Bedrock player thread
  (`GeyserServerInitializer`'s `DefaultEventLoopGroup`) that dies from a fatal error
  (e.g. `OutOfMemoryError` while zlib-compressing a downstream burst) is terminated for
  good by Netty: `Unexpected exception from an event executor` is logged once and every
  later `schedule`/`execute` on it is rejected. Before this patch the group was created
  once, so every session after the death was bound to a dead executor,
  `GeyserSession.connectDownstream` threw `RejectedExecutionException` when scheduling the
  tick, `authenticate()` swallowed it, and the client waited on "generating world" until
  its own timeout while its XUID stayed reserved — a total, silent NetherNet outage for
  the life of the process. The patch (a) replaces a player thread group whose threads have
  terminated instead of handing it to new sessions, logging and counting the loss, and
  (b) disconnects a session with an explicit reason when its tick still cannot be
  scheduled, instead of leaving it hanging. Reference: kanban `t_5cc9131c` /
  `t_ec15de25`, 2026-11-01 NetherNet outage.

## Generating new patches

When you find you need a real source modification:

```sh
./build/apply-overlay.sh                    # set up the work tree
cd build/.work/Geyser
# ...edit Geyser source files...
git diff > ../../patches/NNNN-description.patch
```

Then re-run `apply-overlay.sh` from a clean state to verify the patch applies.

## When upstream conflicts

CI runs `apply-overlay.sh` on every Renovate-bumped Geyser SHA. If a patch
fails to apply with `--3way`, the PR fails with which patch + which file
conflicted. Resolution:

```sh
echo <new-sha> > build/geyser.version
./build/apply-overlay.sh                   # see the conflict locally
cd build/.work/Geyser
git status                                  # rejected hunks shown as .rej
# fix manually, then regenerate:
git diff > ../../patches/NNNN-description.patch
```
