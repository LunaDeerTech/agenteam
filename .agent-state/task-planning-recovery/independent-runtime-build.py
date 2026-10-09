#!/usr/bin/env python3
"""Compile the two independent probes as one tests/work overlay; never run PG.

Each build gets a fresh ignored output directory; a source hash manifest binds
the compiled test binary. No database or container work occurs in this helper.
The B file is a declaration fragment sharing A's imports in the virtual source.
"""
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile

OWN = Path(__file__).resolve().parent
TREE = OWN.parent.parent
GO = Path(os.environ.get("AGENTEAM_GO", "/workspace/toolchains/go1.27.1/bin/go"))
ENV = dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off")
OUTPUT = TREE / "output/ai/task-planning-recovery"
OUTPUT.mkdir(parents=True, exist_ok=True)
BUILD = Path(tempfile.mkdtemp(prefix="independent-runtime-build-", dir=OUTPUT))


def run(argv, limit):
    child = subprocess.Popen(argv, cwd=TREE, env=ENV, start_new_session=True)
    try:
        code = child.wait(timeout=limit)
    except subprocess.TimeoutExpired:
        os.killpg(child.pid, signal.SIGKILL)
        child.wait()
        code = 124
    print(f"INDEPENDENT_RUNTIME_STEP_EXIT={code}; CHILD_WAIT_CONFIRMED", flush=True)
    if code:
        raise SystemExit(code)


a = OWN / "independent-runtime-ab_test.go"
b = OWN / "independent-runtime-b_test.go"
joined = BUILD / "probe_test.go"
joined.write_text(a.read_text() + "\n" + b.read_text().split("package work_test\n", 1)[1])
run([str(GO.with_name("gofmt")), "-w", str(joined)], 3)
overlay = BUILD / "overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(TREE / "tests/work/independent_runtime_test.go"): str(joined)
}}))
paths = [a, b]
paths += sorted((TREE / "tests/work").glob("*.go"))
paths += sorted((TREE / "internal/central/work").glob("*.go"))
paths += [TREE / "db/migrations/00022_task_planning.sql"]
hashes = {str(p.relative_to(TREE)): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}
(BUILD / "input-sha256.json").write_text(json.dumps(hashes, sort_keys=True, indent=2) + "\n")
binary = BUILD / "independent-runtime.test"
print(f"INDEPENDENT_RUNTIME_BUILD={BUILD}", flush=True)
run([str(GO), "test", "-tags=integration", "-race", "-p=2", "-c", "-overlay", str(overlay), "-o", str(binary), "./tests/work"], 40)
if any(hashlib.sha256((TREE / p).read_bytes()).hexdigest() != digest for p, digest in hashes.items()):
    raise SystemExit("INDEPENDENT_RUNTIME_INPUT_CHANGED")
run([str(binary), "-test.list", "^TestTaskPlanningIndependent(AuthorityMembership|CommitRank)$"], 3)
print("INDEPENDENT_RUNTIME_COMPILE_AND_LIST_ONLY=PASS; NO_PG_BODY_EXECUTED", flush=True)
