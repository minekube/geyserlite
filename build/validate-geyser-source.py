#!/usr/bin/env python3
"""Validate upstream Geyser source assumptions that can break native releases."""

from __future__ import annotations

import re
import sys
from pathlib import Path


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: validate-geyser-source.py <Geyser checkout>", file=sys.stderr)
        return 2

    geyser_dir = Path(sys.argv[1])
    populator = (
        geyser_dir
        / "core/src/main/java/org/geysermc/geyser/registry/populator/BlockRegistryPopulator.java"
    )
    source = populator.read_text()

    entries = re.findall(
        r'ObjectIntPair\.of\("(?P<palette>[^"]+)",\s*'
        r"(?P<codec>Bedrock_v\d+)\.CODEC\.getProtocolVersion\(\)\)",
        source,
    )
    if not entries:
        print(f"no block palette registrations found in {populator}", file=sys.stderr)
        return 1

    seen: dict[str, str] = {}
    failures: list[str] = []
    for palette, codec in entries:
        previous = seen.setdefault(codec, palette)
        if previous != palette:
            failures.append(
                f"{codec} is registered for both block_palette.{previous}.nbt "
                f"and block_palette.{palette}.nbt"
            )

    if failures:
        print("invalid Geyser block palette registrations:", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        return 1

    print(f"validated {len(entries)} Geyser block palette registrations")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
