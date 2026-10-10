#!/usr/bin/env python3
"""Offline app race compilation and two exact listings; never runs test bodies."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
AREA = ROOT / "output/ai/knowledge-owner-rename"
GO = Path("/workspace/toolchains/go1.27.1/bin/go")
MODULES = Path("/workspace/shared/agenteam-deps/go-mod")
TOPS = ["TestKnowledgeOwnerRenameWeb", "TestSkillOwnerReadWeb"]
SELECTOR = "^(" + "|".join(TOPS) + ")$"


def groups_present(groups):
    found = []
    for entry in Path("/proc").iterdir():
        if not entry.name.isdigit():
            continue
        try:
            # comm may contain spaces or parentheses; fields begin after it.
            fields = (entry / "stat").read_text().rsplit(")", 1)[1].split()
            if int(fields[2]) in groups:
                found.append(int(entry.name))
        except (FileNotFoundError, ProcessLookupError):
            pass
    return sorted(found)


def child(command, out, label, env, budget):
    started = time.monotonic()
    log = out / (label + ".log")
    with log.open("xb") as stream:
        proc = subprocess.Popen(command, cwd=ROOT, env=env, stdout=stream,
                                stderr=subprocess.STDOUT, start_new_session=True)
        print(json.dumps({"stage": label, "pid": proc.pid,
                          "command": command}), flush=True)
        timed_out = False
        try:
            code = proc.wait(timeout=budget)
        except subprocess.TimeoutExpired:
            timed_out = True
            os.killpg(proc.pid, signal.SIGKILL)
            code = proc.wait()
    result = {"pid": proc.pid, "actual_wait": code, "timed_out": timed_out,
              "seconds": round(time.monotonic() - started, 3)}
    print(json.dumps({"stage": label, **result}), flush=True)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", default=str(AREA / "combined-compile-01"))
    args = parser.parse_args()
    out = Path(args.output)
    if (not out.is_absolute() or out.parent != AREA or out.exists()
            or out.is_symlink() or not GO.is_file() or not MODULES.is_dir()):
        print("STOP exact fresh private output/toolchain/modules required")
        return 1
    # This happens in the same process before the first Go action.
    disk = os.statvfs(ROOT)
    free = disk.f_bavail * disk.f_frsize
    if free < 5 * 1024 ** 3:
        print(json.dumps({"stage": "preflight", "free_bytes": free,
                          "go_started": False, "result": "insufficient_disk"}))
        return 1
    out.mkdir(mode=0o700, parents=True)
    runtime = out / "runtime"
    runtime.mkdir(mode=0o700)
    telemetry = out / "go-config/go/telemetry"
    telemetry.mkdir(mode=0o700, parents=True)
    (telemetry / "mode").write_text("off\n")
    (telemetry / "mode").chmod(0o600)
    cache = AREA / "go-build"
    cache.mkdir(mode=0o700, exist_ok=True)
    env = os.environ.copy()
    for name in ("TEST_TELEMETRY_DIR", "GO_TELEMETRY_CHILD", "GO_TELEMETRY_CHILD_UPLOAD"):
        env.pop(name, None)
    env.update(GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off", GOTELEMETRY="off",
               GOFLAGS="-mod=readonly -p=2", GOMODCACHE=str(MODULES),
               GOCACHE=str(cache), GOTMPDIR=str(runtime), TMPDIR=str(runtime),
               XDG_CONFIG_HOME=str(out / "go-config"))
    candidate = out / "owner-ui-combined-race.test"
    inputs = sorted(str(p.relative_to(ROOT)) for p in
                    (ROOT / "internal/central/app").glob("*.go"))
    source = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, check=True,
                            capture_output=True, text=True).stdout.strip()
    result = {"source": source, "free_bytes": free, "app_inputs": inputs,
              "selector": SELECTOR, "candidate": str(candidate),
              "telemetry_mode": "off", "stages": {}, "tests_executed": False}
    print(json.dumps({"source": source, "free_bytes": free,
                      "app_input_count": len(inputs), "output": str(out)}), flush=True)
    build = child([str(GO), "test", "-tags=integration", "-race", "-p=2",
                   "-mod=readonly", "-c", "-o", str(candidate),
                   "./internal/central/app"], out, "compile", env, 600)
    result["stages"]["compile"] = build
    passed = build["actual_wait"] == 0 and not build["timed_out"]
    if passed:
        listing = child([str(candidate), "-test.list=" + SELECTOR],
                        out, "list", env, 20)
        result["stages"]["list"] = listing
        names = (out / "list.log").read_text().splitlines()
        result["listed"] = names
        passed = (listing["actual_wait"] == 0 and not listing["timed_out"]
                  and sorted(names) == TOPS)
    result["remaining_processes"] = groups_present(
        {stage["pid"] for stage in result["stages"].values()})
    result["runtime_empty"] = not any(runtime.iterdir())
    if result["runtime_empty"]:
        runtime.rmdir()
    result["runtime_absent"] = not runtime.exists()
    passed = passed and not result["remaining_processes"] and result["runtime_absent"]
    if passed:
        result["candidate_bytes"] = candidate.stat().st_size
        with candidate.open("rb") as stream:
            result["candidate_sha256"] = hashlib.file_digest(stream, "sha256").hexdigest()
    result["passed"] = passed
    (out / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({k: v for k, v in result.items() if k != "app_inputs"}), flush=True)
    if not passed:
        for label in result["stages"]:
            print((out / (label + ".log")).read_text()[-12000:], flush=True)
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
