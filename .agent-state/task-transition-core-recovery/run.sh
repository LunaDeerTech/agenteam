#!/bin/sh
set -eu
probe_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo=$(CDPATH= cd -- "$probe_dir/../.." && pwd)
exec python3 - "$repo" "$probe_dir" <<'PY'
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

repo, probe = map(Path, sys.argv[1:])
output = repo / "output/ai/task-transition-core-recovery"
output.mkdir(parents=True, exist_ok=True)
run = Path(tempfile.mkdtemp(prefix="run-", dir=output))
for path in (output / "go-cache", run / "tmp", run / "config", run / "cache"):
    path.mkdir(parents=True, exist_ok=True)
env = os.environ.copy()
env.update(GOTOOLCHAIN="local", GOENV="off", GOPROXY="off", GOSUMDB="off", GOWORK="off",
           GOFLAGS="", GOMAXPROCS="2", CGO_ENABLED="1", GOCACHE=str(output / "go-cache"),
           GOMODCACHE="/home/agent/go/pkg/mod", TMPDIR=str(run / "tmp"), GOTMPDIR=str(run / "tmp"),
           XDG_CONFIG_HOME=str(run / "config"), XDG_CACHE_HOME=str(run / "cache"), GOTELEMETRY="off")
go = "/workspace/toolchains/go1.27.1/bin/go"
version = subprocess.run([go, "version"], env=env, cwd=repo, capture_output=True, text=True, timeout=5)
if version.returncode or not version.stdout.startswith("go version go1.27.1 "):
    raise SystemExit("Go 1.27.1 required")
overlay = run / "overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(repo / "internal/central/work/contract/zz_independent_task_transition_test.go"):
    str(probe / "probe_test.go")
}}) + "\n")
cmd = [go, "test", "-race", "-count=1", "-p=2", "-timeout=30s", "-json", "-overlay=" + str(overlay),
       "-run=^TestIndependentTaskTransition", "./internal/central/work/contract"]
print(version.stdout.strip(), flush=True)
print(" ".join(cmd), flush=True)
print("evidence:", run, flush=True)
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
(run / "go-test.jsonl").write_text(log)
(run / "exit.txt").write_text(f"wait_returncode={p.returncode}\ntimeout={timed_out}\n")
events = []
for line in log.splitlines():
    try:
        events.append(json.loads(line))
    except json.JSONDecodeError:
        pass
top = {"TestIndependentTaskTransition" + name for name in ("Roles", "Precedence", "Position", "ValueSafety")}
children = {f"TestIndependentTaskTransitionRoles/current-{n}" for n in range(3)}
children |= {f"TestIndependentTaskTransitionValueSafety/reader-{n}" for n in range(6)}
ran = {e.get("Test") for e in events if e.get("Action") == "run"}
passed = {e.get("Test") for e in events if e.get("Action") == "pass"}
bad = any(e.get("Action") in ("fail", "skip") for e in events)
package_passed = any(e.get("Action") == "pass" and not e.get("Test") for e in events)
print(f"actual wait returncode={p.returncode}; timeout={timed_out}; "
      f"top run/pass={len(top & ran)}/{len(top & passed)} of 4; "
      f"subtests run/pass={len(children & ran)}/{len(children & passed)} of 9", flush=True)
if timed_out or p.returncode or bad or not package_passed or not (top | children) <= (ran & passed):
    print(log[-16000:])
    raise SystemExit(1)
print("PASS: independent Task transition pure probes executed under race")
PY
