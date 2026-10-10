#!/usr/bin/env python3
"""Run only the four authorized offline Secret HTTP delivery checks."""
import datetime
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
GO = "/workspace/toolchains/go1.27.1/bin/go"
OUTPUT = ROOT / "output/ai/secret-owner-http/delivery-offline-01"
EVIDENCE_CREATED = False
PROGRESS = {}


def main():
    global EVIDENCE_CREATED
    if ROOT != Path("/workspace/agenteam-secret-owner-http") or OUTPUT.exists():
        raise ValueError("unexpected tree or existing evidence directory")
    fresh = shutil.disk_usage(ROOT).free
    if fresh < 5 * 1024**3:
        raise ValueError(f"fresh disk below 5 GiB: {fresh}")
    OUTPUT.mkdir(mode=0o700)
    EVIDENCE_CREATED = True
    runtime = OUTPUT / "runtime-env"
    config = runtime / "go-config"
    go_config = config / "go"
    telemetry = go_config / "telemetry"
    temporary = runtime / "tmp"
    for directory in (runtime, config, go_config, telemetry, temporary):
        directory.mkdir(mode=0o700)
    mode = telemetry / "mode"
    with mode.open("x") as stream:
        stream.write("off\n")
    mode.chmod(0o600)
    env = {key: os.environ[key] for key in ("PATH", "LANG", "LC_ALL", "TZ") if key in os.environ}
    env.update(
        GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off",
        GOMODCACHE="/workspace/shared/agenteam-deps/go-mod",
        GOCACHE=str(ROOT / "output/ai/secret-owner-http/go-build"),
        GOFLAGS="-mod=readonly -p=2", GOMAXPROCS="2",
        XDG_CONFIG_HOME=str(config), TMPDIR=str(temporary), GOTMPDIR=str(temporary),
        PYTHONDONTWRITEBYTECODE="1",
    )
    packages = ["./internal/central/app", "./internal/central/projectvariable",
                "./internal/central/projectvariable/http", "./internal/central/secret"]
    commands = [
        ("ordinary-vet", [GO, "vet", *packages]),
        ("integration-vet", [GO, "vet", "-tags=integration", *packages, "./tests/projectvariable"]),
        ("central-build", [GO, "build", "-o", str(OUTPUT / "agenteam"), "./cmd/agenteam"]),
        ("runner-build", [GO, "build", "-o", str(OUTPUT / "agenteam-runner"), "./cmd/agenteam-runner"]),
    ]
    result = dict(source="109c4db8", utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                  outer_pid=os.getpid(), fresh_free_bytes=fresh, stages=[])
    PROGRESS.update(result)
    started = time.monotonic()
    code = 0
    print(json.dumps(dict(result, phase="started")), flush=True)
    for name, command in commands:
        before = time.monotonic()
        with (OUTPUT / (name + ".log")).open("xb") as log:
            child = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
            PROGRESS.update(active_stage=name, active_pid=child.pid)
            print(json.dumps(dict(stage=name, pid=child.pid, command=command)), flush=True)
            waited = child.wait()
        entry = dict(stage=name, command=command, pid=child.pid, actual_wait=waited,
                     seconds=round(time.monotonic() - before, 3))
        result["stages"].append(entry)
        PROGRESS.pop("active_pid", None)
        print(json.dumps(entry), flush=True)
        if waited != 0:
            code = waited
            break
    observations = [not any(temporary.iterdir()) for _ in (1, 2)]
    if not all(observations):
        code = 1
    result.update(seconds=round(time.monotonic() - started, 3),
                  runtime_observations=observations, outer_terminal=code)
    (OUTPUT / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result), flush=True)
    return code


if __name__ == "__main__":
    try:
        terminal = main()
    except Exception as error:
        # Keep the first failure; never advance to later commands or overwrite
        # an existing run. Exception text may contain uncontrolled paths.
        failure = dict(PROGRESS, outer_terminal=1, error_type=type(error).__name__)
        if EVIDENCE_CREATED:
            try:
                with (OUTPUT / "entry-failure.json").open("x") as stream:
                    json.dump(failure, stream, indent=2)
                    stream.write("\n")
            except OSError:
                failure["failure_record_write_failed"] = True
        print(json.dumps(failure), flush=True)
        terminal = 1
    raise SystemExit(terminal)
