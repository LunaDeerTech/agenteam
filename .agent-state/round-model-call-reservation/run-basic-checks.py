"""Bounded Round Model Call reservation controls and real embedded migration loading.

The accepted closure, input, exact-selection and actual-Wait algorithm is
unchanged. This method starts no PostgreSQL or other fixture resources.
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
NEW_TESTS = {'./internal/central/execution': ['TestExecutionRoundModelCallConstructorKeepsProcessProofPrivate', 'TestExecutionRoundModelCallBindsCompleteOriginalRequest', 'TestExecutionRoundModelCallRequiresLiveTransactionBeforeHistory', 'TestExecutionRoundModelCallUnknownKeepsOriginalCandidateAndReturn', 'TestExecutionRoundModelCallKnownReplayCannotCreateNewUnknownWrite', 'TestExecutionRoundModelCallLoanAndStoreMustActuallyReturn'], './internal/central/postgres': []}
COMPAT_TESTS = {'./internal/central/postgres': ['TestEmbeddedSourceLoadsAllMigrations']}
TESTS = {package: names + COMPAT_TESTS.get(package, []) for package, names in NEW_TESTS.items()}
VET_PACKAGES = ['./internal/central/execution', './internal/central/postgres']
EXPECTED_NEW_SUBS = 28
EXPECTED_COMPAT_SUBS = 0
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
REQUIRED_SOURCES = {'internal/central/execution/round_model_call_owner.go', 'internal/central/postgres/embedded_source_test.go', 'internal/central/postgres/source.go', 'internal/central/execution/round_model_call_repository.go', 'db/migrations/00073_execution_round_model_claims.sql', 'internal/central/execution/contract/round_model_call.go', 'db/migrations/00072_execution_round_model_requests.sql', 'internal/central/execution/round_model_call.go', 'internal/central/execution/round_model_call_test.go', 'db/migrations/embed.go', 'internal/central/object/process_binding.go'}
RUNTIME_INPUTS = set()


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
    parser.add_argument("--scope", choices=("full",), default="full")
    args = parser.parse_args()
    tests, new_tests, compat_tests = TESTS, NEW_TESTS, COMPAT_TESTS
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
    paths = {"go.mod", "go.sum"} | protected | RUNTIME_INPUTS
    for stage, flags, targets in [
        ("deps-race01", ["-race"], packages),
        ("deps-vet01", [], VET_PACKAGES),
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
        closures=closures, runtime_inputs=sorted(RUNTIME_INPUTS), input_count=len(paths), missing_pre_discovery_inputs=missing,
        new_top_tests=new_tests, related_old_top_tests=compat_tests, vet_packages=VET_PACKAGES,
    ))
    if missing or not REQUIRED_SOURCES.issubset(paths):
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
    actual_subs = [item for item in started if "/" in item[1]]
    new_top_ids = {(module + package[2:], name) for package, names in new_tests.items() for name in names}
    old_top_ids = {(module + package[2:], name) for package, names in compat_tests.items() for name in names}
    new_sub_count = sum((package, name.split("/", 1)[0]) in new_top_ids for package, name in actual_subs)
    old_sub_count = sum((package, name.split("/", 1)[0]) in old_top_ids for package, name in actual_subs)
    sub_selection = new_sub_count == EXPECTED_NEW_SUBS and old_sub_count == EXPECTED_COMPAT_SUBS and all(passed.count(item) == 1 for item in actual_subs)
    selected = sub_selection and sorted(actual_tops) == sorted(required) and all(passed.count(item) == 1 for item in required)
    save("unit01-selection.json", dict(
        scope=args.scope,
        required=required, actual_top_runs=actual_tops, actual_sub_runs=actual_subs, passed=passed,
        declared_new_sub_count=EXPECTED_NEW_SUBS, declared_related_old_sub_count=EXPECTED_COMPAT_SUBS,
        actual_new_sub_count=new_sub_count, actual_related_old_sub_count=old_sub_count, exact_sub_selection_passed=sub_selection,
        new_top_count=sum(map(len, new_tests.values())), related_old_top_count=sum(map(len, compat_tests.values())), exact_selection_passed=selected,
    ))
    if not selected:
        raise RuntimeError("selected top was missing, extra, duplicated or did not pass")
    run("vet01", [GO, "vet", "-p", "1", *VET_PACKAGES], after)


if __name__ == "__main__":
    main()
