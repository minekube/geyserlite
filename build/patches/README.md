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
