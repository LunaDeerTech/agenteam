#!/usr/bin/env python3
"""Check saved specification bytes and fixed Git only; execute no archived code."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
INDEX = json.loads((HERE / "index.json").read_bytes())
BASE = "628612cdfc730cc1d89cad4e24a5d20f36cc6812"
SPEC = "8bdfb006b32fbbc8889a190d7829f05e93e29787"
TECH = "bee4f7ddb2b480b8c57b7d1b6b81be36bfce9f726222ebc10c8b468b90a54cda"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def body(data):
    return data[re.search(rb"^## 1\.", data, re.M).start():]


assert INDEX["base"] == BASE and INDEX["spec_commit"] == SPEC
assert INDEX["technical_sha256"] == TECH
objects = {}
for value, meta in INDEX["objects"].items():
    assert re.fullmatch(r"[0-9a-f]{64}", value)
    assert meta["path"] == "objects/" + value
    path = HERE / meta["path"]
    assert not path.is_symlink()
    data = path.read_bytes()
    assert sha(data) == value and len(data) == meta["bytes"]
    objects[value] = data
assert {p.name for p in (HERE / "objects").iterdir()} == set(objects)
assert {v["sha256"] for v in INDEX["artifacts"].values() if "object" in v} == set(objects)
git = {}
requests = [(BASE, p) for p in INDEX["git_sources"]] + [(SPEC, INDEX["card"])]
env = dict(os.environ, GIT_NO_LAZY_FETCH="1", GIT_OPTIONAL_LOCKS="0")
result = subprocess.run(
    ["git", "cat-file", "--batch"], cwd=ROOT, env=env, check=True,
    input="".join(f"{c}:{p}\n" for c, p in requests).encode(),
    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
).stdout
offset = 0
for commit, path in requests:
    end = result.index(b"\n", offset)
    header = result[offset:end].split()
    assert len(header) == 3 and header[1] == b"blob", (commit, path)
    size = int(header[2])
    data = result[end + 1:end + 1 + size]
    assert result[end + size + 1:end + size + 2] == b"\n"
    offset = end + size + 2
    if commit == BASE:
        meta = INDEX["git_sources"][path]
        assert sha(data) == meta["sha256"] and header[0].decode() == meta["git_blob"]
    git[(commit, path)] = data
assert offset == len(result)


def blob(name):
    meta = INDEX["artifacts"][name]
    if "git" in meta:
        return git[(meta["git"]["commit"], meta["git"]["path"])]
    return objects[meta["sha256"]]


def item(name):
    return json.loads(blob(name))


for name, meta in INDEX["artifacts"].items():
    data = blob(name)
    assert sha(data) == meta["sha256"] and len(data) == meta["bytes"], name
frozen = item("author/frozen.json")
for name, meta in frozen["artifacts"].items():
    assert sha(blob("author/" + name)) == meta["sha256"]
review = item("independent/review.json")
assert review["conclusion"] == "STATIC_PASS" and review["blocking_findings"] == []
assert review["base"] == BASE and review["candidate_count"] == 17
assert review["dynamic_product_tests"] is False and review["resources_started"] is False
assert sha(blob("independent/review.md")) == "1f0a5faca177f46a769615011a6758d2f3f3b74a7a8e6a5adb7ea8cf1c653b65"
assert sha(blob("independent/review.json")) == "0bc66903f7adec96143518822c498fa9a640c709af89761563b98cb987d3fba1"
draft = blob("author/d27-system-smtp-delivery-ui.draft.md")
assert sha(draft) == frozen["full_sha256"] == review["full_sha256"]
for data in [draft, blob("formal-rev1/d27-system-smtp-delivery-ui.md"),
             blob("formal-rev1-v2/d27-system-smtp-delivery-ui.md"),
             (ROOT / INDEX["card"]).read_bytes()]:
    assert sha(body(data)) == TECH
v2 = item("formal-rev1-v2/frozen.json")
assert blob("formal-rev1/d27-system-smtp-delivery-ui.md").replace(
    v2["only_removed_clause"].encode(), b"", 1
) == blob("formal-rev1-v2/d27-system-smtp-delivery-ui.md")
assert sha(git[(SPEC, INDEX["card"])]) == v2["full_sha256"]
scope = item("author/scope17.json")["files"]
assert len(scope) == len({x["path"] for x in scope}) == 17
assert [x["index"] for x in scope] == list(range(1, 18))
assert sum(x["new"] for x in scope) == 10
for row in scope:
    if not row["new"]:
        assert sha(git[(BASE, row["path"])]) == row["base_sha256"]
static = item("independent/static-input01.json")
author = item("author/fixed-inputs.json")["files"]
extra = static["additional_five_code_and_postread_sources"]
assert len(author) == 43 and len(extra) == 6 and len(author.keys() | extra.keys()) == 47
assert set(INDEX["git_sources"]) == author.keys() | extra.keys()
for mapping in [author, extra]:
    for path, meta in mapping.items():
        assert meta["sha256"] == sha(git[(BASE, path)])
assert sum(len(v) for v in static["old_exact_selectors"].values()) == 5
check = item("independent/static-check01.json")
assert check["exit"] == 0 and check["actual_wait"] is True and check["seconds"] == 0.284
assert check["resources_started"] is False
assert check["script_sha256"] == sha(blob("independent/verify-static-input01.py"))
assert check["raw_sha256"] == sha(blob("independent/static-check01.raw"))
assert item("independent/static-check01.raw")["checks"] == "PASS"
assert item("formal-rev1/precheck01/result.json")["exit_code"] == 1
plan = item("independent-plan/plan-input01.json")
assert plan["product_base"] == BASE and plan["card_commit"] == SPEC
assert plan["technical_sha256"] == TECH and plan["candidate_paths"] == 17
assert plan["card_sha256"] == sha(git[(SPEC, INDEX["card"])])
assert sha(blob("independent-plan/plan01.md")) == "016cf031d76f5420bfa10bae7a52010acfea25cb6c0d405f6a97fb3c19dddb48"
assert INDEX["counts"]["logical_artifacts"] == len(INDEX["artifacts"])
assert INDEX["counts"]["objects"] == len(objects)
print(json.dumps({
    "status": "PASS: archived bytes/fixed Git only; specification, not product acceptance",
    "logical_artifacts": len(INDEX["artifacts"]), "objects": len(objects),
    "logical_git_source_references": 49, "unique_git_sources": 47,
    "accepted_card_git": 1, "candidate_scope": 17, "old_selectors_not_executed": 5,
    "original_static_actual_exit": 0, "original_static_seconds": 0.284,
    "product_tests_executed": False, "resources_started": False,
}, ensure_ascii=False))
