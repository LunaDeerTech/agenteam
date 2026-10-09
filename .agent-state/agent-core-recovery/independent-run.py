#!/usr/bin/env python3
"""Run only independent pure C1 probes using a Go overlay; no network or PG."""
from pathlib import Path
import json
import os
import signal
import subprocess
import tempfile

OWN = Path(__file__).resolve().parent
ROOT = OWN.parent.parent
GO = Path(os.environ.get("AGENTEAM_GO", "/workspace/toolchains/go1.27.1/bin/go"))
OUTPUT = ROOT / "output/ai/agent-core-recovery"
OUTPUT.mkdir(parents=True, exist_ok=True)
RUN = Path(tempfile.mkdtemp(prefix="independent-", dir=OUTPUT))
overlay = RUN / "overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(ROOT / "internal/central/agent/contract/independent_core_test.go"):
    str(OWN / "independent-core_test.go")
}}))
command = [str(GO), "test", "-race", "-count=1", "-p=2", "-timeout=30s",
           "-overlay", str(overlay), "-run", "^TestIndependentAgentC1", "-v",
           "./internal/central/agent/contract"]
(RUN / "command.json").write_text(json.dumps({"reviewed_revision": "8819779f", "command": command}, indent=2) + "\n")
env = dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off")
print("INDEPENDENT_C1_OUTPUT=" + str(RUN), flush=True)
with (RUN / "test.log").open("w") as log:
    child = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log,
                             stderr=subprocess.STDOUT, start_new_session=True)
    try:
        code = child.wait(timeout=42)
    except subprocess.TimeoutExpired:
        os.killpg(child.pid, signal.SIGKILL)
        child.wait()
        code = 124
print((RUN / "test.log").read_text(), end="", flush=True)
print("INDEPENDENT_C1_EXIT=" + str(code) + "; ACTUAL_CHILD_WAIT=true; NO_PG_OR_HTTP", flush=True)
raise SystemExit(code)
