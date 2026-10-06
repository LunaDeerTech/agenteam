#!/usr/bin/env python3
"""Check archived bytes and local Git objects, without executing saved commands."""

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
assert sum("git" in e for e in entries.values()) == index["counts"]["git_references"]


def original(logical):
    entry = entries[logical]
    if "git" in entry:
        return blob(entry["git"]["commit"], entry["git"]["path"])
    return archived(entry["archive"])


def document(logical):
    return json.loads(original(logical))


def by_source(path):
    matches = [e for e in entries.values() if e["source"] == path]
    assert len(matches) == 1, path
    return original(matches[0]["logical"])


author = document("author/author-report.json")
verifier = document("verifier/verification-report.json")
accepted = index["accepted_commit"]
inputs = {}
for revision, info in author["inputs"].items():
    raw = original("author/" + revision + ".json")
    assert sha(raw) == info["sha256"]
    manifest = json.loads(raw)
    inputs[revision] = manifest
    assert len(manifest["files"]) == 21 and len(manifest["dist"]) == 24
    for path, expected in manifest["files"].items():
        assert sha(original("author/" + revision + "-source/" + path)) == expected
    for check in manifest["checks"].values():
        assert sha(by_source(check["log"])) == check["sha256"]
    for field in ("preserved_regression_red", "rev4_preserved_local_failures"):
        for path, expected in manifest.get(field, {}).items():
            assert sha(original("author/" + path)) == expected
    if revision != "input01":
        previous = "input%02d" % (int(revision[-2:]) - 1)
        assert manifest["previous_input_sha256"] == sha(original("author/" + previous + ".json"))
        changed = {p for p, h in manifest["files"].items() if h != inputs[previous]["files"][p]}
        assert changed == set(info["delta"])
    for path, expected in manifest["locks"].items():
        assert sha(blob(index["backend_base"], path)) == sha(blob(accepted, path)) == expected
    for path, expected in manifest.get("accepted_shared", {}).get("files", {}).items():
        assert sha(blob(index["product_base"], path)) == sha(blob(accepted, path)) == expected

final = inputs["input07"]
web = {p: h for p, h in final["files"].items() if p.startswith("web/")}
assert len(web) == 17
for revision in ("input03", "input04", "input05", "input06", "input07"):
    assert {p: h for p, h in inputs[revision]["files"].items() if p.startswith("web/")} == web
    assert inputs[revision]["dist"] == final["dist"]
for path, expected in final["files"].items():
    assert sha(blob(accepted, path)) == expected, path
changed = git(
    "diff", "--name-only", index["product_base"], accepted, "--",
    "web", "tests", "api", "db", "internal", "cmd", "scripts", "go.mod", "go.sum",
).decode().splitlines()
assert set(changed) == set(final["files"])
assert git("diff", "--name-only", index["backend_base"], accepted, "--", "db/migrations") == b""

readme = document("author/readme-final01/manifest.json")
for name, key in (("README.original.md.txt", "original_sha256"), ("README.final.md.txt", "final_sha256"), ("README.diff", "diff_sha256")):
    assert sha(original("author/readme-final01/" + name)) == readme[key]
assert sha(blob(accepted, readme["path"])) == readme["final_sha256"]
assert sha(blob(index["product_base"], readme["path"])) == readme["original_sha256"]
assert readme["checks"]["source_input_sha256"] == sha(original("author/input07.json"))
assert readme["checks"]["source_dist_locks_shared_unchanged"]
card = blob(accepted, "docs/development/work-items/d27-system-invitation-ui.md")
assert sha(b"## 1." + card.split(b"## 1.", 1)[1]) == index["card_technical_sha256"]


def check_real(prefix, manifest, expected_exit):
    command = document(prefix + "command.json")
    result = document(prefix + "result.json")
    assert command["argv"] and command["cwd"] and command["env"]
    assert command["actual_wait_completed"] and command["exit"] == expected_exit
    assert result["driver_exit"] == expected_exit
    assert result["double_cleanup"] and result["source_files_unchanged"]
    assert result["verification_inputs_unchanged"] and result["browser_runtime_removed"]
    before = document(prefix + "input-before.json")
    assert before == document(prefix + "input-after.json")
    for key in ("files", "dist", "locks"):
        assert before[key] == manifest[key], (prefix, key)
    if "accepted_shared" in before:
        assert before["accepted_shared"] == manifest["accepted_shared"]["files"]
    private = document(prefix + "verification-input.json")
    assert private["driver_sha256"] == sha(original(prefix + "driver.py.txt"))
    for path, expected in private.items():
        if path != "driver_sha256":
            assert sha(original(prefix + "source/" + path)) == expected
    resources = document(prefix + "observed-resources.json")
    processes = document(prefix + "observed-processes.json")
    waits = document(prefix + "adopted-waits.json")
    assert len(resources) == result["observed_resources"]
    assert len(processes) == result["observed_processes"]
    assert len(waits) == result["adopted_waits"] and all(w["actual_wait"] for w in waits)
    assert document(prefix + "monitor-errors.json") == [] and result["monitor_errors"] == 0
    cleanup = document(prefix + "cleanup.json")
    assert len(cleanup) == 2
    for observation in cleanup:
        assert set(observation["exact_absent"]) == set(resources)
        assert all(x["absent"] for x in observation["exact_absent"].values())
        assert observation["baseline_unchanged"]
        for key in ("owned_processes", "remaining_new", "runtime_entries", "browser_runtime_entries"):
            assert observation[key] == [], (prefix, key)
    raw = original(prefix + "raw.log").decode()
    tops = re.findall(r"^--- (PASS|FAIL): (\S+) \(([0-9.]+)s\)$", raw, re.M)
    assert [[state, name] for state, name, seconds in tops] == result["top_levels"]
    return command, result, tops


for name, info in author["runs"].items():
    prefix = "author/runs/" + name + "/"
    command, result, tops = check_real(prefix, inputs[info["input"]], info["exit"])
    assert command["seconds"] == info["seconds"]
    assert sha(original(prefix + "raw.log")) == info["raw_sha256"]
    assert sha(original(prefix + "frozen-input.json")) == sha(original("author/" + info["input"] + ".json"))
    assert [{"state": state, "name": test, "seconds": float(seconds)} for state, test, seconds in tops] == info["top_levels"]
    for path, expected in info["evidence_files"].items():
        assert sha(original(prefix + path)) == expected
    assert (result["observed_resources"], result["observed_processes"], result["adopted_waits"]) == (
        info["exact_resources"], info["owned_pid_starttimes"], info["adopted_actual_waits"],
    )
assert len(author["runs"]) == 7 and len(author["final_new6"]) == 6
for selected in author["final_new6"]:
    info = author["runs"][selected["run"]]
    assert info["input"] == selected["input"]
    assert {"state": "PASS", "name": selected["name"], "seconds": selected["seconds"]} in info["top_levels"]
assert len(author["runs"]["old01"]["top_levels"]) == 5
assert all(x["state"] == "PASS" for x in author["runs"]["old01"]["top_levels"])
for path, expected in author["screenshots"]["files"].items():
    assert sha(original("author/runs/new06/images/" + path)) == expected
assert len(author["screenshots"]["files"]) == 8

pure_exits = [1, 1, 0, 1, 0, 1, 0, 0]
for i, expected in enumerate(pure_exits, 1):
    name = "pure%02d" % i
    prefix = "verifier/runs/" + name + "/"
    result = document(prefix + "result.json")
    assert result["exit"] == expected and result["child_actual_wait"]
    assert result["inputs_equal"] and not result["timed_out"]
    assert sha(original(prefix + "raw.log")) == result["raw_sha256"]
    before = document(prefix + "input-before.json")
    assert before == document(prefix + "input-after.json")
    assert set(before) == set(index["pure_inputs"][name])
    for path, binding in index["pure_inputs"][name].items():
        assert sha(original(binding["original"])) == before[path]
pure_cleanup = document("verifier/pure-input03-cleanup.json")
assert pure_cleanup["actual_driver_and_child_exit_confirmed_by_tool_wait"]
assert pure_cleanup["remaining_first"] == pure_cleanup["remaining_second"] == []

for path, expected in verifier["artifact_hashes"].items():
    assert sha(by_source(path)) == expected
for name, expected in (("independent01", 1), ("independent02", 0)):
    prefix = "verifier/browser-probe01/runs/" + name + "/"
    command, result, tops = check_real(prefix, final, expected)
    assert (result["observed_resources"], result["observed_processes"], result["adopted_waits"]) == (7, 86, 4)
    if name == "independent02":
        assert command == verifier["independent_real"]["command"]
        assert result == verifier["independent_real"]["result"]
        assert sha(original(prefix + "raw.log")) == verifier["independent_real"]["raw_sha256"]
        assert command["seconds"] == 56.812 and tops[0][2] == "9.48"
    raw = original(prefix + "raw.log")
    assert (b"Running 2 tests" if name == "independent01" else b"Running 1 test") in raw
    assert (b"independent composite PASS:" in raw) == (name == "independent02")
old_config = original("verifier/browser-probe01/runs/independent01/source/independent.config.js")
new_config = original("verifier/browser-probe01/runs/independent02/source/independent.config.js")
assert new_config.replace(b" testIgnore: ['**/runs/**'],", b"") == old_config
for name in ("type01", "compile01", "list02"):
    record = document("verifier/browser-probe01/compile/" + name + ".json")
    assert record["exit"] == 0 and record["actual_wait"]
    for path, expected in record.get("inputs", {}).items():
        assert sha(original("verifier/browser-probe01/" + path)) == expected
assert document("verifier/browser-probe01/compile/list02.json")["exact_one"]
assert b"Total: 1 test" in original("verifier/browser-probe01/compile/list02.log")

print(
    "PASS archive/Git only:", len(entries), "logical /", len(physical), "physical /",
    index["counts"]["git_references"], "Git references; 7 inputs / 22 delivery paths;",
    "7 author real runs (combined new6+old5), 8 independent pure runs,",
    "2 independent real runs (original failure retained), 3 compile/list records;",
    "saved cleanup consistent; no product or live-resource execution.",
)
