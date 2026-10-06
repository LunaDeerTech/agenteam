#!/usr/bin/env python3
"""Verify saved evidence and local Git blobs; never run archived commands."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parent
REPOSITORY = ROOT.parents[3]
index = json.loads((ROOT / "archive-index.json").read_bytes())
assert index["format"] == 1
git_cache = {}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def git(*args):
    return subprocess.check_output(
        ["git", *args], cwd=REPOSITORY,
        env={**os.environ, "GIT_NO_LAZY_FETCH": "1"},
    )


def blob(commit, path):
    key = commit + ":" + path
    if key not in git_cache:
        git_cache[key] = git("cat-file", "blob", key)
    return git_cache[key]


def archived(path):
    target = (ROOT / path).resolve()
    assert target.is_relative_to(ROOT), path
    return target.read_bytes()


entries = {}
physical = set()
for entry in index["originals"]:
    assert entry["logical"] not in entries
    if "git" in entry:
        assert "archive" not in entry
        data = blob(entry["git"]["commit"], entry["git"]["path"])
    else:
        physical.add(entry["archive"])
        data = archived(entry["archive"])
    assert (sha(data), len(data)) == (entry["sha256"], entry["bytes"]), entry["logical"]
    entries[entry["logical"]] = entry
assert {str(p.relative_to(ROOT)) for p in ROOT.rglob("*") if p.is_file()} == physical | {
    "archive-index.json", "verify_archive.py", "README.md",
}
assert len(entries) == index["counts"]["logical"]
assert len(physical) == index["counts"]["physical"]
assert sum(len(archived(p)) for p in physical) == index["counts"]["physical_bytes"]


def original(logical):
    entry = entries[logical]
    if "git" in entry:
        return blob(entry["git"]["commit"], entry["git"]["path"])
    return archived(entry["archive"])


def document(logical):
    return json.loads(original(logical))


base = index["product_base"]
accepted = index["accepted_commit"]
inputs = {}
for revision in ("input01", "input02"):
    manifest = document("author/" + revision + ".json")
    inputs[revision] = manifest["files"]
    assert manifest["base_commit"] == base and len(manifest["files"]) == 4
    for path, expected in manifest["files"].items():
        assert sha(original("author/" + revision + "/" + path)) == expected
assert document("author/input02.json")["previous_manifest_sha256"] == sha(original("author/input01.json"))
for path, expected in inputs["input02"].items():
    assert sha(blob(accepted, path)) == expected, path
changed = git("diff", "--name-only", base, accepted, "--", "web", "tests").decode().splitlines()
assert set(changed) == set(inputs["input02"])

baseline = document("author/baseline-input.json")
for path, expected in baseline["files"].items():
    assert sha(blob(base, path)) == expected, path
for revision, name in (("input01", "isolated-input.json"), ("input02", "isolated-input02.json")):
    isolated = document("author/" + name)
    assert not isolated["active_invitation_inputs_consumed"]
    assert len(isolated["files"]) == 103
    for path, expected in isolated["files"].items():
        data = original("author/" + revision + "/" + path) if path in inputs[revision] else blob(base, path)
        assert sha(data) == expected, (revision, path)
for revision, name in (("input01", "closure-input.json"), ("input02", "closure-input02.json")):
    closure = document("author/" + name)
    assert len(closure["files"]) == 11
    for path, expected in closure["files"].items():
        path = path if path.startswith("web/") else "web/" + path
        data = original("author/" + revision + "/" + path) if path in inputs[revision] else blob(base, path)
        assert sha(data) == expected, path
    for path, expected in closure["locks"].items():
        assert sha(blob(base, path)) == sha(blob(accepted, path)) == expected, path
fixed_doc = document("author/fixed-doc-dependency.json")
assert sha(blob(base, fixed_doc["path"])) == fixed_doc["sha256"]

author_count = 0
author_processes = set()
for name in ("run-index.json", "run-index02.json"):
    for recorded in document("author/" + name):
        prefix = "author/runs/" + recorded["run"] + "/"
        command = document(prefix + "command.json")
        terminal = document(prefix + "terminal.json")
        assert command["argv"] and command["cwd"] and command["env"]
        assert terminal["exit"] == recorded["exit"]
        assert terminal["actual_wait_completed"] and not terminal.get("timed_out", False)
        assert sha(original(prefix + "raw.log")) == terminal["raw_sha256"] == recorded["raw_sha256"]
        for path, expected in command.get("inputs", {}).items():
            assert sha(original(prefix + "input/" + path)) == expected, (prefix, path)
        if prefix + "input-before.json" in entries:
            before = document(prefix + "input-before.json")
            assert before == document(prefix + "input-after.json")
            assert terminal["input_unchanged"]
            for path, expected in before.items():
                assert sha(original(prefix + "input/" + path)) == expected, (prefix, path)
        if "tracked_processes" in terminal:
            assert terminal["remaining_first"] == terminal["remaining_second"] == terminal["tmp_contents"] == []
            assert terminal["ports_after"] == document(prefix + "baseline.json")["ports"]
            author_processes.update((p["pid"], p["starttime"]) for p in terminal["tracked_processes"])
            if prefix + "server-0.json" in entries:
                assert document(prefix + "server-0.json")["closed"]
        author_count += 1
assert author_count == 20
author_cleanup = document("author/cleanup-final02.json")
assert len(author_processes) == author_cleanup["owned_pid_starttime_count"] == 170
assert author_cleanup["remaining_first"] == author_cleanup["remaining_second"] == []
assert author_cleanup["ports_unchanged"] and author_cleanup["browser_window_released"]
for name, result in author_cleanup["real_runs"].items():
    assert result["actual_wait_completed"] and result["tmp_absent"]
    assert result["remaining_first"] == result["remaining_second"] == []
    assert all(result["server_closed"])
    assert sha(original("author/runs/" + name + "/terminal.json")) == result["terminal_sha256"]

spec_path = "tests/account-captcha-web/e2e/dialog-outside-focus.spec.ts"
assert original("author/runs/old02/input/" + spec_path) == original("author/runs/new01/input/" + spec_path)
assert "author/runs/new01/input-before.json" not in entries
assert "author/runs/new01/input.json" not in entries  # Historical gap stays visible.
red = document("author/runs/repair-old01/input-before.json")
green = document("author/runs/repair-new01/input-before.json")
assert red[spec_path] == green[spec_path] == inputs["input02"][spec_path]
for path in red:
    if path not in {"web/src/composables/useLayer.ts", "web/src/tests/dialog-outside-focus.spec.ts"}:
        assert red[path] == green[path]
for run, passed in (("new02", 30), ("repair-new01", 10)):
    stats = document("author/runs/" + run + "/results.json")["stats"]
    assert stats["expected"] == passed and stats["unexpected"] == stats["skipped"] == stats["flaky"] == 0
for run, count in (("pure02", 287), ("repair-unit01", 52), ("repair-components01", 1)):
    raw = original("author/runs/" + run + "/raw.log").decode()
    assert re.search(r"Tests\s+" + str(count) + r" passed", raw), run

report = document("verification/verification-report.json")
assert report["conclusion"].startswith("PASS for modal-focus-restoration rev2 fixed input02")
assert report["frozen"] and report["no_more_execution_planned"]
assert report["product_input"]["sha256"] == sha(original("author/input02.json"))
assert report["product_input"]["files"] == inputs["input02"]
assert report["author_reuse"]["sha256"] == sha(original("author/author-report02.md"))
for path, expected in report["artifact_hashes"].items():
    assert sha(original("verification/" + path)) == expected, path
independent_processes = set()
adopted = 0
for run, recorded in report["results"].items():
    prefix = "verification/runs/" + run + "/"
    result = document(prefix + "result.json")
    assert result == recorded and result["actual_wait_completed"] and result["clean"]
    assert result["inputs_unchanged"] and not result["timed_out"]
    assert sha(original(prefix + "raw.log")) == result["raw_sha256"]
    before = document(prefix + "input-before.json")
    assert before == document(prefix + "input-after.json")
    for path, expected in before.items():
        source = prefix + "source/" + path
        if source in entries:
            data = original(source)
        elif "verification/" + path in entries:
            data = original("verification/" + path)
        else:
            tree, path = path.split("/", 1)
            assert tree in {"candidate", "candidate02"}
            revision = "input01" if tree == "candidate" else "input02"
            data = original("author/" + revision + "/" + path) if path in inputs[revision] else blob(base, path)
        assert sha(data) == expected, (run, source)
    cleanup = document(prefix + "cleanup.json")
    assert cleanup["actual_wait_completed"] and cleanup["subreaper_enabled_before_launch"]
    assert cleanup["remaining_first"] == cleanup["remaining_second"] == []
    assert cleanup["tmp_absent"] and cleanup["server_all_closed"] and cleanup["ports_unchanged"]
    assert cleanup["ports_after"] == document(prefix + "baseline.json")["ports"]
    independent_processes.update((p["pid"], p["starttime"]) for p in cleanup["owned_processes"])
    adopted += len(cleanup["adopted_waits"])
    assert all(server["closed"] for server in cleanup["servers"])
final_cleanup = document("verification/cleanup-final.json")
assert len(independent_processes) == final_cleanup["owned_pid_starttime_count"] == 68
assert adopted == final_cleanup["adopted_actual_wait_count"] == 18
assert final_cleanup["remaining_first"] == final_cleanup["remaining_second"] == []
assert final_cleanup["ports_unchanged_from_first_browser_baseline"] and final_cleanup["component_window_released"]
for owner, cleanup, key in (
    ("author", author_cleanup, "historical_not_owned_not_signalled"),
    ("verification", final_cleanup, "historical_unowned_not_touched"),
):
    historical = {p["pid"]: p for p in cleanup[key]}
    for pid in (180182, 180185):
        assert historical[pid]["ppid"] == 1 and historical[pid]["state"] == "Z"
assert original("verification/runs/browser02/source/candidate/" + spec_path) == original(
    "verification/runs/browser03/source/candidate02/" + spec_path
)
for run, count in (("browser03", 3), ("browser04", 1)):
    stats = document("verification/runs/" + run + "/results.json")["stats"]
    assert stats["expected"] == count and stats["unexpected"] == stats["skipped"] == stats["flaky"] == 0
for run, code in (("browser-types01", 2), ("browser-types02", 0)):
    result = document("verification/runs/" + run + "/result.json")
    assert result["exit"] == code and result["actual_wait_completed"]

print(
    "PASS: saved bytes/local Git only; "
    f"{len(entries)} logical, {len(physical)} physical, {index['counts']['git_references']} Git references; "
    f"2 inputs/4 delivered sources; {author_count} author commands, 6 independent runs/2 compiler records; "
    f"170 author + 68 independent PID/starttime records, 18 independent adopted waits; "
    f"{len(git_cache)} fixed Git blobs. No product or resource execution."
)
