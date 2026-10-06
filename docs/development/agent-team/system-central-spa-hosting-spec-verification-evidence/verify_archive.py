#!/usr/bin/env python3
"""Verify immutable specification evidence and administrative insertions only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.parse import unquote

HERE = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--repo", type=Path, required=True)
parser.add_argument("--candidate-root", type=Path)
args = parser.parse_args()
repo = args.repo.resolve()
candidate = (args.candidate_root or repo).resolve()
index = json.loads((HERE / "index.json").read_text())
env = dict(os.environ, GIT_NO_LAZY_FETCH="1")

def sha(data):
    return hashlib.sha256(data).hexdigest()

def git(*argv):
    return subprocess.check_output(["git", "-C", str(repo), *argv], env=env)

def selected(path):
    value = candidate / path
    return value if value.is_file() else repo / path

def technical(data):
    match = re.search(br"^## 1\.", data, re.M)
    assert match, "missing technical heading"
    return data[match.start():]

def scope(data):
    return re.findall(r"^\| (\d+) \| `([^`]+)`", data.decode(), re.M)

git_bytes = {}
for key, item in index["git_references"].items():
    assert key == item["commit"] + ":" + item["path"]
    data = git("show", key)
    assert sha(data) == item["sha256"] and len(data) == item["bytes"], key
    assert git("rev-parse", key).decode().strip() == item["blob"], key
    git_bytes[key] = data
assert not git("diff", "--name-only", index["fixed_frontend_git"],
               index["fixed_design_git"], "--", "web")

originals = {}
new_objects = set()
for name, item in index["artifacts"].items():
    if item["storage"] == "object":
        data = (HERE / item["object"]).read_bytes()
        assert item["object"] == "objects/" + item["sha256"]
        new_objects.add(item["sha256"])
    elif item["storage"] == "git":
        data = git_bytes[item["git_ref"]]
    elif item["storage"] == "repository":
        data = (repo / item["path"]).read_bytes()
    else:
        raise AssertionError("unknown storage")
    assert sha(data) == item["sha256"] and len(data) == item["bytes"], name
    originals[name] = data
assert {p.name for p in (HERE / "objects").iterdir()} == new_objects
assert len(originals) == index["counts"]["logical_artifacts"] == 18
assert len(new_objects) == index["counts"]["new_objects"] == 16
assert sum((HERE / "objects" / h).stat().st_size for h in new_objects) == index["counts"]["new_object_bytes"]
assert len(git_bytes) == index["counts"]["git_references"] == 33

versions = [
    "author/d28-central-spa-hosting.draft.md",
    "author/d28-central-spa-hosting.rev0.1.draft.md",
    "author/d28-central-spa-hosting.rev0.2.draft.md",
    "author/d28-central-spa-hosting.md",
    "author/d28-central-spa-hosting.rev1-v2.md",
]
diffs = [
    "author/rev0-to-rev0.1.diff",
    "author/rev0.1-to-rev0.2.diff",
    "author/rev0.2-to-rev1-header.diff",
    "author/rev1-to-rev1-v2-header.diff",
]

def apply_original_diff(old, delta):
    source = old.splitlines(keepends=True)
    lines = delta.splitlines(keepends=True)
    output = []
    cursor = 0
    pos = 2
    assert lines[0].startswith(b"--- ") and lines[1].startswith(b"+++ ")
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
                assert source[cursor] == line[1:], "diff context changed"
                if line.startswith(b" "):
                    output.append(line[1:])
                cursor += 1
            elif line.startswith(b"+"):
                output.append(line[1:])
            else:
                raise AssertionError("unsupported original diff line")
            pos += 1
    output.extend(source[cursor:])
    return b"".join(output)

for i, name in enumerate(diffs):
    assert apply_original_diff(originals[versions[i]], originals[name]) == originals[versions[i + 1]]
final = originals[versions[-1]]
assert sha(final) == index["card_sha256"]
assert sha(technical(final)) == index["technical_sha256"]
assert technical(originals[versions[2]]) == technical(originals[versions[3]]) == technical(final)
expected_scope = [(str(i["number"]), i["path"]) for i in index["candidate_scope"]]
assert len(expected_scope) == len(set(p for _, p in expected_scope)) == 16
for name in versions:
    assert scope(originals[name]) == expected_scope, name
existing = set(git("ls-tree", "-r", "--name-only", index["fixed_design_git"],
                   "--", *[p for _, p in expected_scope]).decode().splitlines())
assert existing == {"internal/central/app/app.go"}
assert sha(technical(git("show", index["spec_commit"] + ":" + index["card_path"]))) == index["technical_sha256"]

reviews = [
    ("review", versions[0], "NEEDS_NARROW_CLARIFICATION", None),
    ("rev0.1-review", versions[1], "NEEDS_TAIL_LIFETIME_CLARIFICATION", diffs[0]),
    ("rev0.2-review", versions[2], "STATIC_PASS_BOUNDED_SPECIFICATION", diffs[1]),
]
for stem, draft, verdict, diff in reviews:
    review = json.loads(originals["independent/" + stem + ".json"])
    assert review["verdict"] == verdict
    assert review["report_sha256"] == sha(originals["independent/" + stem + ".md"])
    assert review["draft_sha256"] == sha(originals[draft])
    expected = review.get("draft_technical_sha256", review.get("technical_sha256"))
    assert expected == sha(technical(originals[draft]))
    if diff:
        assert review["diff_sha256"] == sha(originals[diff])

plan = json.loads(originals["independent-plan/basis.json"])
assert plan["status"] == "PLAN_ONLY_NO_PRODUCT_INPUT_OR_EXECUTION"
assert plan["plan"]["sha256"] == sha(originals["independent-plan/plan.md"])
assert plan["candidate_paths"] == [p for _, p in expected_scope]
assert plan["actual_dynamic_commands"] == [] and plan["dynamic_result"] == "NOT_RUN"
for item in plan["fixed_source_fingerprints"]:
    assert sha(git_bytes[item["git"] + ":" + item["path"]]) == item["sha256"]

texts = [(index["report_path"], (candidate / index["report_path"]).read_bytes())]
for path, update in index["administrative_updates"].items():
    base = git_bytes[update["base_git_ref"]]
    offset = update["offset"]
    inserted = update["inserted_text"].encode()
    actual = (candidate / path).read_bytes()
    assert actual == base[:offset] + inserted + base[offset:], path
    assert sha(actual) == update["after_sha256"], path
    texts.append((path, inserted))

def slug(heading):
    heading = re.sub(r"\s+#+\s*$", "", heading).strip().lower()
    heading = re.sub(r"[^\w\- ]", "", heading, flags=re.UNICODE)
    return heading.replace(" ", "-")

link_count = 0
for path, data in texts:
    text = data.decode("utf-8")
    assert b"\r" not in data and data.endswith(b"\n"), path
    assert all(line == line.rstrip(" \t") for line in text.splitlines()), path
    assert sum(line.startswith("```") for line in text.splitlines()) % 2 == 0, path
    for target in re.findall(r"\]\(([^)]+)\)", text):
        target = target.strip("<>")
        if re.match(r"[a-z]+://|mailto:", target):
            continue
        value, _, fragment = target.partition("#")
        relative = (Path(path).parent / unquote(value)) if value else Path(path)
        resolved = selected(relative)
        assert resolved.is_file(), (path, target)
        if fragment:
            headings = re.findall(r"^#{1,6}\s+(.+)$", resolved.read_text(), re.M)
            assert unquote(fragment) in {slug(h) for h in headings}, (path, target)
        link_count += 1

print("PASS: 18 logical originals; 16 new objects; 33 fixed Git references; "
      "4 exact diffs; 16 candidate paths; 5 byte-preserving administrative insertions; "
      + str(link_count) + " new local links. Offline archive checks only; no product execution.")
