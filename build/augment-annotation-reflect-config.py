#!/usr/bin/env python3
"""Merge Geyser annotation-processor class lists into native-image reflection config."""

from __future__ import annotations

import argparse
import json
import sys
import zipfile
from pathlib import Path


ANNOTATION_RESOURCES = (
    "org.geysermc.geyser.translator.protocol.Translator",
    "org.geysermc.geyser.translator.level.block.entity.BlockEntity",
    "org.geysermc.geyser.translator.collision.CollisionRemapper",
    "org.geysermc.geyser.translator.sound.SoundTranslator",
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--jar", required=True, type=Path)
    parser.add_argument("--config-dir", required=True, type=Path)
    return parser.parse_args()


def read_generated_classes(jar_path: Path) -> set[str]:
    classes: set[str] = set()
    with zipfile.ZipFile(jar_path) as jar:
        names = set(jar.namelist())
        for resource in ANNOTATION_RESOURCES:
            if resource not in names:
                print(f"warning: annotation resource {resource} not found in {jar_path}", file=sys.stderr)
                continue
            with jar.open(resource) as handle:
                for raw_line in handle:
                    line = raw_line.decode("utf-8").strip()
                    if line:
                        classes.add(line)
    return classes


def main() -> int:
    args = parse_args()
    reflect_path = args.config_dir / "reflect-config.json"

    generated_classes = read_generated_classes(args.jar)
    if not generated_classes:
        print("error: no generated annotation classes found", file=sys.stderr)
        return 1

    config = json.loads(reflect_path.read_text())
    by_name = {entry.get("name"): entry for entry in config if "name" in entry}

    added = 0
    updated = 0
    for class_name in sorted(generated_classes):
        entry = by_name.get(class_name)
        if entry is None:
            entry = {
                "name": class_name,
                "allDeclaredConstructors": True,
                "unsafeAllocated": True,
            }
            config.append(entry)
            by_name[class_name] = entry
            added += 1
            continue

        before = dict(entry)
        entry["allDeclaredConstructors"] = True
        entry["unsafeAllocated"] = True
        if entry != before:
            updated += 1

    reflect_path.write_text(json.dumps(config, indent=2) + "\n")
    print(
        "annotation reflect metadata: "
        f"{len(generated_classes)} generated classes, {added} added, {updated} updated"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
