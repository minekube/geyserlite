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
