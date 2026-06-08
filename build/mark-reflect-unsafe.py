#!/usr/bin/env python3
"""Mark Geyser reflection config entries unsafe-allocated for native-image."""

from __future__ import annotations

import argparse
import json
from pathlib import Path


SKIP_PREFIXES = (
    "java.",
    "javax.",
    "sun.",
    "jdk.",
    "com.sun.",
    "com.oracle.",
    "org.graalvm.",
)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--config-dir",
        required=True,
        type=Path,
        help="Directory containing reflect-config.json",
    )
    args = parser.parse_args()

    path = args.config_dir / "reflect-config.json"
    with path.open() as f:
        config = json.load(f)

    patched = 0
    for entry in config:
        name = entry.get("name", "")
        if name.startswith(SKIP_PREFIXES) or entry.get("unsafeAllocated"):
            continue
        entry["unsafeAllocated"] = True
        patched += 1

    with path.open("w") as f:
        json.dump(config, f, indent=2)

    print(f"patched {patched} entries")


if __name__ == "__main__":
    main()
