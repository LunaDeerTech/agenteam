#!/bin/sh
# Pure overlay only: no test invokes a server, browser, or database fixture.
set -eu
probe_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 - "$probe_dir" <<'PY'
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

probe = Path(sys.argv[1])
source = probe.parents[2]
delivery = Path(os.environ.get("MODEL_LOOKUP_DELIVERY", "/workspace/agenteam-delivery"))
for name in ("project_owner_models_web_fixture_test.go", "project_owner_models_web_test.go"):
    relative = Path("tests/account") / name
    if (source / relative).read_bytes() != (delivery / relative).read_bytes():
        raise SystemExit("Frozen Model Go input differs from delivery input")
output = source / "output/ai/resource-identity-recovery/model-lookup"
output.mkdir(parents=True, exist_ok=True)
run = Path(tempfile.mkdtemp(prefix="run-", dir=output))
for name in ("tmp", "config", "cache"):
    (run / name).mkdir()
cache = os.environ.get("MODEL_LOOKUP_GOCACHE", str(output / "go-cache"))
env = os.environ.copy()
env.update(GOTOOLCHAIN="local", GOENV="off", GOPROXY="off", GOSUMDB="off", GOWORK="off",
           GOFLAGS="", GOMAXPROCS="2", CGO_ENABLED="1", GOCACHE=cache,
           GOMODCACHE=os.environ.get("MODEL_LOOKUP_GOMODCACHE", "/home/agent/go/pkg/mod"),
           TMPDIR=str(run / "tmp"), GOTMPDIR=str(run / "tmp"),
           XDG_CONFIG_HOME=str(run / "config"), XDG_CACHE_HOME=str(run / "cache"), GOTELEMETRY="off")
go = "/workspace/toolchains/go1.27.1/bin/go"
version = subprocess.run([go, "version"], cwd=delivery, env=env, text=True, capture_output=True, timeout=5)
if version.returncode or not version.stdout.startswith("go version go1.27.1 "):
    raise SystemExit("Go 1.27.1 required")
overlay = run / "overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(delivery / "tests/account/zz_independent_model_lookup_test.go"):
    str(probe / "independent-lookup-probe_test.go")
}}) + "\n")
cmd = [go, "test", "-tags=integration", "-race", "-count=1", "-p=2", "-timeout=30s", "-json",
       "-overlay=" + str(overlay), "-run=^TestIndependentModelLookup", "./tests/account"]
print(version.stdout.strip(), flush=True)
print(" ".join(cmd), flush=True)
print("evidence:", run, flush=True)
p = subprocess.Popen(cmd, env=env, cwd=delivery, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
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
(run / "go-test.jsonl").write_text(log)
(run / "exit.txt").write_text(f"wait_returncode={p.returncode}\ntimeout={timed_out}\n")
events = []
for line in log.splitlines():
    try:
        events.append(json.loads(line))
    except json.JSONDecodeError:
        pass
top = {"TestIndependentModelLookup" + suffix for suffix in ("Controls", "ClosedUnion", "ResponseBinding")}
children = {name + "/" + family for name in top for family in ("configuration", "credential")}
ran = {e.get("Test") for e in events if e.get("Action") == "run"}
passed = {e.get("Test") for e in events if e.get("Action") == "pass"}
bad = any(e.get("Action") in ("fail", "skip") for e in events)
package_passed = any(e.get("Action") == "pass" and not e.get("Test") for e in events)
print(f"actual wait returncode={p.returncode}; timeout={timed_out}; "
      f"top run/pass={len(top & ran)}/{len(top & passed)} of 3; "
      f"subtests run/pass={len(children & ran)}/{len(children & passed)} of 6", flush=True)
if timed_out or p.returncode or bad or not package_passed or not (top | children) <= (ran & passed):
    print(log[-16000:])
    raise SystemExit(1)
print("PASS: independent lookup pure admission probes executed under race; no browser recovery claim")
PY
