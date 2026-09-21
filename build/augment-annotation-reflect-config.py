#!/usr/bin/env python3
"""Merge generated-class metadata into the native-image reflection config.

Two sources of generated classes have to be registered explicitly, because
native-image cannot discover either of them on its own:

1. Geyser's own annotation-processor class lists (``Translator``,
   ``BlockEntity``, ...). These lists are resources inside the pinned
   ``Geyser-Standalone.jar``; Geyser loads the classes by name at runtime.

2. Configurate's interface configuration implementations
   (``configurate-extra-interface``). Geyser's config is written as
   ``@ConfigSerializable`` *interfaces*; the ``configurate-extra-interface-ap``
   annotation processor generates the ``...Impl`` bodies at compile time and
   writes an ``interface_mappings.properties`` resource that
   ``InterfaceTypeSerializer`` reads at runtime to resolve an interface to its
   implementation with ``Class.forName``.

   In a JVM that lookup always succeeds, so an agent capture of a JVM run
   records the classes that existed *at that time*. Native-image, however, only
   keeps classes that are reachable or registered: a config interface added
   upstream after the last agent capture is absent from the image, and
   ``Class.forName`` then fails with

     SerializationException: Could not find implementation class
     org.geysermc.geyser.configuration.GeyserConfigImpl$SignalingConfigImpl
     for type org.geysermc.geyser.configuration.GeyserConfig$SignalingConfig

   which makes every Geyser config load fail. Deriving the list from the JAR
   (instead of hand-maintaining it) keeps native builds honest across upstream
   config changes: Geyser 2808f7d added the NetherNet signaling config tree and
   broke both the ELF and the shared library this way.
"""

from __future__ import annotations

import argparse
import json
import sys
import zipfile
from pathlib import Path
from typing import Any


ANNOTATION_RESOURCES = (
    "org.geysermc.geyser.translator.protocol.Translator",
    "org.geysermc.geyser.translator.level.block.entity.BlockEntity",
    "org.geysermc.geyser.translator.collision.CollisionRemapper",
    "org.geysermc.geyser.translator.sound.SoundTranslator",
)

# Written by configurate-extra-interface-ap, read by
# org.spongepowered.configurate.interfaces.InterfaceTypeSerializer.
CONFIGURATE_MAPPINGS_RESOURCE = "org/spongepowered/configurate/interfaces/interface_mappings.properties"

# Shapes mirrored from the config classes the tracing agent captured, so a
# derived entry has exactly the same effect as a captured one:
#   - the interface needs its methods invocable (configurate reads the
#     interface's default/abstract methods)
#   - the generated implementation needs its fields (configurate's object
#     mapper reads/writes fields) and its no-arg constructor
INTERFACE_SHAPE = {"queryAllDeclaredMethods": True, "unsafeAllocated": True}
IMPLEMENTATION_SHAPE = {
    "allDeclaredFields": True,
    "queryAllDeclaredMethods": True,
    "unsafeAllocated": True,
    "methods": [{"name": "<init>", "parameterTypes": []}],
}


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


def read_configurate_mappings(jar_path: Path) -> dict[str, str]:
    """Return {config interface -> generated implementation} from the JAR.

    Empty when the pinned Geyser has no interface-based configs (the
    annotation processor did not run), which is a legitimate upstream state.
    """
    mappings: dict[str, str] = {}
    with zipfile.ZipFile(jar_path) as jar:
        try:
            raw = jar.read(CONFIGURATE_MAPPINGS_RESOURCE)
        except KeyError:
            print(
                f"warning: {CONFIGURATE_MAPPINGS_RESOURCE} not in {jar_path}; "
                "no configurate interface implementations to register",
                file=sys.stderr,
            )
            return mappings
        names = set(jar.namelist())

    for raw_line in raw.decode("latin-1").splitlines():
        line = raw_line.strip()
        # Properties files allow a leading comment line (the generator writes a
        # timestamp into one) and blank lines.
        if not line or line.startswith("#"):
            continue
        interface, sep, implementation = line.partition("=")
        if not sep:
            continue
        interface, implementation = interface.strip(), implementation.strip()
        if not interface or not implementation:
            continue

        # Fail closed: a mapping whose implementation is not in the JAR means
        # the image would ship an interface the runtime can never resolve —
        # exactly the failure this script exists to prevent.
        for class_name in (interface, implementation):
            if f"{class_name.replace('.', '/')}.class" not in names:
                print(
                    f"error: configurate mapping lists {class_name}, which is not in {jar_path}",
                    file=sys.stderr,
                )
                raise SystemExit(1)

        mappings[interface] = implementation
    return mappings


def merge_entry(config: list[dict[str, Any]], by_name: dict[str, dict[str, Any]],
                class_name: str, shape: dict[str, Any]) -> bool:
    """Add or strengthen one reflect-config entry. Returns True if it changed."""
    entry: dict[str, Any] | None = by_name.get(class_name)
    if entry is None:
        entry = {"name": class_name}
        config.append(entry)
        by_name[class_name] = entry

    before = json.dumps(entry, sort_keys=True)
    for key, value in shape.items():
        if key == "methods":
            # Never shrink a captured method list: union by (name, parameterTypes)
            # so an entry that already registers more methods keeps them.
            existing = entry.setdefault("methods", [])
            known = {(m.get("name"), tuple(m.get("parameterTypes", []))) for m in existing}
            for method in value:
                key_id = (method.get("name"), tuple(method.get("parameterTypes", [])))
                if key_id not in known:
                    existing.append(method)
                    known.add(key_id)
            continue
        entry[key] = value
    return json.dumps(entry, sort_keys=True) != before


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

    mappings = read_configurate_mappings(args.jar)
    for interface, implementation in sorted(mappings.items()):
        for class_name, shape in (
            (interface, INTERFACE_SHAPE),
            (implementation, IMPLEMENTATION_SHAPE),
        ):
            if merge_entry(config, by_name, class_name, shape):
                updated += 1

    reflect_path.write_text(json.dumps(config, indent=2) + "\n")
    print(
        "annotation reflect metadata: "
        f"{len(generated_classes)} generated classes, {added} added, {updated} updated"
    )
    print(
        "configurate reflect metadata: "
        f"{len(mappings)} interface implementations registered"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
