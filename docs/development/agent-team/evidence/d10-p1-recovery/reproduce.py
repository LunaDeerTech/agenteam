#!/usr/bin/env python3
"""Replay the frozen D10 P1 pure checks using task-owned caches and a Go overlay."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--go", required=True, help="Path to the Go 1.27.1 binary")
args = parser.parse_args()
evidence = Path(__file__).resolve().parent
root = evidence.parents[4]
inputs = json.loads((evidence / "inputs.json").read_text())
compiled = json.loads((evidence / "compiled-inputs.json").read_text())
expected = {
    name: digest
    for name, digest in inputs["files"].items()
    if not name.startswith("docs/")
}
prefix = "github.com/LunaDeerTech/agenteam/"
for package, files in compiled.items():
    if package.startswith(prefix):
        for name, digest in files.items():
            expected[f"{package[len(prefix):]}/{name}"] = digest
for name, digest in expected.items():
    if hashlib.sha256((root / name).read_bytes()).hexdigest() != digest:
        raise SystemExit(f"Frozen source mismatch: {name}")

owned = Path(tempfile.mkdtemp(prefix="agenteam-d10-p1-replay-"))
for name in ("cache", "modcache", "tmp"):
    (owned / name).mkdir()
env = os.environ.copy()
env.update(
    GOTOOLCHAIN="local",
    GOCACHE=str(owned / "cache"),
    GOMODCACHE=str(owned / "modcache"),
    GOTMPDIR=str(owned / "tmp"),
    GOPROXY="off",
    GOSUMDB="off",
)
go = str(Path(args.go).resolve())
version = subprocess.check_output([go, "version"], cwd=root, env=env, text=True)
if "go1.27.1 " not in version:
    raise SystemExit(f"Wrong toolchain: {version.strip()}")
overlay = owned / "overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(root / "internal/central/skill/recovery_independent_test.go"):
        str(evidence / "probe_test.go.txt")
}}, indent=2) + "\n")
commands = [
    ("dependency", ["mod", "download", "-json", "golang.org/x/text@v0.41.0"]),
    ("unit", ["test", "-count=1", "-json", "./internal/central/skill/..."]),
    ("race", ["test", "-race", "-count=1", "-json", "./internal/central/skill/..."]),
    ("vet", ["vet", "./internal/central/skill/..."]),
    ("probe-race", ["test", f"-overlay={overlay}", "-race", "-count=1", "-json",
                    "-run", "^TestRecovery", "./internal/central/skill"]),
]
records = []
print(f"Task-owned evidence and caches: {owned}", flush=True)
for name, command in commands:
    run_env = env.copy()
    if name == "dependency":
        run_env.update(GOPROXY="https://proxy.golang.org", GOSUMDB="sum.golang.org")
    log = owned / f"{name}.log"
    with log.open("w") as out:
        result = subprocess.run([go, *command], cwd=root, env=run_env,
                                stdout=out, stderr=subprocess.STDOUT)
    records.append({"name": name, "argv": [go, *command], "exit": result.returncode,
                    "log": str(log)})
    (owned / "results.json").write_text(json.dumps(records, indent=2) + "\n")
    print(f"{name}: exit={result.returncode}; {log}", flush=True)
    if result.returncode:
        raise SystemExit(result.returncode)
