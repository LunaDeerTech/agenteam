#!/usr/bin/env python3
"""Two fixed offline checks; cache ownership is assigned by the coordinator."""
import argparse
import ctypes
import json
import os
from pathlib import Path
import signal
import subprocess
import time

ROOT = Path('/workspace/agenteam-task-flow-delivery')
GO = "/workspace/toolchains/go1.27.1/bin/go"
TOPS = {
    'app': [
        'TestWorkPlanningPureConstructionAndDrain',
        'TestWorkPlanningTransitionActualCallMustJoin',
        'TestWorkPlanningSprintLifecycleActualCallMustJoin',
        'TestSkillManagementRootRoutesKeepReadCompatibility',
    ],
}
SELECTOR = "^(" + "|".join(top for tops in TOPS.values() for top in tops) + ")$"
PACKAGES = ["./internal/central/" + package for package in TOPS]
COMMANDS = [
    ("race", [GO, "test", "-mod=readonly", "-p=2", "-race", "-count=1", "-timeout=90s", "-json", "-run", SELECTOR, *PACKAGES]),
    ("vet", [GO, "vet", "-mod=readonly", "-p=2", "./internal/central/app"]),
]


def emit(value):
    print(json.dumps(value, ensure_ascii=False), flush=True)


def utc():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def group_absent(pid):
    try:
        os.killpg(pid, 0)
        return False
    except ProcessLookupError:
        return True


def signal_group(pid, value):
    try:
        os.killpg(pid, value)
    except ProcessLookupError:
        pass


def race_summary(path):
    observed = {}
    for line in path.read_text().splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        test = event.get("Test", "")
        if test and "/" not in test and event.get("Action") in ("pass", "fail", "skip"):
            observed[event["Package"] + "/" + test] = event["Action"]
    expected = {
        "github.com/LunaDeerTech/agenteam/internal/central/" + package + "/" + top: "pass"
        for package, tops in TOPS.items() for top in tops
    }
    return {"tops": observed, "exact_4_top_pass": observed == expected}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--cache", type=Path, required=True)
    args = parser.parse_args()
    cache = args.cache
    if not cache.is_absolute() or not cache.is_dir() or cache.resolve() != cache:
        parser.error("an existing coordinator-assigned absolute cache is required")
    out = ROOT / "output/ai/task-flow-delivery/app-pure-01"
    out.mkdir(parents=True, exist_ok=False)
    out.chmod(0o700)
    runtime = out / "runtime"
    config = out / "go-config"
    runtime.mkdir(mode=0o700)
    config.mkdir(mode=0o700)
    (config / "go").mkdir(mode=0o700)
    (config / "go/telemetry").mkdir(mode=0o700)
    with (config / "go/telemetry/mode").open("x") as stream:
        stream.write("off\n")
    (config / "go/telemetry/mode").chmod(0o600)
    env = os.environ.copy()
    for name in ("TEST_TELEMETRY_DIR", "GOTELEMETRY", "GOTELEMETRYDIR", "GO_TELEMETRY_CHILD", "GO_TELEMETRY_CHILD_UPLOAD"):
        env.pop(name, None)
    env.update({
        "PATH": str(Path(GO).parent) + ":/usr/bin:/bin", "LANG": "C.UTF-8",
        "LC_ALL": "C.UTF-8", "TZ": "UTC", "GOTOOLCHAIN": "local",
        "GOPROXY": "off", "GOSUMDB": "off",
        "GOMODCACHE": "/workspace/shared/agenteam-deps/go-mod",
        "GOCACHE": str(cache), "GOTMPDIR": str(runtime), "TMPDIR": str(runtime),
        "XDG_CONFIG_HOME": str(config), "GOMAXPROCS": "2", "CGO_ENABLED": "1",
        "AGENTEAM_WORK_HTTP_SCHEMA_PYTHON": "/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3",
    })
    # Preserve normal HOME/TLS; fixed offline/private values above override Go state.
    result = {"outer_pid": os.getpid(), "started_utc": utc(), "selector": SELECTOR,
              "cache": str(cache), "source": None, "phases": [], "whole_pass": False}
    code = 1
    try:
        result["source"] = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
        if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0:
            raise OSError(ctypes.get_errno(), "subreaper unavailable")
        for name, command in COMMANDS:
            stat = os.statvfs(ROOT)
            available = stat.f_bavail * stat.f_frsize
            phase = {"name": name, "available_bytes": available, "argv": command}
            result["phases"].append(phase)
            if available < 5 * 1024**3:
                phase["preflight_failure"] = "disk_below_5GiB_no_go"
                break
            start = time.monotonic()
            log_path = out / (name + ".jsonl")
            with log_path.open("x") as log:
                process = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log,
                                           stderr=subprocess.STDOUT, start_new_session=True)
                phase["go_pid"] = process.pid
                emit({"outer_pid": os.getpid(), "phase": name, "go_pid": process.pid,
                      "utc": utc(), "available_bytes": available})
                try:
                    phase["actual_wait"] = process.wait(timeout=300)
                except subprocess.TimeoutExpired:
                    phase["timeout"] = True
                    signal_group(process.pid, signal.SIGTERM)
                    try:
                        phase["actual_wait"] = process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        signal_group(process.pid, signal.SIGKILL)
                        phase["actual_wait"] = process.wait()
                finally:
                    if process.poll() is None:
                        phase["interrupted"] = True
                        signal_group(process.pid, signal.SIGKILL)
                        phase["actual_wait"] = process.wait()
            phase["elapsed_seconds"] = round(time.monotonic() - start, 3)
            phase["group_absent"] = group_absent(process.pid)
            if not phase["group_absent"]:
                signal_group(process.pid, signal.SIGTERM)
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
                        signal_group(process.pid, signal.SIGKILL)
                        killed = True
                        reap_deadline = time.monotonic() + 5
                    time.sleep(0.02)
                    continue
                adopted.append({"pid": pid, "actual_wait": os.waitstatus_to_exitcode(status)})
            phase["adopted_waits"] = adopted
            phase["group_empty_tail"] = [group_absent(process.pid), group_absent(process.pid)]
            phase["runtime_empty"] = [not any(runtime.iterdir()), not any(runtime.iterdir())]
            if name == "race":
                phase.update(race_summary(log_path))
            emit(phase)
            if (phase.get("timeout") or phase.get("interrupted") or phase["actual_wait"] != 0
                    or not phase["group_absent"] or phase.get("unjoined_child") or adopted
                    or not all(phase["group_empty_tail"]) or not all(phase["runtime_empty"])
                    or phase.get("exact_4_top_pass") is False):
                break
        else:
            result["whole_pass"], code = True, 0
    except BaseException as error:
        result["wrapper_failure"] = type(error).__name__
    finally:
        result["exit_code"] = code
        result["finished_utc"] = utc()
        (out / "result.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
        emit({"whole_pass": result["whole_pass"], "exit_code": code,
              "result": str(out / "result.json")})
    return code


if __name__ == "__main__":
    raise SystemExit(main())
