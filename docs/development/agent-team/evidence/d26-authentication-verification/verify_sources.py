"""Verify archived source locators using immutable Git objects; runs no product code."""
from pathlib import Path
import hashlib
import json
import subprocess

root = Path(__file__).resolve().parent
repo = root.parents[4]
cache = {}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git_bytes(locator):
    if locator not in cache:
        result = subprocess.run(
            ["git", "show", locator], cwd=repo, capture_output=True, check=True
        )
        cache[locator] = result.stdout
    return cache[locator]


author = json.loads((root / "source-locators.json").read_text())
author_count = 0
for snapshot in author["snapshots"]:
    for entry in snapshot["files"]:
        data = git_bytes(entry["git"]) if "git" in entry else (root / entry["blob"]).read_bytes()
        assert digest(data) == entry["sha256"], (snapshot["id"], entry["path"])
        author_count += 1

independent = json.loads((root / "independent-input-locators.json").read_text())
source_count = 0
derived = []
for entry in independent["files"]:
    if "derived_build" in entry:
        derived.append(entry["path"])
        continue
    data = git_bytes(entry["git"]) if "git" in entry else (root / entry["archived"]).read_bytes()
    assert digest(data) == entry["sha256"], entry["path"]
    source_count += 1

print(json.dumps({
    "author_snapshots": len(author["snapshots"]),
    "author_source_entries": author_count,
    "independent_source_entries": source_count,
    "dist_not_rebuilt": derived,
    "product_commands_run": False,
}))
