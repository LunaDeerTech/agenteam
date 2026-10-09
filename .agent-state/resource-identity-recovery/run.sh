#!/bin/sh
# Run a pure external-package probe through a Go overlay; never modify product files.
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

repo, probe_dir = map(Path, sys.argv[1:])
output = repo / "output/ai/resource-identity-recovery"
output.mkdir(parents=True, exist_ok=True)
run = Path(tempfile.mkdtemp(prefix="run-", dir=output))
for path in (output / "go-cache", run / "tmp", run / "config", run / "cache"):
    path.mkdir(parents=True, exist_ok=True)
overlay = run / "overlay.json"
overlay.write_text(json.dumps({"Replace": {
    str(repo / "internal/central/identity/contract/zz_independent_resource_identity_test.go"):
    str(probe_dir / "probe_test.go")
}}) + "\n")
env = os.environ.copy()
env.update(GOTOOLCHAIN="local", GOENV="off", GOPROXY="off", GOSUMDB="off",
           GOWORK="off", GOFLAGS="", GOMAXPROCS="2", CGO_ENABLED="1",
           GOCACHE=str(output / "go-cache"), GOMODCACHE="/workspace/go/pkg/mod",
           TMPDIR=str(run / "tmp"), GOTMPDIR=str(run / "tmp"),
           XDG_CONFIG_HOME=str(run / "config"), XDG_CACHE_HOME=str(run / "cache"),
           GOTELEMETRY="off")
go = "/workspace/toolchains/go1.27.1/bin/go"
version = subprocess.run([go, "version"], env=env, cwd=repo, capture_output=True, text=True, timeout=5)
if version.returncode or not version.stdout.startswith("go version go1.27.1 "):
    raise SystemExit("Go 1.27.1 is required")
print(version.stdout.strip(), flush=True)
cmd = [go, "test", "-race", "-count=1", "-p=2", "-timeout=30s", "-json",
       "-overlay=" + str(overlay), "-run=^TestIndependentResourceIdentity",
       "./internal/central/identity/contract"]
print(" ".join(cmd), flush=True)
print("evidence:", run, flush=True)
process = subprocess.Popen(cmd, env=env, cwd=repo, stdout=subprocess.PIPE,
                           stderr=subprocess.STDOUT, text=True, start_new_session=True)
timed_out = False
try:
    log, _ = process.communicate(timeout=43)
except subprocess.TimeoutExpired:
    timed_out = True
    os.killpg(process.pid, signal.SIGTERM)
    try:
        log, _ = process.communicate(timeout=1)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        log, _ = process.communicate(timeout=1)
(run / "go-test.jsonl").write_text(log)
(run / "exit.txt").write_text(f"wait_returncode={process.returncode}\ntimeout={timed_out}\n")
events = []
for line in log.splitlines():
    try:
        events.append(json.loads(line))
    except json.JSONDecodeError:
        pass
expected = {"TestIndependentResourceIdentityConsumers", "TestIndependentResourceIdentityScalars",
            "TestIndependentResourceIdentityRejection"}
expected_subtests = {f"{test}/{kind}"
                     for test in ("TestIndependentResourceIdentityScalars", "TestIndependentResourceIdentityRejection")
                     for kind in ("tool", "mount", "project-variable")}
ran = {e.get("Test") for e in events if e.get("Action") == "run"}
passed = {e.get("Test") for e in events if e.get("Action") == "pass"}
failed = [e for e in events if e.get("Action") in ("fail", "skip")]
package_passed = any(e.get("Action") == "pass" and not e.get("Test") for e in events)
print(f"actual wait returncode={process.returncode}; timeout={timed_out}; "
      f"probe tests run={len(expected & ran)}/3; passed={len(expected & passed)}/3; "
      f"subtests run={len(expected_subtests & ran)}/6; passed={len(expected_subtests & passed)}/6", flush=True)
if (timed_out or process.returncode or not expected <= ran or not expected <= passed
        or not expected_subtests <= ran or not expected_subtests <= passed or failed or not package_passed):
    print(log[-12000:])
    raise SystemExit(1)
print("PASS: all three independent probes and six subtests executed under race")
PY
