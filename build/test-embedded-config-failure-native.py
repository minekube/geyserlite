#!/usr/bin/env python3
"""Exercise libgeyserlite's immediate config-load failure contract."""

from __future__ import annotations

import argparse
import ctypes
import os
import time
from pathlib import Path
from typing import Any


def bind(library: ctypes.CDLL, name: str, args: list[object]) -> Any:
    function = getattr(library, name)
    function.argtypes = args
    function.restype = ctypes.c_int
    return function


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("library", type=Path)
    parser.add_argument("config", type=Path)
    args = parser.parse_args()

    library = ctypes.CDLL(os.fspath(args.library.resolve()))
    void_pointer = ctypes.c_void_p
    create_isolate = bind(
        library,
        "graal_create_isolate",
        [void_pointer, ctypes.POINTER(void_pointer), ctypes.POINTER(void_pointer)],
    )
    tear_down = bind(library, "graal_tear_down_isolate", [void_pointer])
    geyser_init = bind(library, "geyser_init", [void_pointer, ctypes.c_char_p])
    geyser_run = bind(library, "geyser_run", [void_pointer])

    isolate = void_pointer()
    thread = void_pointer()
    rc = create_isolate(None, ctypes.byref(isolate), ctypes.byref(thread))
    if rc != 0:
        raise RuntimeError(f"graal_create_isolate returned {rc}")

    try:
        rc = geyser_init(thread, os.fsencode(args.config.resolve()))
        if rc != 0:
            raise RuntimeError(f"geyser_init returned {rc}")

        started = time.monotonic()
        rc = geyser_run(thread)
        elapsed = time.monotonic() - started
        if rc != -3:
            raise RuntimeError(f"geyser_run returned {rc}, want -3")
        if elapsed >= 1.0:
            raise RuntimeError(f"geyser_run took {elapsed:.3f}s, want <1s")
        print(f"geyser_run rejected invalid config with rc=-3 in {elapsed:.3f}s")
    finally:
        rc = tear_down(thread)
        if rc != 0:
            raise RuntimeError(f"graal_tear_down_isolate returned {rc}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
