# build/ agent notes

- `build/geyser.version` pins the upstream Geyser source ref used by the native
  overlay.
- Normal updates should follow upstream Geyser stable/master through Renovate.
- Preview PR pins are exceptional. Document why the preview is needed, which
  upstream PR or artifact it matches, and how to return to the normal channel.
