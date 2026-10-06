#!/usr/bin/env python3
"""Verify archived bytes and fixed local Git blobs; execute no recorded commands."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[3]
REUSED = ROOT.parent / "dialog-outside-focus-repair-verification-evidence"
INDEX = json.loads((ROOT / "archive-index.json").read_text())
PREFIX = INDEX["source_prefixes"]


def sha(data):
    return hashlib.sha256(data).hexdigest()


originals = {}
physical = {}
for entry in INDEX["entries"]:
    path = (ROOT / entry["archive"]).resolve()
    assert path.is_relative_to(ROOT) or path.is_relative_to(REUSED)
    data = path.read_bytes()
    assert sha(data) == entry["sha256"] and len(data) == entry["bytes"], entry
    assert entry["source"] not in originals
    originals[entry["source"]] = data
    physical[entry["archive"]] = data
assert len(originals) == INDEX["logical_originals"]
assert len(physical) == INDEX["physical_originals"]
assert sum(map(len, originals.values())) == INDEX["logical_bytes"]
local = {name: data for name, data in physical.items() if not name.startswith("../")}
assert len(local) == INDEX["local_physical_originals"]
assert len(physical) - len(local) == INDEX["reused_physical_originals"]
assert sum(map(len, local.values())) == INDEX["local_physical_bytes"]


def raw(owner, name):
    return originals[PREFIX[owner] + name]


def record(owner, name):
    return json.loads(raw(owner, name))


git_blobs = {}


def git(commit, name):
    key = commit + ":" + name
    if key not in git_blobs:
        git_blobs[key] = subprocess.check_output(
            ["git", "cat-file", "blob", key], cwd=REPO,
            env={**os.environ, "GIT_NO_LAZY_FETCH": "1"},
        )
    return git_blobs[key]


inputs = {}
for revision in ("input01", "input02", "input03", "input04"):
    name = revision + "/" + revision + ".json"
    candidate = record("author", name)
    inputs[revision] = candidate
    assert len(candidate["sources"]) == 19 and len(candidate["dist"]) == 22
    for path, digest in candidate["sources"].items():
        assert sha(raw("author", revision + "/files/" + path)) == digest
    dist_lines = raw("author", revision + "/dist-sha256.txt").decode().splitlines()
    assert {line.split(maxsplit=1)[1]: line.split()[0] for line in dist_lines} == candidate["dist"]
    checks = record("author", revision + "/author-checks.json")
    assert checks["input_sha256"] == sha(raw("author", name))
    for check in checks["checks"]:
        assert sha(originals[check["log"]]) == check["log_sha256"]
    for path, digest in candidate["locked_dependencies"].items():
        assert sha(git(candidate["product_base"], path)) == digest
    for path, digest in candidate["backend_dependency"]["sources"].items():
        assert sha(git(INDEX["backend_base"], path)) == digest
    for path, digest in candidate["unchanged_inputs"].items():
        assert sha(git(candidate["product_base"], path)) == digest
assert inputs["input01"]["dist"] == inputs["input02"]["dist"]
for previous, current, count in (("input01", "input02", 2), ("input02", "input03", 3), ("input03", "input04", 0)):
    before, after = inputs[previous]["sources"], inputs[current]["sources"]
    assert set(before) == set(after)
    assert sum(before[path] != after[path] for path in before) == count
final = inputs["input04"]
for path, digest in final["foundation_dependency"]["sources"].items():
    assert sha(git(INDEX["integration_base"], path)) == digest
delivery = record("author", "readme-final01/delivery20.json")
assert sha(raw("author", "readme-final01/delivery20.json")) == "503c60ed42fc4f521bf83c16379d429c155a8e77a4a8d0e28d9532e388053ce6"
assert len(delivery["paths"]) == 20 and delivery["dist"] == final["dist"]
for path, digest in delivery["paths"].items():
    assert sha(git(INDEX["accepted_commit"], path)) == digest
    if path in final["sources"]:
        assert final["sources"][path] == digest
assert sha(raw("author", "readme-final01/README.final.md")) == delivery["paths"]["docs/development/frontend/README.md"]
assert sha(raw("author", "readme-final01/checks.json")) == delivery["documentation_checks"]["sha256"]

combined = {}
for revision in ("input01", "input02", "input04"):
    frozen = record("verification", "frozen-" + revision + ".json")
    combined[revision] = frozen
    assert frozen["author_input_sha256"] == sha(raw("author", revision + "/" + revision + ".json"))
    assert frozen["dist"] == inputs[revision]["dist"]
    for path, digest in frozen["files"].items():
        data = raw("author", revision + "/files/" + path) if path in inputs[revision]["sources"] else git(frozen["product_base"], path)
        assert sha(data) == digest, (revision, path)
    migrations = [p for p in frozen["files"] if p.startswith("db/migrations/")]
    assert len(migrations) == 18 and not any("00019" in p for p in migrations)
assert len(combined["input04"]["files"]) == 82
assert b"244 passed" in raw("author", "revision04-check-01.log")

report = record("verification", "verification-report.json")
assert sha(raw("verification", "verification-report.json")) == "3dc8c6608c5db99c909c325fd74424a4fb180747e83bef29fa2b85d3889481ec"
assert sha(raw("verification", "verification-report.md")) == "1cceaf0cfb56dd49b5db15216852c1913985ea96f6369faa7ac98d297f7b297d"
baseline = None
for run, facts in report["runs"].items():
    prefix = "runs/" + run + "/"
    command = record("verification", prefix + "command.json")
    result = record("verification", prefix + "result.json")
    output = raw("verification", prefix + "raw.log")
    assert sha(output) == facts["raw_sha256"]
    assert command["exit"] == result["driver_exit"] == facts["exit"]
    assert command["seconds"] == facts["seconds"] and command["actual_wait_completed"]
    top = [list(match) for match in re.findall(r"^--- (PASS|FAIL): (\w+) ", output.decode(), re.M)]
    assert top == facts["top_levels"] == result["top_levels"]
    assert command["argv"][:3] == ["sh", "scripts/test-objects.sh", "-run"]
    assert all(re.fullmatch(command["argv"][3], name) for _, name in top)
    assert result["selected_tests_passed"] == (facts["exit"] == 0)
    frozen = record("verification", prefix + "frozen-input.json")
    for phase in ("before", "after"):
        snapshot = record("verification", prefix + "input-" + phase + ".json")
        assert snapshot["files"] == frozen["files"] and snapshot["dist"] == frozen["dist"]
    assert result["source_files_unchanged"] and result["verification_inputs_unchanged"]
    verifier_input = record("verification", prefix + "verification-input.json")
    assert sha(raw("verification", prefix + "driver.py.txt")) == verifier_input["driver_sha256"]
    if run.startswith("independent"):
        assert sha(raw("verification", prefix + "overlay.json")) == verifier_input["overlay_sha256"]
        assert sha(raw("verification", prefix + "directory_independent_test.go.txt")) == verifier_input["probe_sha256"]
        for name, digest in verifier_input["browser_probes"].items():
            assert sha(raw("verification", prefix + name + ".txt")) == digest
    this_baseline = record("verification", prefix + "baseline.json")
    baseline = this_baseline if baseline is None else baseline
    assert baseline == this_baseline
    assert sum(row["kind"] == "container" for row in baseline.values()) == 2
    assert sum(row["kind"] == "network" for row in baseline.values()) == 4
    resources = record("verification", prefix + "observed-resources.json")
    assert len(resources) == result["observed_resources"] == facts["resources"] == 7
    assert not set(resources).intersection(baseline)
    assert len(record("verification", prefix + "observed-processes.json")) == result["observed_processes"] == facts["observed_processes"]
    waits = record("verification", prefix + "adopted-waits.json")
    assert len(waits) == result["adopted_waits"] == facts["adopted_waits"]
    assert all(wait["actual_wait"] for wait in waits)
    assert not record("verification", prefix + "monitor-errors.json") and result["monitor_errors"] == 0
    cleanup = record("verification", prefix + "cleanup.json")
    assert len(cleanup) == 2 and result["double_cleanup"] and result["browser_runtime_removed"]
    for sweep in cleanup:
        assert sweep["baseline_unchanged"]
        assert set(sweep["exact_absent"]) == set(resources)
        assert all(row["absent"] for row in sweep["exact_absent"].values())
        assert all(not sweep[key] for key in ("owned_processes", "remaining_new", "runtime_entries", "browser_runtime_entries"))

pure = record("verification", "runs/pure01/command.json")
assert pure["exit"] == 0 and pure["source_unchanged"] and pure["dist_unchanged"] and pure["probes_unchanged"]
assert sha(raw("verification", "runs/pure01/raw.log")) == pure["raw_sha256"]
assert pure["frozen_input_sha256"] == sha(raw("verification", "frozen-input01.json"))
for path, digest in pure["probes"].items():
    assert sha(originals[path]) == digest
for path in ("web/src/api/client.ts", "web/src/api/account.ts", "web/src/api/system-account.ts", "web/src/composables/useSession.ts", "web/src/composables/useSystemUserDirectory.ts"):
    assert inputs["input01"]["sources"][path] == final["sources"][path]
for run in ("probe-types01", "probe-types02", "probe-types03", "probe-compile01"):
    command = record("verification", "runs/" + run + "/command.json")
    assert command["exit"] == 0 and command["actual_wait_completed"]
    assert sha(raw("verification", "runs/" + run + "/raw.log")) == command["raw_sha256"]
assert sha(raw("verification", "runs/independent03/directory-independent.spec.ts.txt")) == report["final_probe_sha256"]
assert sum(entry["source"].endswith(".png") for entry in INDEX["entries"]) == 12

print(json.dumps({
    "result": "PASS: archived bytes and fixed local Git only",
    "logical_originals": len(originals), "local_physical_originals": len(local),
    "reused_physical_originals": len(physical) - len(local), "local_git_blobs": len(git_blobs),
    "author_inputs": 4, "tested_sources": 19, "accepted_paths": 20,
    "final_combination_files": 82, "dist": "22 hashes only; not rebuilt or read from live tree",
    "final_real_groups": "new3 + old3 + independent1 PASS; four earlier failed runs retained",
    "independent_pure": "2 PASS on input01, unchanged client/controller reused through input04",
    "resources": "all seven UI rounds: actual wait and seven exact IDs absent twice",
}, ensure_ascii=False, indent=2))
