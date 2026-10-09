#!/usr/bin/env python3
"""Replay preserved private-schema probes against the current frozen input."""
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile

OWN = Path(__file__).resolve().parent
TREE = OWN.parent.parent
OUTPUT = TREE / "output/ai/task-planning-recovery"
OUTPUT.mkdir(parents=True, exist_ok=True)
RUN = Path(tempfile.mkdtemp(prefix="independent-runtime-private-", dir=OUTPUT))
sources = [OWN / "independent-runtime-private_test.go", OWN / "independent-runtime-private-matrix_test.go"]
paths = sources + sorted((TREE / "internal/central/work").glob("*.go"))
hashes = {str(p.relative_to(TREE)): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}
(RUN / "input-sha256.json").write_text(json.dumps(hashes, sort_keys=True, indent=2) + "\n")
overlay = RUN / "overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(TREE / "internal/central/work" / ("independent_private_" + str(n) + "_test.go")): str(p)
    for n, p in enumerate(sources)
}}))
command = [os.environ.get("AGENTEAM_GO", "/workspace/toolchains/go1.27.1/bin/go"), "test", "-race", "-p=2", "-count=1", "-timeout=25s", "-overlay", str(overlay), "-run", "^TestIndependentTaskPrivate(NestedKeys|ShapeMatrix|RawUnicode)$", "-v", "./internal/central/work"]
(RUN / "command.json").write_text(json.dumps(command) + "\n")
print(f"INDEPENDENT_PRIVATE_RUN={RUN}", flush=True)
with (RUN / "test.log").open("w") as log:
    child = subprocess.Popen(command, cwd=TREE, env=dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off"), stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        code = child.wait(timeout=40)
    except subprocess.TimeoutExpired:
        os.killpg(child.pid, signal.SIGKILL)
        child.wait()
        code = 124
for line in (RUN / "test.log").read_text().splitlines():
    if line.startswith(("--- ", "FAIL", "PASS", "ok\t")) or "malformed persisted" in line or "noncanonical nested" in line:
        print(line)
unchanged = all(hashlib.sha256((TREE / p).read_bytes()).hexdigest() == digest for p, digest in hashes.items())
print(f"INDEPENDENT_PRIVATE_EXIT={code}; CHILD_WAIT_CONFIRMED; INPUT_UNCHANGED={unchanged}", flush=True)
raise SystemExit(code if unchanged else 125)
