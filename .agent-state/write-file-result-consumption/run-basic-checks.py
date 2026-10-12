"""Frozen explicit Write receipt consumption controls, only in a root Go window.

The closed scope is twelve new tops and five affected compatibility tops across
three test packages, with five vet packages. Original closure/Wait records stay;
this script never starts PG, Runner, Model, HTTP or native resource fixtures.
The closed reader-followup scope selects only the affected reader controls and
their runtime package vet; the default full selection remains unchanged.
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
NEW_TESTS = {'./internal/central/tool/runtime': ['TestWriteFileResultLoanIsSameTxCompleteAndCleared',
                                     'TestWriteFileResultLoanRejectsForeignStaleAndNonPublication',
                                     'TestWriteFileResultLoanWaitsForOriginalReturnAndClearsFailure'],
 './internal/central/execution': ['TestExecutionCommittedWriteCallHistoricalLoan',
                                  'TestExecutionCommittedWriteCallRejectsUnboundHistory',
                                  'TestExecutionWriteResultHistoricalReplayAndCurrentGate',
                                  'TestExecutionWriteResultUnknownUsesOriginalObservation'],
 './internal/central/agentloop': ['TestWriteFileToolResultPreservesCompleteReceipt',
                                  'TestWriteFileToolResultRejectsUnboundOrDamagedReceipt',
                                  'TestWriteFileToolResultDefaultsRedactReceipt',
                                  'TestWriteFileRoundProjectionPreservesCommittedHistory',
                                  'TestWriteFileRoundProjectionRejectsMixedOrChangedReceipt']}
COMPAT_TESTS = {'./internal/central/execution': ['TestExecutionToolResultPersistenceIsExplicitAndComplete',
                                  'TestExecutionToolResultUnknownCannotBecomeRetry',
                                  'TestExecutionToolResultStopWaitsForActualReaderReturn',
                                  'TestExecutionRoundProjectionLoanEndsWithOriginalCallback'],
 './internal/central/agentloop': ['TestRoundProjectionUsesCommittedOrderAndFixedSources']}
TESTS = {package: names + COMPAT_TESTS.get(package, []) for package, names in NEW_TESTS.items()}
VET_PACKAGES = ['./internal/central/tool/runtime',
 './internal/central/execution',
 './internal/central/agentloop',
 './internal/central/tool/contract',
 './internal/central/execution/contract']
READER_NEW_TESTS = {'./internal/central/tool/runtime': ['TestWriteFileResultDiscoveryDeadlineScopes']}
READER_COMPAT_TESTS = {'./internal/central/tool/runtime': NEW_TESTS['./internal/central/tool/runtime']}
READER_REQUIRED_SOURCES = {
    'db/migrations/00066_execution_tool_result_consumptions.sql',
    'db/migrations/00068_execution_round_inputs.sql',
    'db/migrations/00069_runner_write_file_operations.sql',
    'db/migrations/00070_runner_command_operations.sql',
    'internal/central/tool/contract/committed_write_file_call.go',
    'internal/central/tool/contract/write_file_result.go',
    'internal/central/tool/runtime/write_file_result.go',
    'internal/central/tool/runtime/write_file_result_test.go',
}
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
REQUIRED_SOURCES = {'db/migrations/00066_execution_tool_result_consumptions.sql',
 'db/migrations/00068_execution_round_inputs.sql',
 'db/migrations/00069_runner_write_file_operations.sql',
 'db/migrations/00070_runner_command_operations.sql',
 'internal/central/agentloop/round_projection.go',
 'internal/central/agentloop/write_file_result.go',
 'internal/central/execution/round_input_repository.go',
 'internal/central/execution/round_projection.go',
 'internal/central/execution/write_file_result_authority.go',
 'internal/central/execution/write_file_result_consumption.go',
 'internal/central/tool/contract/committed_write_file_call.go',
 'internal/central/tool/contract/write_file_result.go',
 'internal/central/tool/runtime/write_file_result.go'}

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def head():
    return subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()


def decode_stream(raw):
    decoder = json.JSONDecoder()
    offset, rows = 0, []
    while offset < len(raw):
        while offset < len(raw) and raw[offset].isspace():
            offset += 1
        if offset == len(raw):
            break
        row, offset = decoder.raw_decode(raw, offset)
        rows.append(row)
    return rows


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--scope", choices=("full", "reader-followup"), default="full")
    args = parser.parse_args()
    tests, new_tests, compat_tests = TESTS, NEW_TESTS, COMPAT_TESTS
    vet_packages, required_sources = VET_PACKAGES, REQUIRED_SOURCES
    if args.scope == "reader-followup":
        new_tests, compat_tests = READER_NEW_TESTS, READER_COMPAT_TESTS
        tests = {package: names + compat_tests.get(package, []) for package, names in new_tests.items()}
        vet_packages, required_sources = list(tests), READER_REQUIRED_SOURCES
    packages = list(tests)
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    env.update(ENV)
    harness = str(Path(__file__).resolve().relative_to(ROOT))
    protected = {harness, GO}

    def save(name, value):
        with (out / name).open("x") as stream:
            json.dump(value, stream, indent=2)
            stream.write("\n")

    def manifest(paths):
        return {name: digest(ROOT / name) for name in sorted(paths)}

    fixed_harness = manifest(protected)

    def run(stage, command, frozen=None):
        if manifest(protected) != fixed_harness:
            raise RuntimeError("wrapper or fixed Go binary changed before " + stage)
        before = manifest(frozen) if frozen is not None else None
        if before is not None:
            save(stage + "-inputs-before.json", before)
            if before != frozen:
                raise RuntimeError("frozen inputs changed before " + stage)
        save(stage + "-invocation.json", dict(
            cwd=str(ROOT), command=command, environment_overrides=ENV,
            scope=args.scope,
            wrapper_sha256=fixed_harness[harness], go_binary_sha256=fixed_harness[GO],
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
        result = dict(
            pid=child.pid, actual_wait_exit=code, elapsed_seconds=time.monotonic() - started,
            source_head_after=head(), harness_unchanged=manifest(protected) == fixed_harness,
        )
        if after is not None:
            save(stage + "-inputs-after.json", after)
            result.update(input_count=len(frozen), inputs_unchanged=before == after)
        save(stage + "-result.json", result)
        print(json.dumps(dict(stage=stage, **result)), flush=True)
        if code or before != after or not result["harness_unchanged"]:
            raise SystemExit(code or 1)

    # Bind candidates before either discovery, including untracked files and
    # migration embeds. Retain only the actual union of race-test/vet inputs.
    candidates = {"go.mod", "go.sum"} | protected
    for prefix in ["internal", "tests/testsupport", "db"]:
        for path in (ROOT / prefix).rglob("*"):
            if path.is_file():
                candidates.add(str(path.relative_to(ROOT)))
    prior = manifest(candidates)
    closures = {}
    paths = {"go.mod", "go.sum"} | protected
    for stage, flags, targets in [
        ("deps-race01", ["-race"], packages),
        ("deps-vet01", [], vet_packages),
    ]:
        run(stage, [GO, "list", *flags, "-deps", "-test", "-json", *targets])
        discovered, local_packages = set(), []
        for row in decode_stream((out / (stage + ".log")).read_text()):
            directory = Path(row.get("Dir", "/"))
            if not directory.is_relative_to(ROOT):
                continue
            local_packages.append(row["ImportPath"])
            # Synthetic [package.test] GoFiles include actual compiled tests;
            # unrelated dependency TestGoFiles must not inflate this closure.
            for field in SOURCE_FIELDS:
                for item in row.get(field, []):
                    path = directory / item
                    if path.is_relative_to(ROOT):
                        discovered.add(str(path.relative_to(ROOT)))
        paths.update(discovered)
        closures[stage] = dict(packages=targets, local_packages=local_packages, paths=sorted(discovered))
    missing = sorted(paths - prior.keys())
    save("deps-closure.json", dict(
        scope=args.scope,
        closures=closures, input_count=len(paths), missing_pre_discovery_inputs=missing,
        new_top_tests=new_tests, related_old_top_tests=compat_tests, vet_packages=vet_packages,
    ))
    if missing or not required_sources.issubset(paths):
        raise RuntimeError("actual closure missing or reaching unbound source; no test started")
    before = {name: prior[name] for name in sorted(paths)}
    after = manifest(paths)
    save("deps-inputs-before.json", before)
    save("deps-inputs-after.json", after)
    save("deps-stability.json", dict(input_count=len(paths), inputs_unchanged=before == after))
    if before != after:
        raise RuntimeError("source changed during dependency discovery; no test started")

    tops = [name for names in tests.values() for name in names]
    pattern = "^(" + "|".join(tops) + ")$"
    run("unit01", [GO, "test", "-race", "-p", "1", "-count=1", "-timeout=10m", "-json", "-run", pattern, *packages], after)
    passed, started = [], []
    for line in (out / "unit01.log").read_text().splitlines():
        event = json.loads(line)
        if event.get("Test") and event.get("Action") in ("pass", "run"):
            target = passed if event["Action"] == "pass" else started
            target.append([event["Package"], event["Test"]])
    module = "github.com/LunaDeerTech/agenteam/"
    required = [[module + package[2:], name] for package, names in tests.items() for name in names]
    actual_tops = [item for item in started if "/" not in item[1]]
    selected = sorted(actual_tops) == sorted(required) and all(passed.count(item) == 1 for item in required)
    save("unit01-selection.json", dict(
        scope=args.scope,
        required=required, actual_top_runs=actual_tops, passed=passed,
        new_top_count=sum(map(len, new_tests.values())), related_old_top_count=sum(map(len, compat_tests.values())), exact_selection_passed=selected,
    ))
    if not selected:
        raise RuntimeError("selected top was missing, extra, duplicated or did not pass")
    run("vet01", [GO, "vet", "-p", "1", *vet_packages], after)


if __name__ == "__main__":
    main()
