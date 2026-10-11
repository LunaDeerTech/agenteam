"""Offline checks for the two local Runner filesystem/workspace packages.

Invoke only after root hands over the Go/cache window. --help is inert.
Uses the existing recorder's Popen/actual Wait and immutable output convention;
there is no PG, service, resource supervisor, retry or network setup.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time


ROOT = Path(__file__).resolve().parents[2]
GO = Path("/workspace/toolchains/go1.27.1/bin/go")
PACKAGES = ["./internal/runner/filesystem", "./internal/runner/workspace"]
IMPORTS = ["github.com/LunaDeerTech/agenteam/" + p[2:] for p in PACKAGES]
WRITE_TESTS = (
    "TestWriteValidationAndSafeProjection",
    "TestWriteRealRoundTripAndReplacement",
    "TestWriteNoReplaceRacesAcrossRoots",
    "TestWriteShortIOPublishesOnlyCompleteFile",
    "TestWriteRejectsUnsafeTargets",
    "TestWriteConcurrentNamespaceReplacement",
    "TestWritePublicationFailureFacts",
    "TestWriteStopWaitsForActualCallAndKeepsPublication",
    "TestHandleWriteReadAndIsolation",
    "TestHandleWriteStopJoinsOriginalRootAndKeepsResult",
)
ENV = dict(
    GOTOOLCHAIN="local", GOENV="off", GOWORK="off", GOPROXY="off",
    GOSUMDB="off", GOTELEMETRY="off", GOFLAGS="-mod=readonly -p=1",
    GOMAXPROCS="1", CGO_ENABLED="1",
    GOCACHE="/workspace/agenteam-skill-package-http/output/ai/skill-package-http/go-build",
    GOMODCACHE="/workspace/agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod",
)
FIELDS = (
    "GoFiles", "CgoFiles", "CFiles", "CXXFiles", "MFiles", "HFiles",
    "FFiles", "SFiles", "SwigFiles", "SwigCXXFiles", "SysoFiles", "EmbedFiles",
    "TestGoFiles", "XTestGoFiles", "TestEmbedFiles", "XTestEmbedFiles",
)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def snapshot(paths):
    return {str(p): sha(p) for p in sorted(paths)}


def packages_from(path):
    text = path.read_text()
    decoder = json.JSONDecoder()
    offset, rows = 0, []
    while offset < len(text):
        while offset < len(text) and text[offset].isspace():
            offset += 1
        if offset == len(text):
            break
        row, offset = decoder.raw_decode(text, offset)
        rows.append(row)
    return rows


def closure(rows):
    paths = {ROOT / "go.mod", ROOT / "go.sum", Path(__file__).resolve(), GO}
    generated, selected = [], set()
    for row in rows:
        # go list's synthesized test main is a generated cache artifact, not
        # source. Its actual package/test source dependencies are separate rows.
        if row.get("Name") == "main" and row["ImportPath"].endswith(".test"):
            generated.append(row["ImportPath"])
            continue
        directory = Path(row["Dir"])
        for field in FIELDS:
            if field.startswith(("Test", "XTest")) and row["ImportPath"] not in IMPORTS:
                continue  # Do not include unexecuted dependency-package tests.
            for name in row.get(field, []):
                path = directory / name
                paths.add(path)
                if field in ("TestGoFiles", "XTestGoFiles") and row["ImportPath"] in IMPORTS:
                    for name in re.findall(r"^func (Test[A-Za-z0-9_]+)\(", path.read_text(), re.M):
                        selected.add((row["ImportPath"], name))
        module = row.get("Module", {})
        if module.get("GoMod"):
            paths.add(Path(module["GoMod"]))
    return paths, generated, sorted(selected)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="Fresh directory under output/ai/runner-write-file-adapter")
    parser.add_argument("--write-only", action="store_true", help="Run only the ten existing Write tests; vet still covers both packages")
    args = parser.parse_args()
    out = args.output.resolve()
    if not out.is_relative_to(ROOT / "output/ai/runner-write-file-adapter"):
        parser.error("output must be under this task's output directory")
    out.mkdir(parents=True, exist_ok=False)
    temporary = out / "tmp"
    temporary.mkdir(mode=0o700)
    overrides = dict(ENV, TMPDIR=str(temporary), GOTMPDIR=str(temporary))
    env = os.environ.copy()
    env.update(overrides)

    def save(name, value):
        with (out / name).open("x") as stream:
            json.dump(value, stream, indent=2)
            stream.write("\n")

    source = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    save("invocation.json", dict(cwd=str(ROOT), source_head=source, packages=PACKAGES,
         environment_overrides=overrides, script_sha256=sha(__file__), go_sha256=sha(GO),
         wrapper_pid=os.getpid(), tests=list(WRITE_TESTS) if args.write_only else "all tests in both packages; no -run selector"))

    def run(stage, command):
        save(stage + "-invocation.json", dict(command=[str(p) for p in command]))
        start = time.monotonic()
        with (out / (stage + ".log")).open("xb") as log:
            child = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
            save(stage + "-process.json", dict(pid=child.pid, wrapper_pid=os.getpid()))
            print(json.dumps(dict(stage=stage, pid=child.pid)), flush=True)
            code = child.wait()
            log.flush()
            os.fsync(log.fileno())
        result = dict(pid=child.pid, actual_wait_exit=code, elapsed_seconds=time.monotonic() - start)
        save(stage + "-result.json", result)
        print(json.dumps(dict(stage=stage, **result)), flush=True)
        return code

    def discover(when):
        rows = []
        # Race can select different runtime source than vet. Bind the union of
        # both actual build modes rather than pretending their closures match.
        for mode, flags in (("race", ["-race"]), ("plain", [])):
            stage = "deps-" + when + "-" + mode
            code = run(stage, [GO, "list", *flags, "-deps", "-test", "-json", *PACKAGES])
            if code:
                return code, None
            rows.extend(packages_from(out / (stage + ".log")))
        return 0, closure(rows)

    code, discovered = discover("before")
    if code:
        return code
    paths, generated, selected = discovered
    if args.write_only:
        selected = [item for item in selected if item[1] in WRITE_TESTS]
        if {name for _, name in selected} != set(WRITE_TESTS) or len(selected) != len(WRITE_TESTS):
            raise RuntimeError("the exact ten Write tests were not discovered")
    before = snapshot(paths)
    save("inputs-before.json", before)
    save("selection.json", dict(expected_top_tests=selected, input_count=len(paths),
         generated_test_mains=generated, scope="actual go-list active sources, embeds and module files; no whole-tree scan"))
    if not selected or any(not any(p == package for p, _ in selected) for package in IMPORTS):
        raise RuntimeError("expected package test sources were not discovered")
    selector = ["-run", "^(" + "|".join(WRITE_TESTS) + ")$"] if args.write_only else []
    code = run("unit", [GO, "test", "-race", "-count=1", "-p=1", "-timeout=120s", "-json", *selector, *PACKAGES])
    events = []
    for line in (out / "unit.log").read_text().splitlines():
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            pass  # Compiler diagnostics remain intact in the original log.
    passed = {(e["Package"], e["Test"]) for e in events if e.get("Action") == "pass" and e.get("Test")}
    missing = [item for item in selected if item not in passed]
    save("unit-selection.json", dict(passed=sorted(passed), missing_top_tests=missing,
         top_pass_count=sum("/" not in name for _, name in passed),
         subtest_pass_count=sum("/" in name for _, name in passed)))
    code = code or bool(missing)
    unit_after = snapshot(paths)
    save("inputs-after-unit.json", unit_after)
    if unit_after != before:
        code = 1
    if not code:
        code = run("vet", [GO, "vet", "-p=1", *PACKAGES])
    # Also rediscover filenames, so a newly added source cannot evade the
    # comparison merely because it was absent from the first package closure.
    listed, discovered_after = discover("after")
    after_paths = discovered_after[0] if not listed else paths
    after = snapshot(after_paths)
    save("inputs-after.json", after)
    unchanged = not listed and before == after
    code = int(code or listed or not unchanged)
    save("result.json", dict(actual_checks_exit=code, inputs_unchanged=unchanged,
         input_count_before=len(before), input_count_after=len(after),
         selected_top_count=len(selected), observed_top_pass_count=sum("/" not in name for _, name in passed)))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
