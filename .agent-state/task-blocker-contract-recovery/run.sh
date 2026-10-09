#!/bin/sh
set -eu
probe_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo=$(CDPATH= cd -- "$probe_dir/../.." && pwd)
exec python3 - "$repo" "$probe_dir" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

repo, probe = map(Path, sys.argv[1:])
output = repo / "output/ai/task-blocker-contract-recovery"
output.mkdir(parents=True, exist_ok=True)
run = Path(tempfile.mkdtemp(prefix="run-", dir=output))
for path in (run / "tmp", run / "config", run / "cache"):
    path.mkdir()
# Explicitly transferred by root for this offline verification window.
go_cache = repo / "output/ai/task-transition-core-recovery/go-cache"
if not go_cache.is_dir():
    raise SystemExit("Transferred warm cache is unavailable")
env = os.environ.copy()
env.update(GOTOOLCHAIN="local", GOENV="off", GOPROXY="off", GOSUMDB="off", GOWORK="off",
           GOFLAGS="", GOMAXPROCS="2", CGO_ENABLED="1", GOCACHE=str(go_cache),
           GOMODCACHE="/home/agent/go/pkg/mod", TMPDIR=str(run / "tmp"), GOTMPDIR=str(run / "tmp"),
           XDG_CONFIG_HOME=str(run / "config"), XDG_CACHE_HOME=str(run / "cache"), GOTELEMETRY="off")
go = os.environ.get("AGENTEAM_GO", "/workspace/toolchains/go1.27.1/bin/go")
version = subprocess.run([go, "version"], env=env, cwd=repo, capture_output=True, text=True, timeout=5)
if version.returncode or not version.stdout.startswith("go version go1.27.1 "):
    raise SystemExit("Go 1.27.1 required")
inputs = sorted((repo / "internal/central/work/contract").glob("*.go"))
inputs += [repo / "go.mod", repo / "go.sum", probe / "probe_test.go", probe / "run.sh",
           repo / "docs/development/work-items/d11-task-blocker-contracts.md"]
def hashes():
    return {str(p.relative_to(repo)): hashlib.sha256(p.read_bytes()).hexdigest() for p in inputs}
before = hashes()
(run / "inputs.json").write_text(json.dumps(before, indent=2) + "\n")
overlay = run / "overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(repo / "internal/central/work/contract/zz_independent_task_blocker_test.go"):
    str(probe / "probe_test.go")
}}) + "\n")
top = {"TestIndependentBlocker" + n for n in (
    "WireAndPrecedence", "LexicalLimits", "TextAndEncoding", "ValueSemantics", "LegacyAndLog")}
children = {f"TestIndependentBlockerValueSemantics/shared-{n}" for n in range(4)}
print(version.stdout.strip(), flush=True)
print("evidence:", run, flush=True)
for mode in ("pure", "race"):
    cmd = [go, "test"] + (["-race"] if mode == "race" else [])
    cmd += ["-count=1", "-p=2", "-timeout=30s", "-json", "-overlay=" + str(overlay),
            "-run=^TestIndependentBlocker", "./internal/central/work/contract"]
    (run / (mode + "-command.json")).write_text(json.dumps(cmd) + "\n")
    print(" ".join(cmd), flush=True)
    p = subprocess.Popen(cmd, env=env, cwd=repo, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                         text=True, start_new_session=True)
    timed_out = False
    try:
        log, _ = p.communicate(timeout=43)
    except subprocess.TimeoutExpired:
        timed_out = True
        os.killpg(p.pid, signal.SIGTERM)
        try:
            log, _ = p.communicate(timeout=1)
        except subprocess.TimeoutExpired:
            os.killpg(p.pid, signal.SIGKILL)
            log, _ = p.communicate(timeout=1)
    (run / (mode + ".jsonl")).write_text(log)
    (run / (mode + "-exit.txt")).write_text(f"actual_wait_returncode={p.returncode}\ntimeout={timed_out}\n")
    events = []
    for line in log.splitlines():
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            pass
    ran = {e.get("Test") for e in events if e.get("Action") == "run"}
    passed = {e.get("Test") for e in events if e.get("Action") == "pass"}
    bad = any(e.get("Action") in ("fail", "skip") for e in events)
    package_passed = any(e.get("Action") == "pass" and not e.get("Test") for e in events)
    unchanged = before == hashes()
    print(f"{mode}: actual wait={p.returncode}; timeout={timed_out}; "
          f"top run/pass={len(top & ran)}/{len(top & passed)} of 5; "
          f"subtests run/pass={len(children & ran)}/{len(children & passed)} of 4; "
          f"inputs_unchanged={unchanged}", flush=True)
    if timed_out or p.returncode or bad or not package_passed or not unchanged or not (top | children) <= (ran & passed):
        print(log[-18000:])
        raise SystemExit(1)
print("PASS: independent public API pure and race probes actually executed")
PY
