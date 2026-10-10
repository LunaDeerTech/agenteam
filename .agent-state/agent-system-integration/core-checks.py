#!/usr/bin/env python3
"""Combined 21-top race/vet; reuses tool-operation 6eb3a62a Wait/tail method."""
import argparse
import ctypes
import json
import os
from pathlib import Path
import signal
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
GO = "/workspace/toolchains/go1.27.1/bin/go"
TOPS = {
    "agent": [
        "TestAgentExecutionConfigurationStagesAndLocks",
        "TestAgentExecutionConfigurationOriginalAuthorityAndTransaction",
        "TestAgentExecutionConfigurationCancellationWaitsForOriginalCallback",
    ],
    "execution": [
        "TestExecutionLaunchOriginalIdentityReplayAndSlot",
        "TestExecutionLaunchRejectsMissingAndStaleOwners",
        "TestExecutionUnknownLookupDoesNotRepeatLaunch",
        "TestExecutionLaunchWitnessCannotBeConstructedOrReused",
    ],
    "tool/registry": [
        "TestRegistryCurrentBuiltinRequiresExactCurrentTuple",
        "TestRegistryCurrentBuiltinRejectsMissingTransactionLocksOrSource",
        "TestRegistryCurrentBuiltinRejectsCodeAndDefinitionDrift",
    ],
    "tool/authorization": [
        "TestInstallAuthorizationCurrentFactsAndBoundPlan",
        "TestInstallAuthorizationDependenciesCancellationAndSafeProjection",
    ],
    "tool/runtime": [
        "TestOperationInstallCanonicalInput",
        "TestOperationPrepareAndLookupBindOriginalInput",
        "TestOperationUnknownRetainsCauseAndRequiresLookup",
        "TestOperationRequiresCurrentExecutionAndSafeMetadata",
        "TestInstallRuntimePrivateHandoffAndOriginalJoin",
        "TestInstallRuntimeTerminalReceiptAndUnknownProvenance",
    ],
    "skill": [
        "TestAgentInstallCurrentTransactionAndRecovery",
        "TestAgentInstallRejectsMissingOrChangedHandoff",
        "TestInstallationExecutionOriginKeepsHumanCompatibility",
    ],
}
REPAIR_TOPS = {"skill": [
    "TestAgentInstallCurrentTransactionAndRecovery",
    "TestInstallationExecutionOriginKeepsHumanCompatibility",
]}
SELECTOR = "^(" + "|".join(top for tops in TOPS.values() for top in tops) + ")$"
PACKAGES = ["./internal/central/" + package for package in TOPS]
COMMANDS = [
    ("race", [GO, "test", "-mod=readonly", "-p=2", "-race", "-count=1", "-timeout=90s", "-json", "-run", SELECTOR, *PACKAGES]),
    ("vet", [GO, "vet", "-mod=readonly", "-p=2", *PACKAGES,
             "./internal/central/agent/contract", "./internal/central/execution/contract",
             "./internal/central/skill/contract", "./internal/central/tool/contract"]),
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


def race_summary(path, tops=TOPS):
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
        for package, names in tops.items() for top in names
    }
    return {"tops": observed, f"exact_{sum(map(len, tops.values()))}_top_pass": observed == expected}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--cache", type=Path, required=True)
    parser.add_argument("--profile", choices=("initial", "repair"), default="initial")
    args = parser.parse_args()
    cache = args.cache
    if not cache.is_absolute() or not cache.is_dir() or cache.resolve() != cache:
        parser.error("an existing coordinator-assigned absolute cache is required")
    tops, selector, commands, run = TOPS, SELECTOR, COMMANDS, "combined-core-01"
    if args.profile == "repair":
        tops = REPAIR_TOPS
        selector = "^(" + "|".join(tops["skill"]) + ")$"
        race = [selector if arg == SELECTOR else arg
                for arg in COMMANDS[0][1] if arg not in PACKAGES]
        commands = [("race", race + ["./internal/central/skill"]), COMMANDS[1]]
        run = "combined-core-02"
    pass_key = f"exact_{sum(map(len, tops.values()))}_top_pass"
    out = ROOT / "output/ai/agent-system-integration" / run
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
    env = {
        "PATH": str(Path(GO).parent) + ":/usr/bin:/bin", "LANG": "C.UTF-8",
        "LC_ALL": "C.UTF-8", "TZ": "UTC", "GOTOOLCHAIN": "local",
        "GOPROXY": "off", "GOSUMDB": "off",
        "GOMODCACHE": "/workspace/shared/agenteam-deps/go-mod",
        "GOCACHE": str(cache), "GOTMPDIR": str(runtime), "TMPDIR": str(runtime),
        "XDG_CONFIG_HOME": str(config), "GOMAXPROCS": "2", "CGO_ENABLED": "1",
    }
    # Whitelist above excludes telemetry/config bypasses before the first Go.
    result = {"outer_pid": os.getpid(), "started_utc": utc(), "selector": selector,
              "profile": args.profile,
              "cache": str(cache), "phases": [], "whole_pass": False}
    code = 1
    try:
        if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0:
            raise OSError(ctypes.get_errno(), "subreaper unavailable")
        for name, command in commands:
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
                phase.update(race_summary(log_path, tops))
            emit(phase)
            if (phase.get("timeout") or phase.get("interrupted") or phase["actual_wait"] != 0
                    or not phase["group_absent"] or phase.get("unjoined_child") or adopted
                    or not all(phase["group_empty_tail"]) or not all(phase["runtime_empty"])
                    or phase.get(pass_key) is False):
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
