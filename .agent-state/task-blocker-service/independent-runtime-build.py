#!/usr/bin/env python3
"""Build/list the independent integration probes, without starting real resources."""
from pathlib import Path
import json
import os
import signal
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / "output/ai/task-blocker-service/independent"
OUT.mkdir(parents=True, exist_ok=True)
for name in ("gocache", "tmp"):
    (OUT / name).mkdir(exist_ok=True)
overlay = OUT / "runtime-overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(ROOT / f"tests/work/zz_independent_task_blocker_runtime_{part}_test.go"):
    str(ROOT / f".agent-state/task-blocker-service/independent-runtime-{part}_test.go")
    for part in ("a", "b")
}}, indent=2) + "\n")
env = os.environ.copy()
env.update(GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off", GOTELEMETRY="off",
           GOMODCACHE=str(ROOT / "output/ai/model-ui-recovery/go-mod"),
           GOCACHE=str(OUT / "gocache"), GOTMPDIR=str(OUT / "tmp"))
binary = OUT / "independent-runtime-race.test"
go = "/workspace/toolchains/go1.27.1/bin/go"
command = [go, "test", "-tags=integration", "-race", "-p=2", "-overlay=" + str(overlay),
           "-c", "-o", str(binary), "./tests/work"]
log_path = OUT / "runtime-build.log"
# Preserve failed commands; subsequent offline rebuilds get a distinct log.
count = 1
while log_path.exists():
    count += 1
    log_path = OUT / f"runtime-build-{count}.log"
with log_path.open("w") as log:
    proc = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log,
                            stderr=subprocess.STDOUT, start_new_session=True)
    try:
        code = proc.wait(timeout=180)
    except subprocess.TimeoutExpired:
        os.killpg(proc.pid, signal.SIGKILL)
        proc.wait()
        print("offline build timed out; process group killed and joined", flush=True)
        code = 124
print(f"build exit={code} log={log_path}", flush=True)
if code:
    print(log_path.read_text()[-12000:], flush=True)
    sys.exit(code)
listed = subprocess.run([str(binary), "-test.list=^TestIndependentTaskBlockerRuntime[AB]$"],
                        cwd=ROOT, env=env, text=True, capture_output=True, timeout=20)
(OUT / "runtime-list.log").write_text(listed.stdout + listed.stderr)
print(listed.stdout + listed.stderr, end="", flush=True)
print(f"list exit={listed.returncode}", flush=True)
sys.exit(listed.returncode)
