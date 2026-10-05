#!/usr/bin/env python3
"""Replay only the independent R1 probes against their frozen product input."""

import json
import os
from pathlib import Path
import subprocess
import tempfile


evidence = Path(__file__).resolve().parent
repo = evidence.parents[4]
frozen_commit = "98262b49aa87c52cfb0c09586dd1b76f9aa1fd52"
changed = subprocess.check_output(
    ["git", "diff", "--name-only", frozen_commit, "--", ".", ":(exclude)docs"],
    cwd=repo,
    text=True,
)
untracked = subprocess.check_output(
    ["git", "ls-files", "--others", "--exclude-standard"], cwd=repo, text=True
).splitlines()
if changed or any(p.endswith(".go") and not p.startswith("docs/") for p in untracked):
    raise SystemExit("Replay requires the frozen product input at " + frozen_commit)

go = os.environ.get("AGENTEAM_GO", "/workspace/toolchains/go1.27.1/bin/go")
env = {
    key: os.environ[key]
    for key in ("PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "TZ")
    if key in os.environ
}
env.update(
    AGENTEAM_GO=go,
    GOTOOLCHAIN="local",
    GOENV="off",
    GOWORK="off",
    GOPROXY="off",
    GOSUMDB="off",
    GOFLAGS="-mod=readonly -p=2",
    GOMODCACHE=os.environ.get(
        "GOMODCACHE", "/workspace/agenteam-dependency-cache/modcache"
    ),
    GOOS="linux",
    GOARCH="amd64",
    GOAMD64="v1",
    CGO_ENABLED="1",
)
if subprocess.check_output([go, "env", "GOVERSION"], env=env, text=True).strip() != "go1.27.1":
    raise SystemExit("Go 1.27.1 is required")
with tempfile.TemporaryDirectory(prefix="agenteam-d08-r1-replay-") as directory:
    owned = Path(directory)
    env["GOCACHE"] = os.environ.get("GOCACHE", str(owned / "go-cache"))
    env["TMPDIR"] = env["GOTMPDIR"] = str(owned)
    overlay = owned / "overlay.json"
    overlay.write_text(
        json.dumps(
            {
                "Replace": {
                    str(repo / "internal/central/project/r1_independent_probe_test.go"):
                    str(evidence / "r1_independent_probe.go.txt")
                }
            }
        )
    )
    result = subprocess.run(
        [
            go, "test", "-race", "-count=1", "-timeout=2m", "-v", "-overlay", str(overlay),
            "-run", "^TestIndependentR1(FrozenManifestCompatibility|PlanIsolation)$",
            "./internal/central/project",
        ],
        cwd=repo,
        env=env,
    )
raise SystemExit(result.returncode)
