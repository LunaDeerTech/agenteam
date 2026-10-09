#!/usr/bin/env python3
"""Overlay independent tests onto a stable delivery tree without source edits.

AGENTEAM_INDEPENDENT_CONTRACT_TREE selects an equivalent isolated checkout.
The default full selector intentionally retains the two original broad logging
claim failures. Use an explicit test selector to verify supported projections.
"""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

OWN = Path(__file__).resolve().parent
DELIVERY = Path(os.environ.get("AGENTEAM_INDEPENDENT_CONTRACT_TREE", "/workspace/agenteam-delivery")).resolve()
GO = os.environ.get("AGENTEAM_GO", "/workspace/toolchains/go1.27.1/bin/go")

with tempfile.TemporaryDirectory(prefix="independent-contract-run-", dir=OWN) as name:
    overlay = Path(name) / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {
        str(DELIVERY / "internal/central/work/contract/independent_probe_test.go"):
            str(OWN / "independent-contract-probe_test.go"),
        str(DELIVERY / "internal/central/project/independent_task_gate_test.go"):
            str(OWN / "independent-contract-project_test.go")
    }}))
    selector = sys.argv[1] if len(sys.argv) > 1 else "^TestIndependentTask"
    command = [GO, "test", "-overlay", str(overlay), "-race", "-count=1", "-timeout=35s", "-v", "-run", selector, "./internal/central/work/contract", "./internal/central/project"]
    env = dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off")
    child = subprocess.Popen(command, cwd=DELIVERY, env=env, start_new_session=True)
    try:
        result = child.wait(timeout=44)
    except subprocess.TimeoutExpired:
        os.killpg(child.pid, signal.SIGKILL)
        child.wait()
        result = 124
    print(f"INDEPENDENT_CONTRACT_EXIT={result}; CHILD_WAIT_CONFIRMED", flush=True)
raise SystemExit(result)
