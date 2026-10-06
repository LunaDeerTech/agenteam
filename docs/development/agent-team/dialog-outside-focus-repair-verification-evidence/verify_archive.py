#!/usr/bin/env python3
"""Read archived bytes and local Git only; never execute recorded commands."""

import hashlib
import json
import os
from pathlib import Path
import subprocess


ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[3]
INDEX = json.loads((ROOT / "archive-index.json").read_text())
BASE = INDEX["baseline_commit"]
ACCEPTED = INDEX["accepted_commit"]
PREFIX = {
    "a": "/workspace/scratch/agenteam-dialog-focus-author-je2dVDnE/",
    "v": "/workspace/scratch/agenteam-dialog-focus-verification-vqncynra/",
    "u": "/workspace/scratch/agenteam-d27-directory-ui-verification-nrfghn71/",
}


def sha(data):
    return hashlib.sha256(data).hexdigest()


originals = {}
physical = {}
for entry in INDEX["entries"]:
    path = ROOT / entry["archive"]
    assert path.resolve().is_relative_to(ROOT), "archive path escapes evidence"
    data = path.read_bytes()
    assert sha(data) == entry["sha256"] and len(data) == entry["bytes"], entry
    assert entry["source"] not in originals, "duplicate logical original"
    originals[entry["source"]] = data
    physical[entry["archive"]] = data
assert len(originals) == INDEX["logical_originals"]
assert len(physical) == INDEX["physical_originals"]
assert sum(map(len, originals.values())) == INDEX["logical_bytes"]
assert sum(map(len, physical.values())) == INDEX["physical_original_bytes"]


def raw(owner, name):
    return originals[PREFIX[owner] + name]


def record(owner, name):
    return json.loads(raw(owner, name))


git_blobs = {}


def git(commit, name):
    key = commit + ":" + name
    if key not in git_blobs:
        git_blobs[key] = subprocess.check_output(
            ["git", "cat-file", "blob", key],
            cwd=REPO,
            env={**os.environ, "GIT_NO_LAZY_FETCH": "1"},
        )
    return git_blobs[key]


candidate = record("a", "input01.json")
frozen = record("a", "frozen-input.json")
independent = record("v", "input01.json")
assert len(candidate) == 4
assert candidate == frozen["candidate_paths"] == independent["candidate_four_paths"]
assert sha(raw("a", "input01.json")) == independent["author_input_sha256"]
assert frozen["baseline"] == independent["dependency_base"] == BASE
for name, digest in candidate.items():
    assert sha(raw("a", "candidate/" + name)) == digest == sha(git(ACCEPTED, name))
for name, digest in frozen["pure_inputs"].items():
    commit = ACCEPTED if name in candidate else BASE
    assert sha(git(commit, name)) == digest, (commit, name)
assert len(independent["files"]) == 10
assert independent["files"] == frozen["component_closure"]
for name, digest in independent["files"].items():
    path = "web/" + name
    assert sha(git(ACCEPTED if path in candidate else BASE, path)) == digest
assert frozen["changed_web_base"] == ["web/src/components/ui/UiDialog.vue"]
assert frozen["old_new_closure_delta"] == ["src/components/ui/UiDialog.vue"]
assert raw("a", "runs/old01/input/UiDialog.vue") == git(BASE, "web/src/components/ui/UiDialog.vue")
for name in ("dialog-outside-focus.config.js", "e2e/dialog-outside-focus.spec.ts"):
    path = "tests/account-captcha-web/" + name
    assert raw("a", "runs/old01/input/" + path) == raw("a", "candidate/" + path)

for run, exit_code in (("old01", 1), ("new01", 0), ("pure01", 1), ("pure02", 0)):
    terminal = record("a", "runs/" + run + "/terminal.json")
    assert terminal["exit"] == exit_code
    assert terminal["raw_sha256"] == sha(raw("a", "runs/" + run + "/raw.log"))
for run, passed, failed in (("old01", 0, 1), ("new01", 11, 0)):
    stats = record("a", "runs/" + run + "/results.json")["stats"]
    assert (stats["expected"], stats["unexpected"], stats["skipped"], stats["flaky"]) == (passed, failed, 0, 0)
    terminal = record("a", "runs/" + run + "/terminal.json")
    assert record("a", "runs/" + run + "/baseline.json")["ports"] == terminal["ports_after"]
    assert record("a", "runs/" + run + "/server-0.json")["closed"]
    events = record("a", "runs/" + run + "/pointer-events.json")
    assert len(events) == passed + failed and all(not e["blocked"] for e in events)
assert record("a", "runs/harness-types01-tool-result.json")["exit"] == 2
harness = record("a", "runs/harness-pure02/terminal.json")
assert len(harness["commands"]) == 3 and all(c["exit"] == 0 for c in harness["commands"])
assert harness["raw_sha256"] == sha(raw("a", "runs/harness-pure02/raw.log"))
new = record("a", "runs/new01/terminal.json")
assert not new["remaining_first"] and not new["remaining_second"] and not new["tmp_contents"]
assert len(new["reaped"]) == 4 and len(new["tracked_processes"]) == 41
orphans = record("v", "author-old01-orphans.json")["rows"]
assert {r["pid"] for r in orphans} == {180182, 180185}
assert all(r["state"] == "Z" and r["ppid"] == 1 and r["starttime"] == "408854" for r in orphans)
for sweep in ("remaining_first", "remaining_second"):
    rows = record("a", "runs/old01/terminal.json")[sweep]
    assert {r["pid"] for r in rows} == {180182, 180185}
    assert all(r["state"] == "Z" and r["ppid"] == 1 and r["starttime"] == 408854 for r in rows)

for run in ("independent01", "independent02"):
    prefix = "runs/" + run + "/"
    command = record("v", prefix + "command.json")
    result = record("v", prefix + "result.json")
    assert command["actual_wait_completed"] and command["exit"] == result["actual_exit"] == 0
    assert command["raw_sha256"] == sha(raw("v", prefix + "raw.log"))
    assert command["input_sha256"] == sha(raw("v", "input01.json"))
    assert command["source_unchanged"] and command["probes_unchanged"] and command["monitor_unchanged"]
    for name, digest in command["probe_sha256"].items():
        assert sha(raw("v", prefix + name + ".txt")) == digest
    assert sha(raw("v", prefix + "monitor.py.txt")) == command["monitor_sha256"]
    assert len(record("v", prefix + "observed-processes.json")) == result["observed_processes"] == 13
    waits = record("v", prefix + "adopted-waits.json")
    assert len(waits) == result["adopted_waits"] == 4 and all(w["actual_wait"] for w in waits)
    assert record("v", prefix + "server-terminal.json")["actual_server_close"]
    cleanup = record("v", prefix + "cleanup.json")
    assert len(cleanup) == 2 and all(not c["owned_processes"] and c["baseline_listeners_unchanged"] for c in cleanup)
    facts = record("v", prefix + "independent-result.json")
    assert facts["completed"] and facts["late_focus_not_stolen"]
    assert [p["prevented"] for p in facts["pointerEvents"]] == [True, True, False, False, True]
assert not record("v", "runs/independent01/result.json")["clean"]
later = record("v", "runs/independent01/post-run-cleanup.json")
assert later["final_clean"] and len(later["checks"]) == 2
assert all(not c["owned_processes"] and c["browser_runtime_removed"] and c["baseline_listeners_unchanged"] for c in later["checks"])
assert record("v", "runs/independent02/result.json")["clean"]
assert all(not c["browser_runtime_entries"] for c in record("v", "runs/independent02/cleanup.json"))

for run, expected_exit in (("focus-mechanism01", 1), ("focus-mechanism02", 0)):
    command = record("u", "runs/" + run + "/command.json")
    assert command["exit"] == expected_exit and command["actual_wait_completed"]
    assert command["raw_sha256"] == sha(raw("u", "runs/" + run + "/raw.log"))
mechanism = record("u", "runs/focus-mechanism02/events.json")
assert [c["active"] for c in mechanism["cases"]] == ["body", "trigger"]
assert mechanism["browser_close_completed"]
history = record("u", "runs/new02/result.json")
assert history["driver_exit"] == 1 and not history["selected_tests_passed"]
assert [t[0] for t in history["top_levels"]] == ["PASS", "FAIL", "FAIL"]
assert sha(raw("u", "runs/new02/raw.log")) == "ea421557434f5d918b9849297d6cbd208e4df76bfd3fd64a7dfb9918db255409"

print(json.dumps({
    "result": "PASS: archived bytes and fixed Git inputs only",
    "logical_originals": len(originals), "physical_originals": len(physical),
    "accepted_sources": len(candidate), "component_closure": len(independent["files"]),
    "pure_input_files": len(frozen["pure_inputs"]), "local_git_blobs": len(git_blobs),
    "author_browser": "old01 FAIL; new01 11 PASS",
    "independent": "one group, two PASS runs",
    "preserved_limits": ["old01 two PPID1 Z", "independent01 initial clean=false", "preparation failures", "business UI not accepted"],
}, ensure_ascii=False, indent=2))
