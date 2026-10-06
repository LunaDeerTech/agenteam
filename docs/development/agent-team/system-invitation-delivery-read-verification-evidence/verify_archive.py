#!/usr/bin/env python3
"""Read saved bytes and fixed local Git objects; never execute archived commands."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parent
REPOSITORY = ROOT.parents[3]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def archived(path):
    target = (ROOT / path).resolve()
    assert target.is_relative_to(ROOT), path
    return target.read_bytes()


index = json.loads(archived("archive-index.json"))
assert index["format"] == 1
entries = {}
for entry in index["originals"]:
    data = archived(entry["archive"])
    assert (sha(data), len(data)) == (entry["sha256"], entry["bytes"]), entry["logical"]
    assert entry["logical"] not in entries
    entries[entry["logical"]] = entry
physical = {e["archive"] for e in entries.values()}
assert {str(p.relative_to(ROOT)) for p in ROOT.rglob("*") if p.is_file()} == physical | {
    "archive-index.json", "verify_archive.py", "README.md",
}


def original(logical):
    return archived(entries[logical]["archive"])


def document(logical):
    return json.loads(original(logical))


inputs = {}
source_roots = {
    "input01": "author/runs/author01/input/",
    "input02": "author/runs/author02/input/",
    "input03": "author/input03/",
    "input04": "author/input04/",
}
for revision, prefix in source_roots.items():
    inputs[revision] = document("author/" + revision + ".json")
    assert len(inputs[revision]) == 10
    for path, expected in inputs[revision].items():
        assert sha(original(prefix + path)) == expected, (revision, path)
final = inputs["input04"]
product_paths = set(final) - {"go.mod", "go.sum"}
assert len(product_paths) == 8
for revision in ("input01", "input02", "input03"):
    for path, expected in final.items():
        if path not in {
            "internal/central/account/http_invitation_delivery_test.go",
            "tests/account/invitation_delivery_query_test.go",
        }:
            assert inputs[revision][path] == expected, (revision, path)

git_cache = {}


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


for path, expected in final.items():
    assert sha(blob(index["accepted_commit"], path)) == expected, path
for path in ("go.mod", "go.sum"):
    assert sha(blob(index["product_base"], path)) == final[path]
changed = git(
    "diff", "--name-only", index["product_base"], index["accepted_commit"], "--",
    "api", "cmd", "db", "internal", "scripts", "tests", "web", "go.mod", "go.sum",
).decode().splitlines()
assert set(changed) == product_paths
static = document("author/static04.json")
assert len(static["migration_prefix_sha256"]) == 18
for name, expected in static["migration_prefix_sha256"].items():
    for commit in (index["product_base"], index["accepted_commit"]):
        assert sha(blob(commit, "db/migrations/" + name)) == expected, name
migration = blob(index["accepted_commit"], "db/migrations/00019_account_invitation_delivery_read.sql")
assert migration == (
    b"-- agenteam:transaction tx\n-- +goose Up\n"
    b"CREATE INDEX account_invitation_delivery_link ON agenteam_account.delivery_intents(link_id,id) WHERE kind='invitation';\n"
)

report = document("verification/verification-report.json")
assert report["frozen"] and report["result"] == "PASS"
assert report["accepted_product_commit"] == index["accepted_commit"]
assert report["candidate_manifest_sha256"] == sha(original("author/input04.json"))
assert report["author_report_sha256"] == sha(original("author/author-report.md"))
for path, expected in report["artifact_hashes"].items():
    assert sha(original("verification/" + path)) == expected, path

check_count = 0
for name, count, revision in (
    ("pure02", 6, "input01"), ("pure03", 2, "input02"),
    ("pure04", 5, "input03"), ("pure05", 2, "input04"),
):
    prefix = "author/runs/" + name + "/"
    result = document(prefix + "result.json")
    assert result["input_sha256"] == sha(original("author/" + revision + ".json"))
    assert len(result["commands"]) == count
    assert ("env" in result) == (name != "pure03")  # Historical omission remains visible.
    for number, command in enumerate(result["commands"], 1):
        filename = str(number) if name == "pure03" else f"{number:02d}"
        assert command["exit"] == 0
        assert sha(original(prefix + filename + ".raw.log")) == command["raw_sha256"]
        check_count += 1

baseline = document("author/runs/plan01/baseline.json")
author_review = document("verification/author-evidence-review.json")
reviewed_runs = {r["run"]: r for r in author_review["real_records"]}
tops = {}
for owner, name, revision, expected_exit, resource_count, pid_count in (
    ("author", "plan01", None, 0, 7, 277),
    ("author", "author01", "input01", 1, 9, 94),
    ("author", "author02", "input02", 1, 7, 77),
    ("author", "author03", "input03", 1, 7, 74),
    ("author", "author04", "input04", 0, 7, 71),
    ("verification", "independent01", "input04", 0, 9, 212),
):
    prefix = owner + "/runs/" + name + "/"
    command = document(prefix + "command.json")
    result = document(prefix + "result.json")
    raw = original(prefix + "raw.log")
    assert command["actual_wait_completed"] and command["exit"] == expected_exit, name
    assert result["driver_exit"] == expected_exit and result["source_files_unchanged"], name
    assert result["verification_inputs_unchanged"] and result["double_cleanup"], name
    matches = re.findall(r"^--- (PASS|FAIL|SKIP): (\S+) \(([0-9.]+)s\)$", raw.decode(), re.M)
    tops[name] = [[state, test] for state, test, _ in matches]
    assert tops[name] == result["top_levels"], name
    assert not any(state == "SKIP" for state, _, _ in matches), name
    assert bool(expected_exit) == any(state == "FAIL" for state, _, _ in matches), name
    if owner == "author":
        assert sha(raw) == reviewed_runs[name]["raw_sha256"]
        assert tops[name] == reviewed_runs[name]["top_levels"]
    else:
        assert sha(raw) == report["independent_dynamic"]["raw_sha256"]
        assert command["seconds"] == report["independent_dynamic"]["seconds"] == 145.337
        assert tops[name] == [[t["result"], t["name"]] for t in report["independent_dynamic"]["tests"]]
    before = document(prefix + "input-before.json")
    after = document(prefix + "input-after.json")
    assert before["files"] == after["files"], name  # Historical HEAD may move for documentation.
    if revision:
        assert before["files"] == inputs[revision], name
    observed = document(prefix + "observed-resources.json")
    assert len(observed) == result["observed_resources"] == resource_count, name
    assert len(document(prefix + "observed-processes.json")) == result["observed_processes"] == pid_count
    assert document(prefix + "baseline.json") == baseline, name
    assert document(prefix + "monitor-errors.json") == document(prefix + "adopted-waits.json") == []
    assert result["monitor_errors"] == 0
    cleanup = document(prefix + "cleanup.json")
    assert len(cleanup) == 2
    for check in cleanup:
        assert set(check["exact_absent"]) == set(observed), name
        assert all(value["absent"] for value in check["exact_absent"].values()), name
        assert check["baseline_unchanged"], name
        assert check["owned_processes"] == check["remaining_new"] == check["runtime_entries"] == [], name

probe_input = document("verification/runs/independent01/verification-input.json")
assert probe_input["driver_sha256"] == sha(original("verification/run_fixture.py"))
assert probe_input["overlay_sha256"] == sha(original("verification/overlay.json"))
for absolute, expected in probe_input["probe_sha256"].items():
    relative = str(Path(absolute).relative_to(index["verification_root"]))
    assert sha(original("verification/" + relative)) == expected
compile_record = document("verification/runs/compile01/command.json")
assert compile_record["exit"] == 0 and compile_record["actual_wait_completed"]
assert compile_record["inputs_unchanged"]
assert sha(original("verification/runs/compile01/raw.log")) == compile_record["raw_sha256"]
for path, expected in compile_record["inputs"].items():
    assert sha(original("verification/" + path)) == expected

query_path = "tests/account/invitation_delivery_query_test.go"
helper_sections = []
for revision in ("input01", "input04"):
    source = original(source_roots[revision] + query_path)
    helper_sections.append(source[source.index(b"const invitationIndexMigration"):source.index(b"type invitationPlannedGate")])
assert helper_sections[0] == helper_sections[1]
assert sha(helper_sections[0]) == document("author/evidence-reuse.json")["sha256"]
assert sha(helper_sections[0]) == author_review["unchanged_migration_and_plan_section_sha256"]
plan_counts = {}
for run, expected_count in (("plan01", 10), ("author01", 11)):
    prefix = "author/runs/" + run + "/"
    raw = original(prefix + "raw.log")
    summary = document(prefix + "plans-summary.json")
    if run == "author01":
        assert summary["derived_from_original_raw"] and summary["raw_sha256"] == sha(raw)
        plans = summary["plans"]
    else:
        assert isinstance(summary, list)  # Earlier original summary has no wrapper.
        plans = summary
    decoded = {}
    text = raw.decode()
    for match in re.finditer(r"EXPLAIN (\S+)/(\S+) (\[)", text):
        plan, _ = json.JSONDecoder().raw_decode(text[match.start(3):])
        decoded[(match[1], match[2])] = plan[0]
    assert len(decoded) == len(plans) == expected_count
    for saved in plans:
        assert decoded[(saved["stage"], saved["case"])]["Execution Time"] == saved["execution_ms"]
    plan_counts[run] = len(decoded)

assert len(index["missing_plan01_source_originals"]) == 2
for missing in index["missing_plan01_source_originals"]:
    assert document("author/runs/plan01/input-before.json")["files"][missing["path"]] == missing["sha256"]
    assert missing["sha256"] not in {entry["sha256"] for entry in entries.values()}

print(json.dumps({
    "status": "PASS: saved bytes, fixed Git, original results and cleanup only",
    "logical_originals": len(entries), "physical_originals": len(physical),
    "original_bytes": sum(len(archived(p)) for p in physical),
    "author_inputs": 4, "accepted_sources": 8, "unchanged_locks": 2,
    "unchanged_migrations": 18, "sole_new_migration": "00019",
    "author_check_commands": check_count, "independent_compile_commands": 1,
    "real_runs_double_cleanup": 6, "raw_top_levels": tops,
    "original_explain_records": plan_counts, "git_blobs_read": len(git_cache),
    "historical_missing_source_originals": 2, "pure03_environment_record": "absent",
}, ensure_ascii=False, indent=2))
