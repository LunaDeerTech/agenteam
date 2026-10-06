#!/usr/bin/env python3
"""Read-only archive/Git checks; never executes stored tests, drivers or resources."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parent
REPOSITORY = ROOT.parents[3]


def digest(data):
    return hashlib.sha256(data).hexdigest()


def archived(path):
    resolved = (ROOT / path).resolve()
    assert resolved.is_relative_to(ROOT), path
    return resolved.read_bytes()


def document(path):
    return json.loads(archived(path))


index = document("archive-index.json")
assert index["format"] == 1
source_entries = {}
for entry in index["originals"]:
    data = archived(entry["archive"])
    assert len(data) == entry["bytes"], entry["archive"]
    assert digest(data) == entry["sha256"], entry["archive"]
    assert entry["source"] not in source_entries, entry["source"]
    source_entries[entry["source"]] = entry


def original(suffix):
    matches = [e for source, e in source_entries.items() if source.endswith("/" + suffix)]
    assert matches, suffix
    assert len({e["sha256"] for e in matches}) == 1, suffix
    return archived(matches[0]["archive"])


def original_json(suffix):
    return json.loads(original(suffix))


selection = original_json("archive-selection.json")
assert selection["frozen"] and len(selection["files"]) == selection["file_count"]
for entry in selection["files"]:
    stored = source_entries[entry["source"]]
    assert (stored["sha256"], stored["bytes"]) == (entry["sha256"], entry["bytes"])
assert sum(e["bytes"] for e in selection["files"]) == selection["total_bytes"]

inputs = {}
for revision in ("input01", "input02"):
    manifest = document("inputs/" + revision + ".json")
    assert manifest["revision"] == revision
    assert manifest["product_base"] == index["product_base"]
    assert set(index["input_sources"][revision]) == set(manifest["sources"])
    for path, expected in manifest["sources"].items():
        assert digest(archived(index["input_sources"][revision][path])) == expected, path
    inputs[revision] = manifest
assert len(inputs["input01"]["sources"]) == 5
assert len(inputs["input02"]["sources"]) == 6

git_cache = {}


def git_blob(commit, path):
    key = commit + ":" + path
    if key not in git_cache:
        git_cache[key] = subprocess.check_output(
            ["git", "cat-file", "blob", key],
            cwd=REPOSITORY,
            env={**os.environ, "GIT_NO_LAZY_FETCH": "1"},
        )
    return git_cache[key]


for path, expected in inputs["input02"]["sources"].items():
    assert digest(git_blob(index["accepted_commit"], path)) == expected, path
boundaries = {
    **inputs["input02"]["locked_dependencies"],
    **inputs["input02"]["unchanged_boundaries"],
}
for path, expected in boundaries.items():
    assert digest(git_blob(index["product_base"], path)) == expected, path
    assert digest(git_blob(index["accepted_commit"], path)) == expected, path

author_checks = (
    "unit01", "race01", "vet01", "integration-compile01", "build-central01", "build-runner01",
    "unit02", "race02", "vet02", "integration-compile02", "build-central02",
)
for label in author_checks:
    metadata = document("author/logs/" + label + ".json")
    assert metadata["exit_code"] == 0 and metadata["inputs_unchanged"], label
    assert digest(archived("author/logs/" + label + ".log")) == metadata["log_sha256"], label
    revision = "input" + label[-2:]
    expected = {**inputs[revision]["sources"], **inputs[revision]["locked_dependencies"]}
    assert metadata["inputs"] == expected, label
    for key, value in {"GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off"}.items():
        assert metadata["env"][key] == value, label

report = original_json("verification-report.json")
assert report["final_head"] == index["accepted_commit"]
assert report["frozen_sources"] == inputs["input02"]["sources"]
assert report["frozen_input02_sha256"] == digest(archived("inputs/input02.json"))
assert report["independent_probe_sha256"] == digest(original("directory_independent_test.go"))
assert report["reviewer_driver_sha256"] == digest(original("run_fixture.py"))
initial = original_json("docker-baseline.json")
assert len(initial["containers"]) == 2 and len(initial["networks"]) == 4
initial_resources = {}
for key, kind in (("containers", "container"), ("networks", "network")):
    for resource in initial[key]:
        initial_resources[resource["id"]] = {**resource, "kind": kind}

tops = {}
for name, revision, expected_exit in (
    ("author01", "input01", 1),
    ("author02", "input02", 0),
    ("independent01", "input02", 0),
):
    prefix = "runs/" + name + "/"
    command = original_json(prefix + "command.json")
    result = original_json(prefix + "result.json")
    raw = original(prefix + "raw.log")
    assert command["actual_wait_completed"] and command["exit"] == expected_exit, name
    assert result["driver_exit"] == expected_exit and result["source_files_unchanged"], name
    assert digest(raw) == report["runs"][name]["raw_sha256"], name
    matches = re.findall(r"^--- (PASS|FAIL|SKIP): (\S+) \(([0-9.]+)s\)$", raw.decode(), re.M)
    assert [[state, test] for state, test, _ in matches] == result["top_levels"], name
    assert not any(state == "SKIP" for state, _, _ in matches), name
    assert bool(expected_exit) == any(state == "FAIL" for state, _, _ in matches), name
    before = original_json(prefix + "input-before.json")
    after = original_json(prefix + "input-after.json")
    expected = {**inputs[revision]["sources"], **inputs[revision]["locked_dependencies"]}
    assert before["files"] == after["files"] == expected, name
    observed = original_json(prefix + "observed-resources.json")
    assert len(observed) == result["observed_resources"] == 7, name
    assert original_json(prefix + "baseline.json") == initial_resources, name
    assert len(original_json(prefix + "observed-processes.json")) == result["observed_processes"], name
    assert original_json(prefix + "monitor-errors.json") == [], name
    cleanup = original_json(prefix + "cleanup.json")
    assert len(cleanup) == 2, name
    for check in cleanup:
        assert set(check["exact_absent"]) == set(observed), name
        assert all(value["absent"] for value in check["exact_absent"].values()), name
        assert check["baseline_unchanged"], name
        assert check["owned_processes"] == check["remaining_new"] == check["runtime_entries"] == [], name
    assert [check["time"] for check in cleanup] == report["runs"][name]["cleanup_utc"], name
    tops[name] = [[state, test] for state, test, _ in matches]

verification_input = original_json("runs/independent01/verification-input.json")
assert verification_input["probe_sha256"] == report["independent_probe_sha256"]
assert verification_input["driver_sha256"] == report["reviewer_driver_sha256"]
assert verification_input["overlay_sha256"] == digest(original("overlay.json"))
minio = original_json("minio-binary.json")
assert minio["exact_sha_match"]
assert minio["sha256"] == report["environment"]["minio_binary_sha256"]

print(json.dumps({
    "status": "PASS: archive bytes, historical inputs, accepted Git and saved results only",
    "logical_originals": len(index["originals"]),
    "physical_originals": len({e["archive"] for e in index["originals"]}),
    "input_sources": [len(inputs[rev]["sources"]) for rev in ("input01", "input02")],
    "accepted_sources": 6,
    "unchanged_boundaries_and_locks": len(boundaries),
    "author_checks": len(author_checks),
    "raw_top_levels": tops,
    "real_runs_double_cleanup": 3,
    "git_blobs_read": len(git_cache),
}, ensure_ascii=False, indent=2))
