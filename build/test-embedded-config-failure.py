#!/usr/bin/env python3
"""Regression checks for the embedded config-load failure overlay.

These tests use a minimal upstream bootstrap fixture so they stay fast and prove
both sides of the contract without cloning or compiling Geyser:

* embedded config failure reports the config path, preserves the ordinary
  standalone System.exit path, and exposes a bridge-visible failure accessor;
* GeyserBridge checks that accessor immediately after initialization and returns
  the dedicated error code before entering the shutdown wait.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
import tempfile
from pathlib import Path

BOOTSTRAP_PATH = Path(
    "bootstrap/standalone/src/main/java/org/geysermc/geyser/platform/standalone/"
    "GeyserStandaloneBootstrap.java"
)
BRIDGE_PATH = Path(
    "geyserlite-native/src/main/java/com/minekube/geyserlite/bridge/GeyserBridge.java"
)
CONFIG_LOADER_PATH = Path(
    "core/src/main/java/org/geysermc/geyser/configuration/ConfigLoader.java"
)

BOOTSTRAP_FIXTURE = """package org.geysermc.geyser.platform.standalone;

public class GeyserStandaloneBootstrap {
    private Object geyserConfig;
    private Object gui;
    private Object geyserLogger;
    private boolean useGui = false;
    private String configFilename = "config.yml";

    public static void main(String[] args) {
        System.setProperty("java.util.logging.manager", "org.apache.logging.log4j.jul.LogManager");
        GeyserStandaloneLogger.setupStreams();
    }

    public void onGeyserEnable() {
        this.geyserConfig = loadConfig(Object.class);
        if (this.geyserConfig == null) {
            if (gui == null) {
                System.exit(1);
            } else {
                return;
            }
        }
        geyserLogger.start();
    }

    @Override
    public <T extends GeyserConfig> T loadConfig(Class<T> configClass) {
        return null;
    }

    public void onGeyserShutdown() {
        geyser.shutdown();
        System.exit(0);
    }
}
"""

CONFIG_LOADER_FIXTURE = """package org.geysermc.geyser.configuration;

public final class ConfigLoader {
    private Object bootstrap;
    private java.io.File configFile;

    public Object load() {
        try {
            return load0();
        } catch (java.io.IOException ex) {
            bootstrap.getGeyserLogger().error(GeyserLocale.getLocaleStringLog("geyser.config.failed"), ex);
            return null;
        }
    }
}
"""


def fail(message: str) -> None:
    print(f"embedded-config-failure test: {message}", file=sys.stderr)
    raise SystemExit(1)


def run_overlay(apply_overlay: Path, bridge: Path) -> tuple[str, str, str]:
    with tempfile.TemporaryDirectory() as temp:
        root = Path(temp)
        bootstrap = root / BOOTSTRAP_PATH
        staged_bridge = root / BRIDGE_PATH
        config_loader = root / CONFIG_LOADER_PATH
        bootstrap.parent.mkdir(parents=True)
        staged_bridge.parent.mkdir(parents=True)
        config_loader.parent.mkdir(parents=True)
        bootstrap.write_text(BOOTSTRAP_FIXTURE)
        staged_bridge.write_text(bridge.read_text())
        config_loader.write_text(CONFIG_LOADER_FIXTURE)

        result = subprocess.run(
            [
                "bash",
                str(apply_overlay),
                "--patch-bootstrap",
                str(bootstrap),
                str(staged_bridge),
                str(config_loader),
            ],
            text=True,
            capture_output=True,
            check=False,
        )
        if result.returncode:
            fail(
                f"fixture overlay failed with rc={result.returncode}\n"
                f"stdout:\n{result.stdout}\nstderr:\n{result.stderr}"
            )
        return bootstrap.read_text(), staged_bridge.read_text(), config_loader.read_text()


def assert_bootstrap_contract(source: str) -> None:
    if source.count('Boolean.getBoolean("geyserlite.embedded")') != 3:
        fail("expected exactly three embedded property gates in patched bootstrap")
    if not re.search(r"else \{\s*System\.exit\(1\);\s*\}", source):
        fail("standalone config-load failure no longer preserves System.exit(1)")
    if "System.err.println" not in source or "configFilename" not in source:
        fail("embedded config-load failure does not report its config path to stderr")
    if "public boolean geyserLiteConfigLoadFailed()" not in source:
        fail("patched bootstrap does not expose config-load failure state")
    if "return this.geyserConfig == null;" not in source:
        fail("config-load failure accessor is not derived from the loaded config")


def assert_config_loader_contract(source: str) -> None:
    embedded = source.index('Boolean.getBoolean("geyserlite.embedded")')
    stderr = source.index("System.err.println", embedded)
    config_path = source.index("configFile", stderr)
    cause = source.index(' + ": " + ex', config_path)
    stack = source.index("ex.printStackTrace(System.err);", cause)
    ordinary_logger = source.index("bootstrap.getGeyserLogger().error", stack)
    if not embedded < stderr < config_path < cause < stack < ordinary_logger:
        fail("embedded diagnostics must print path and cause before ordinary logging")


def assert_bridge_contract(source: str) -> None:
    init = source.index("bootstrap.onGeyserInitialize();")
    failure_check = source.index("bootstrap.geyserLiteConfigLoadFailed()", init)
    running = source.index("running.set(true);", init)
    waiting = source.index("shutdownLatch.await();", init)
    if not init < failure_check < running < waiting:
        fail("bridge failure check must happen before running=true and latch wait")
    match = re.search(
        r"if \(bootstrap\.geyserLiteConfigLoadFailed\(\)\) \{\s*return (?P<code>-\d+);\s*\}",
        source,
    )
    if match is None or match.group("code") != "-3":
        fail("bridge must return dedicated config-load failure code -3")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--apply-overlay", type=Path, required=True)
    parser.add_argument("--bridge", type=Path, required=True)
    args = parser.parse_args()

    bootstrap, bridge, config_loader = run_overlay(
        args.apply_overlay.resolve(), args.bridge.resolve()
    )
    assert_bootstrap_contract(bootstrap)
    assert_config_loader_contract(config_loader)
    assert_bridge_contract(bridge)
    print("embedded config-load failure overlay contract OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
