#!/usr/bin/env python3
"""Offline compile/list only. Run after the parent freezes the package inputs."""
from pathlib import Path
import json
import os
import signal
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / "output/ai/work-owner-http/independent"
CACHE = ROOT / "output/ai/task-blocker-service/independent/gocache"
TOP = "TestIndependentWorkOwnerBlockerPagination"
OUT.mkdir(parents=True, exist_ok=True)
(OUT / "tmp").mkdir(exist_ok=True)
CACHE.mkdir(parents=True, exist_ok=True)
overlay = OUT / "pager-overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(ROOT / "tests/work/zz_independent_work_owner_pager_test.go"):
    str(ROOT / ".agent-state/work-owner-http/independent-pager_test.go")
}}, indent=2) + "\n")
env = os.environ.copy()
env.update(GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off", GOTELEMETRY="off",
           GOMODCACHE=str(ROOT / "output/ai/model-ui-recovery/go-mod"),
           GOCACHE=str(CACHE), GOTMPDIR=str(OUT / "tmp"))
binary = OUT / "independent-pager-race.test"
command = ["/workspace/toolchains/go1.27.1/bin/go", "test", "-tags=integration",
           "-race", "-p=2", "-overlay=" + str(overlay), "-c", "-o", str(binary),
           "./tests/work"]
number = 1
while (OUT / f"pager-build-{number}.log").exists():
    number += 1
log_path = OUT / f"pager-build-{number}.log"
with log_path.open("w") as log:
    proc = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log,
                            stderr=subprocess.STDOUT, start_new_session=True)
    print(f"offline build pid={proc.pid} log={log_path}", flush=True)
    timed_out = False
    try:
        code = proc.wait(timeout=180)
    except subprocess.TimeoutExpired:
        timed_out = True
        os.killpg(proc.pid, signal.SIGKILL)
        code = proc.wait()
    log.write(f"\nactualWait={code} timedOut={timed_out}\n")
print(f"build actualWait={code} timedOut={timed_out}", flush=True)
if code != 0 or timed_out:
    print(log_path.read_text()[-12000:], flush=True)
    sys.exit(124 if timed_out else code)

# This package has no TestMain: -test.list does not execute any resource setup.
with (OUT / f"pager-list-{number}.log").open("w") as log:
    proc = subprocess.Popen([str(binary), "-test.list=^" + TOP + "$"],
                            cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, start_new_session=True)
    timed_out = False
    try:
        listing, _ = proc.communicate(timeout=20)
    except subprocess.TimeoutExpired:
        timed_out = True
        os.killpg(proc.pid, signal.SIGKILL)
        listing, _ = proc.communicate()
    log.write(listing)
    log.write(f"\nactualWait={proc.returncode} timedOut={timed_out}\n")
print(listing, end="", flush=True)
print(f"list actualWait={proc.returncode} timedOut={timed_out}", flush=True)
if timed_out or proc.returncode != 0 or TOP not in listing.splitlines():
    sys.exit(124 if timed_out else 1)
