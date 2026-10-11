"""Run frozen Tool-result reader, Loop and Execution checks, offline only.

Preparation is inert: root must hand over the single Go/cache window before this
script is invoked. All five dependency packages and migration 00066 must be
frozen, including test and embedded files discovered from the working tree.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time


ROOT = Path(__file__).resolve().parents[2]
GO = "/workspace/toolchains/go1.27.1/bin/go"
PACKAGES = [
    "./internal/central/tool/runtime", "./internal/central/agentloop",
    "./internal/central/execution",
]
VET_PACKAGES = [
    "./internal/central/execution/contract", "./internal/central/execution",
    "./internal/central/tool/contract", "./internal/central/tool/runtime",
    "./internal/central/agentloop",
]
TOPS = [
    "TestReadFileResultLoanIsSameTxCompleteAndCleared",
    "TestReadFileResultLoanRejectsForeignStaleAndNonSuccess",
    "TestReadFileToolResultCompleteProjection",
    "TestReadFileToolResultRejectsUnboundOrDamagedResult",
    "TestReadFileToolResultDefaultsRedactContent",
    "TestExecutionToolResultObservationBindsWholeCandidate",
    "TestExecutionToolResultUnknownCannotBecomeRetry",
    "TestExecutionToolResultStopWaitsForActualReaderReturn",
    "TestExecutionToolResultPersistenceIsExplicitAndComplete",
]
TOOL_REJECTIONS = [
    "foreign-reader", "foreign-tx", "missing-locks", "denied",
    "result-digest", "failed", "callback-error", "cancelled",
]
ENV = dict(
    GOTOOLCHAIN="local", GOENV="off", GOWORK="off", GOPROXY="off",
    GOSUMDB="off", GOTELEMETRY="off", GOFLAGS="-mod=readonly -p=1",
    GOMAXPROCS="1", CGO_ENABLED="1",
    GOCACHE="/workspace/agenteam-skill-package-http/output/ai/skill-package-http/go-build",
    GOMODCACHE="/workspace/agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod",
)
SOURCE_FIELDS = [
    "GoFiles", "CgoFiles", "CFiles", "CXXFiles", "MFiles", "HFiles",
    "FFiles", "SFiles", "SwigFiles", "SwigCXXFiles", "SysoFiles", "EmbedFiles",
]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def head():
    return subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--execution-only", action="store_true", help="Run only the four new Execution tops; still vet all five packages")
    args = parser.parse_args()
    packages = ["./internal/central/execution"] if args.execution_only else PACKAGES
    tops = TOPS[5:] if args.execution_only else TOPS
    tool_rejections = [] if args.execution_only else TOOL_REJECTIONS
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    env.update(ENV)

    def save(name, value):
        with (out / name).open("x") as stream:
            json.dump(value, stream, indent=2)
            stream.write("\n")

    def manifest(paths):
        return {name: digest(ROOT / name) for name in sorted(paths)}

    def run(stage, command, frozen=None):
        before = manifest(frozen) if frozen is not None else None
        if before is not None:
            save(stage + "-inputs-before.json", before)
            if before != frozen:
                raise RuntimeError("frozen inputs changed before " + stage)
        save(stage + "-invocation.json", dict(
            cwd=str(ROOT), command=command, environment_overrides=ENV,
            wrapper_sha256=digest(Path(__file__)), go_binary_sha256=digest(Path(GO)),
            source_head=head(),
        ))
        started = time.monotonic()
        with (out / (stage + ".log")).open("xb") as log:
            child = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
            save(stage + "-process.json", dict(pid=child.pid, outer_pid=os.getpid()))
            print(json.dumps(dict(stage=stage, pid=child.pid)), flush=True)
            code = child.wait()
            log.flush()
            os.fsync(log.fileno())
        after = manifest(frozen) if frozen is not None else None
        result = dict(pid=child.pid, actual_wait_exit=code, elapsed_seconds=time.monotonic() - started, source_head_after=head())
        if after is not None:
            save(stage + "-inputs-after.json", after)
            result.update(input_count=len(frozen), inputs_unchanged=before == after)
        save(stage + "-result.json", result)
        print(json.dumps(dict(stage=stage, **result)), flush=True)
        if code or before != after:
            raise SystemExit(code or 1)

    # Only the actual go-list closure is retained. The temporary pre-discovery
    # source binding detects a source change while Go resolves that closure.
    # Include untracked files and all migration embeds; do not use git's tracked
    # list or exclusions to conceal a dependency from the actual working tree.
    candidates = {"go.mod", "go.sum"}
    for prefix in ["internal", "tests/testsupport", "db"]:
        for path in (ROOT / prefix).rglob("*"):
            if path.is_file():
                relative = str(path.relative_to(ROOT))
                candidates.add(relative)
    prior = manifest(candidates)
    run("deps01", [GO, "list", "-deps", "-test", "-json", *VET_PACKAGES])
    raw = (out / "deps01.log").read_text()
    decoder = json.JSONDecoder()
    offset, rows = 0, []
    while offset < len(raw):
        while offset < len(raw) and raw[offset].isspace():
            offset += 1
        if offset == len(raw):
            break
        row, offset = decoder.raw_decode(raw, offset)
        rows.append(row)
    paths, local_packages = {"go.mod", "go.sum"}, []
    for row in rows:
        directory = Path(row.get("Dir", "/"))
        if not directory.is_relative_to(ROOT):
            continue
        local_packages.append(row["ImportPath"])
        for field in SOURCE_FIELDS:
            for item in row.get(field, []):
                path = directory / item
                if path.is_relative_to(ROOT):
                    paths.add(str(path.relative_to(ROOT)))
    missing = sorted(paths - prior.keys())
    save("deps01-closure.json", dict(
        packages=packages, vet_packages=VET_PACKAGES, local_packages=local_packages, input_count=len(paths),
        missing_pre_discovery_inputs=missing,
        top_tests=tops, tool_rejection_subtests=tool_rejections,
    ))
    if missing:
        raise RuntimeError("test dependency reached unbound source; no test started")
    before = {name: prior[name] for name in sorted(paths)}
    after = manifest(paths)
    save("deps01-inputs-before.json", before)
    save("deps01-inputs-after.json", after)
    save("deps01-stability.json", dict(input_count=len(paths), inputs_unchanged=before == after))
    if before != after:
        raise RuntimeError("dependency source changed during discovery; no test started")
    pattern = "^(" + "|".join(tops) + ")$"
    run("unit01", [GO, "test", "-race", "-p", "1", "-count=1", "-json", "-run", pattern, *packages], after)
    passed = []
    for line in (out / "unit01.log").read_text().splitlines():
        event = json.loads(line)
        if event.get("Action") == "pass" and event.get("Test"):
            passed.append(event["Test"])
    required = tops + [TOPS[1] + "/" + name for name in tool_rejections]
    save("unit01-selection.json", dict(required=required, passed=passed, all_required_passed=all(name in passed for name in required)))
    if any(name not in passed for name in required):
        raise RuntimeError("expected selected test did not pass")
    run("vet01", [GO, "vet", "-p", "1", *VET_PACKAGES], after)


if __name__ == "__main__":
    main()
