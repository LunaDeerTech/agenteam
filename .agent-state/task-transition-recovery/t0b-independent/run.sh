#!/bin/sh
set -eu
probe_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo=$(CDPATH= cd -- "$probe_dir/../../.." && pwd)
exec python3 - "$repo" "$probe_dir" <<'PY'
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

repo, probe = map(Path, sys.argv[1:])
output = repo / "output/ai/task-transition-recovery/t0b-independent"
output.mkdir(parents=True, exist_ok=True)
run = Path(tempfile.mkdtemp(prefix="run-", dir=output))
cache = Path(os.environ.get("AGENTEAM_T0B_GOCACHE", str(output / "go-cache")))
for path in (cache, run / "tmp", run / "config", run / "cache"):
    path.mkdir(parents=True, exist_ok=True)
env = os.environ.copy()
env.update(GOTOOLCHAIN="local", GOENV="off", GOPROXY="off", GOSUMDB="off", GOWORK="off",
           GOFLAGS="", GOMAXPROCS="2", CGO_ENABLED="1", GOCACHE=str(cache),
           GOMODCACHE="/home/agent/go/pkg/mod", TMPDIR=str(run / "tmp"), GOTMPDIR=str(run / "tmp"),
           XDG_CONFIG_HOME=str(run / "config"), XDG_CACHE_HOME=str(run / "cache"), GOTELEMETRY="off")
go = os.environ.get("AGENTEAM_GO", "/workspace/toolchains/go1.27.1/bin/go")
version = subprocess.run([go, "version"], env=env, cwd=repo, capture_output=True, text=True, timeout=5)
if version.returncode or not version.stdout.startswith("go version go1.27.1 "):
    raise SystemExit("Go 1.27.1 required")
(run / "input-revision.txt").write_text(os.environ.get("AGENTEAM_T0B_INPUT_REVISION", "Revision not supplied; consult root's source-freeze record") + "\n")
(run / "probe_test.go").write_bytes((probe / "probe_test.go").read_bytes())
overlay = run / "overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(repo / "internal/central/work/contract/zz_independent_t0b_test.go"):
    str(probe / "probe_test.go")
}}) + "\n")
top = {"TestIndependentT0b" + name for name in (
    "Digest", "Factory", "RawBoundaries", "Envelope", "CapsAndClones", "LegacyAndLog")}
factory_cases = (
    "other-legal-edge", "other-assignee-pair", "assignee-nil-mismatch", "comment-not-request",
    "resolve-id", "resolution-comment", "add-type", "add-id", "missing", "extra", "order",
    "duplicate-id", "actor", "operation", "history-time", "history-version", "project", "task",
    "business-field", "version-step", "missing-handoff", "missing-review", "target-next",
    "source-self", "source-state", "target-priority", "header-aggregate", "header-project",
    "header-version", "header-sequence")
children = {"TestIndependentT0bFactory/" + name for name in factory_cases}
children |= {f"TestIndependentT0bCapsAndClones/reader-{n}" for n in range(4)}
print(version.stdout.strip(), flush=True)
print("evidence:", run, flush=True)
for mode in ("pure", "race", "vet"):
    if mode == "vet":
        cmd = [go, "vet", "-p=2", "-overlay=" + str(overlay), "./internal/central/work/contract"]
    else:
        cmd = [go, "test"] + (["-race"] if mode == "race" else [])
        cmd += ["-count=1", "-p=2", "-timeout=30s", "-json", "-overlay=" + str(overlay),
                "-run=^TestIndependentT0b", "./internal/central/work/contract"]
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
    if mode == "vet":
        print(f"vet: actual wait={p.returncode}; timeout={timed_out}", flush=True)
        if timed_out or p.returncode:
            print(log[-22000:])
            raise SystemExit(1)
        continue
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
    print(f"{mode}: actual wait={p.returncode}; timeout={timed_out}; "
          f"top run/pass={len(top & ran)}/{len(top & passed)} of {len(top)}; "
          f"subtest run/pass={len(children & ran)}/{len(children & passed)} of {len(children)}", flush=True)
    if timed_out or p.returncode or bad or not package_passed or not (top | children) <= (ran & passed):
        print(log[-22000:])
        raise SystemExit(1)
print("PASS: independent T0b public API pure/race probes and vet actually executed")
PY
