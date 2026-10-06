#!/usr/bin/env python3
"""Check fixed specification versions and original failure evidence; never execute probes."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from urllib.parse import unquote

HERE = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--repo", type=Path, required=True)
parser.add_argument("--candidate-root", type=Path)
args = parser.parse_args()
repo = args.repo.resolve()
candidate = (args.candidate_root or repo).resolve()
index = json.loads((HERE / "index.json").read_text())
env = dict(os.environ, GIT_NO_LAZY_FETCH="1", PYTHONDONTWRITEBYTECODE="1")


def sha(data):
    return hashlib.sha256(data).hexdigest()


def git(ref):
    return subprocess.check_output(["git", "-C", str(repo), "show", ref], env=env)


def selected(path):
    path = Path(path)
    value = candidate / path
    return value if value.is_file() else repo / path


def technical(data):
    return data[data.index(b"## 1. "):]


def scope(data):
    return re.findall(br"^\| (\d+) \| `([^`]+)`", data, re.M)


def apply_diff(old, delta):
    source = old.splitlines(keepends=True)
    lines = delta.splitlines(keepends=True)
    assert lines[0].startswith(b"--- ") and lines[1].startswith(b"+++ ")
    output, cursor, pos = [], 0, 2
    while pos < len(lines):
        match = re.match(br"@@ -(\d+)(?:,\d+)? \+\d+(?:,\d+)? @@", lines[pos])
        assert match, "invalid original diff"
        start = max(0, int(match.group(1)) - 1)
        assert start >= cursor
        output.extend(source[cursor:start])
        cursor = start
        pos += 1
        while pos < len(lines) and not lines[pos].startswith(b"@@ "):
            line = lines[pos]
            if line[:1] in (b" ", b"-"):
                assert source[cursor] == line[1:], "original diff context mismatch"
                if line.startswith(b" "):
                    output.append(line[1:])
                cursor += 1
            elif line.startswith(b"+"):
                output.append(line[1:])
            else:
                raise AssertionError("unsupported diff marker")
            pos += 1
    output.extend(source[cursor:])
    return b"".join(output)


git_data = {}
for ref, item in index["git_references"].items():
    assert ref == item["commit"] + ":" + item["path"]
    data = git(ref)
    assert sha(data) == item["sha256"] and len(data) == item["bytes"], ref
    blob = subprocess.check_output(["git", "-C", str(repo), "rev-parse", ref], env=env).decode().strip()
    assert blob == item["blob"], ref
    git_data[ref] = data
assert len(git_data) == index["counts"]["fixed_git_references"]

originals, objects = {}, set()
for name, item in index["artifacts"].items():
    if item["storage"] == "object":
        assert item["object"] == "objects/" + item["sha256"]
        data = (HERE / item["object"]).read_bytes()
        objects.add(item["sha256"])
    else:
        assert item["storage"] == "git"
        data = git_data[item["git_ref"]]
    assert sha(data) == item["sha256"] and len(data) == item["bytes"], name
    originals[name] = data
assert len(originals) == index["counts"]["logical_artifacts"]
assert {p.name for p in (HERE / "objects").iterdir()} == objects
assert len(objects) == index["counts"]["new_objects"]
assert sum((HERE / "objects" / h).stat().st_size for h in objects) == index["counts"]["new_object_bytes"]


def record(name):
    return json.loads(originals[name])


# The legacy checker is immutable. Its exact administrative document inputs are
# materialized from its accepted archive commit, not from the revised worktree.
for path, expected in index["legacy_files_unchanged"].items():
    assert sha((repo / path).read_bytes()) == expected, path
legacy = repo / index["legacy_evidence_path"] / "verify_archive.py"
with tempfile.TemporaryDirectory(prefix="agenteam-spa-spec-history-") as tmp:
    historical = Path(tmp)
    for path in index["legacy_historical_document_paths"]:
        target = historical / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(git(index["historical_archive_commit"] + ":" + path))
    run = subprocess.run(
        [sys.executable, str(legacy), "--repo", str(repo), "--candidate-root", str(historical)],
        env=env, capture_output=True, timeout=60,
    )
    assert run.returncode == 0, run.stdout.decode() + run.stderr.decode()
    assert not run.stderr
    print("Historical rev1: " + run.stdout.decode().strip())

v1 = git_data[index["original_spec_commit"] + ":" + index["card_path"]]
initial = originals["revision/initial/d28-central-spa-hosting.md"]
v2 = originals["revision/v2/d28-central-spa-hosting.md"]
v3 = originals["revision/v3/d28-central-spa-hosting.md"]
assert apply_diff(v1, originals["revision/initial/rev1-to-rev1.1.diff"]) == initial
assert apply_diff(initial, originals["revision/v2/rev1.1-initial-to-v2.diff"]) == v2
assert apply_diff(v1, originals["revision/v2/rev1-to-rev1.1-v2.diff"]) == v2
assert apply_diff(v2, originals["revision/v3/v2-to-v3.diff"]) == v3
assert v3 == git_data[index["spec_commit"] + ":" + index["card_path"]]
assert sha(v3) == index["card_sha256"]
assert sha(technical(v1)) == index["original_technical_sha256"]
assert sha(technical(v3)) == index["technical_sha256"]
assert technical(v2) == technical(v3)
assert len(scope(v1)) == 16
assert all(scope(version) == scope(v1) for version in (initial, v2, v3))
checks = record("revision/v2/checks.json")
changes = checks["technical_changes"]
restored = technical(v3).decode().replace(changes["linux_gate_sentence"], "", 1)
for name in ("step5", "step6"):
    restored = restored.replace(changes[name]["after"], changes[name]["before"], 1)
assert restored.encode() == technical(v1)
assert record("revision/v3/checks.json")["all_other_bytes_unchanged"] is True

manifest = record("author-stage/manifest.json")
review = record("independent/review.json")
frozen = record("independent/input.json")
assert review["status"] == "BLOCKED"
assert {f["id"] for f in review["findings"]} == {"A-SCRIPT-01", "A-SCRIPT-02"}
assert sha(originals["author-stage/manifest.json"]) == review["stage_manifest_sha256"]
assert sha(originals["author-stage/report.md"]) == review["author_report_sha256"]
assert sha(originals["independent/input.json"]) == review["input_sha256"]
assert sha(originals["independent/run-pure-probes.py"]) == review["driver_sha256"]
assert len(manifest["files"]) == 9 and len(manifest["dependencies"]) == 26
for path, expected in manifest["dependencies"].items():
    assert sha(git_data[manifest["baseline"] + ":" + path]) == expected
for path in ("scripts/build-central-web.mjs", "scripts/build-central-web.test.mjs"):
    assert sha(originals["author-stage/source/" + path]) == manifest["files"][path]
for name, item in index["artifacts"].items():
    if item["origin"] in frozen["frozen_files"]:
        assert item["sha256"] == frozen["frozen_files"][item["origin"]], name

owned_count, adopted_count = 0, 0
for run in review["independent_results"]:
    label = Path(run["run"]).name
    prefix = "independent/runs/" + label + "/"
    for name, expected in run["artifacts"].items():
        assert sha(originals[prefix + name]) == expected
    command = record(prefix + "command.json")
    result = record(prefix + "result.json")
    cleanup = record(prefix + "cleanup.json")
    observations = record(prefix + "private/observations.json")
    assert command["input_sha256"] == sha(originals["independent/input.json"])
    assert command["budget_seconds"] == 45
    assert command["argv"][2] == frozen["product_script"]
    assert record(prefix + "input-before.json") == record(prefix + "input-after.json") == frozen["frozen_files"]
    assert result == run["result"]
    assert result["actual_exit"] == 1 and result["input_unchanged"] and result["cleanup_clean"]
    assert not result["timed_out"] and result["driver_error"] is None and result["gate_error"] is None
    assert sha(originals[prefix + "raw.log"]) == result["raw_sha256"]
    assert sha(originals[prefix + "cleanup.json"]) == result["cleanup_sha256"]
    assert json.loads(originals[prefix + "raw.log"]) == observations == run["observations"]
    assert observations["pass"] is False
    assert cleanup["clean"] and cleanup["baseline_children"] == []
    assert cleanup["first_remaining"] == cleanup["second_remaining"] == []
    identities = {(p["pid"], p["starttime"]) for p in cleanup["owned"]}
    assert len(identities) == len(cleanup["owned"])
    assert all((w["pid"], w["starttime"]) in identities for w in cleanup["actual_waits"])
    assert [w["returncode"] for w in cleanup["actual_waits"] if not w["adopted"]] == [1]
    owned_count += len(identities)
    adopted_count += sum(w["adopted"] for w in cleanup["actual_waits"])
assert owned_count == 6 and adopted_count == 2
tail = record("independent/runs/child-tail01/private/observations.json")["cases"]
assert all(not c["directChildLive"] and c["descendantLive"] and not c["contractSatisfied"] for c in tail)
assert tail[0]["settledBeforeProbeCleanup"] is False and tail[1]["settledBeforeProbeCleanup"] is True
publication = record("independent/runs/publication01/private/observations.json")["cases"]
assert publication[0]["contractSatisfied"] is True
assert publication[1]["code"] == "WEB_BUILD_OWNERSHIP_CHANGED"
assert publication[1]["binaryIsNext"] and not publication[1]["matchingSummaryExists"]
assert publication[1]["oldSummaryPreserved"] and not publication[1]["contractSatisfied"]


def slug(heading):
    heading = re.sub(r"\s+#+\s*$", "", heading).strip().lower()
    return re.sub(r"[^\w\- ]", "", heading, flags=re.UNICODE).replace(" ", "-")


links = 0
for path, update in index["administrative_updates"].items():
    base = git_data[update["base_git_ref"]]
    assert sha(base) == update["base_sha256"]
    offset, addition = update["offset"], update["inserted_text"].encode()
    actual = selected(path).read_bytes()
    assert actual == base[:offset] + addition + base[offset:], path
    assert sha(actual) == update["after_sha256"], path
    text = addition.decode("utf-8")
    assert b"\r" not in addition and addition.endswith(b"\n")
    assert all(line == line.rstrip(" \t") for line in text.splitlines())
    assert sum(line.startswith("```") for line in text.splitlines()) % 2 == 0
    for target in re.findall(r"\]\(([^)]+)\)", text):
        target = target.strip("<>")
        if re.match(r"[a-z]+://|mailto:", target):
            continue
        value, _, fragment = target.partition("#")
        relative = Path(path).parent / unquote(value) if value else Path(path)
        resolved = selected(relative)
        assert resolved.is_file(), (path, target)
        if fragment:
            headings = re.findall(r"^#{1,6}\s+(.+)$", resolved.read_text(), re.M)
            assert unquote(fragment) in {slug(h) for h in headings}, (path, target)
        links += 1

print(
    f"PASS rev1.1 appendix: {len(originals)} logical originals / {len(objects)} new objects / "
    f"{len(git_data)} fixed Git references; 4 exact diffs; 16 unchanged paths; "
    f"2 original exit-1 product failures / 6 owned identities / 2 adopted waits; "
    f"4 byte-preserving document additions / {links} new local links. "
    "Offline evidence only; no probe, product, process scan or resource execution."
)
