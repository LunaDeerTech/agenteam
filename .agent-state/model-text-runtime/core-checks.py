#!/usr/bin/env python3
"""Fixed offline Runtime core unit/race/vet; no socket or schema subprocesses."""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
GO = "/workspace/toolchains/go1.27.1/bin/go"
SELECTOR = "^(TestRuntime.*|TestModelSecretRouterPreservesFallbackAndDoesNotGuessLeasePurpose)$"
PACKAGES = ["./internal/central/model"]
COMMANDS = [
    ("ordinary", [GO, "test", "-mod=readonly", "-p=2", "-count=1", "-timeout=90s", "-json", "-run", SELECTOR, *PACKAGES]),
    ("race", [GO, "test", "-mod=readonly", "-p=2", "-race", "-count=1", "-timeout=90s", "-json", "-run", SELECTOR, *PACKAGES]),
    ("vet", [GO, "vet", "-mod=readonly", "-p=2", *PACKAGES]),
]


def emit(value):
    print(json.dumps(value, ensure_ascii=False), flush=True)


def group_absent(pid):
    try:
        os.killpg(pid, 0)
        return False
    except ProcessLookupError:
        return True


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in ("core-01", "core-02", "regression-01", "regression-02", "regression-03", "scope-grant-01", "candidate-01"):
        raise SystemExit("exact evidence directory required")
    commands = COMMANDS
    selector = SELECTOR
    if sys.argv[1] in ("regression-01", "regression-02", "regression-03"):
        selector = "^TestRuntime(ActiveDuplicateKeepsOriginalAdmissionContext|StartFailurePrecedesGateHandoff)$"
        commands = [
            (name, [selector if item == SELECTOR else item for item in argv])
            for name, argv in COMMANDS if name != "vet" or sys.argv[1] != "regression-01"
        ]
    elif sys.argv[1] == "scope-grant-01":
        selector = "^TestRuntimeSecretGrantRespectsAuditScope$"
        commands = [(name, [selector if item == SELECTOR else item for item in argv]) for name, argv in COMMANDS]
    out = ROOT / "output/ai/model-text-runtime" / sys.argv[1]
    binary = out / "model-runtime.test"
    if sys.argv[1] == "candidate-01":
        selector = "^TestModelTextRuntimePersistentWire$"
        commands = [
            ("build", [GO, "test", "-mod=readonly", "-p=2", "-race", "-tags=integration", "-c", "-o", str(binary), "./tests/model"]),
            ("list", [str(binary), "-test.list=" + selector]),
        ]
    out.mkdir(parents=True, exist_ok=False)
    out.chmod(0o700)
    runtime = out / "runtime"
    config = out / "go-config"
    runtime.mkdir(mode=0o700)
    config.mkdir(mode=0o700)
    (config / "go").mkdir(mode=0o700)
    (config / "go/telemetry").mkdir(mode=0o700)
    mode = config / "go/telemetry/mode"
    with mode.open("x") as stream:
        stream.write("off\n")
    mode.chmod(0o600)
    cache = ROOT / "output/ai/model-text-runtime/go-build"
    cache.mkdir(parents=True, exist_ok=True)
    env = {"PATH": str(Path(GO).parent) + ":/usr/bin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "TZ": "UTC", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off", "GOMODCACHE": "/workspace/shared/agenteam-deps/go-mod", "GOCACHE": str(cache), "GOTMPDIR": str(runtime), "TMPDIR": str(runtime), "XDG_CONFIG_HOME": str(config), "GOMAXPROCS": "2", "CGO_ENABLED": "1"}
    # No inherited TEST_TELEMETRY_DIR or any user/global config override.
    if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0:
        raise OSError(ctypes.get_errno(), "subreaper unavailable")
    result = {"outer_pid": os.getpid(), "started_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "selector": selector, "phases": [], "whole_pass": False}
    code = 1
    try:
        for name, command in commands:
            stat = os.statvfs(ROOT)
            available = stat.f_bavail * stat.f_frsize
            phase = {"name": name, "available_bytes": available, "argv": command}
            result["phases"].append(phase)
            if available < 5 * 1024**3:
                phase["preflight_failure"] = "disk_below_5GiB_no_go"
                break
            start = time.monotonic()
            with (out / (name + ".jsonl")).open("x") as log:
                process = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                phase["go_pid"] = process.pid
                emit({"outer_pid": os.getpid(), "phase": name, "go_pid": process.pid, "utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "available_bytes": available})
                try:
                    phase["actual_wait"] = process.wait(timeout=300)
                except subprocess.TimeoutExpired:
                    phase["timeout"] = True
                    os.killpg(process.pid, signal.SIGTERM)
                    try:
                        phase["actual_wait"] = process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        phase["actual_wait"] = process.wait()
            phase["elapsed_seconds"] = round(time.monotonic() - start, 3)
            phase["group_absent"] = group_absent(process.pid)
            if not phase["group_absent"]:
                # Preserve this failure even if owned descendants later retire.
                os.killpg(process.pid, signal.SIGTERM)
            adopted = []
            reap_deadline = time.monotonic() + 5
            killed = False
            while True:
                try:
                    pid, status = os.waitpid(-1, os.WNOHANG)
                except ChildProcessError:
                    break
                if pid == 0:
                    phase["unjoined_child"] = True
                    if time.monotonic() >= reap_deadline:
                        if killed:
                            break
                        if not group_absent(process.pid):
                            os.killpg(process.pid, signal.SIGKILL)
                        killed = True
                        reap_deadline = time.monotonic() + 5
                    time.sleep(0.02)
                    continue
                adopted.append({"pid": pid, "actual_wait": os.waitstatus_to_exitcode(status)})
            phase["adopted_waits"] = adopted
            phase["group_empty_tail"] = [group_absent(process.pid), group_absent(process.pid)]
            phase["runtime_empty"] = [not any(runtime.iterdir()), not any(runtime.iterdir())]
            if name == "list":
                phase["exact_top"] = (out / "list.jsonl").read_text().splitlines() == ["TestModelTextRuntimePersistentWire"]
                with binary.open("rb") as stream:
                    phase["candidate_sha256"] = hashlib.file_digest(stream, "sha256").hexdigest()
                phase["candidate_bytes"] = binary.stat().st_size
            emit(phase)
            if phase.get("timeout") or phase["actual_wait"] != 0 or not phase["group_absent"] or phase.get("unjoined_child") or adopted or not all(phase["runtime_empty"]) or phase.get("exact_top") is False:
                break
        else:
            result["whole_pass"], code = True, 0
    except Exception as error:
        result["wrapper_failure"] = type(error).__name__
    finally:
        result["exit_code"] = code
        result["finished_utc"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        (out / "result.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
        emit({"whole_pass": result["whole_pass"], "exit_code": code, "result": str(out / "result.json")})
    return code


if __name__ == "__main__":
    raise SystemExit(main())
