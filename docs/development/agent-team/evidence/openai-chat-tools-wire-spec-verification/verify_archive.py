#!/usr/bin/env python3
"""Verify archived source bytes and fixed Git locators; never execute the SDK."""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import subprocess

ARCHIVE = Path(__file__).resolve().parent
COMMIT = "becc1d20eed83c1b8d85e15dc131a372d9dc7813"


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def read_json(name):
    return json.loads((ARCHIVE / name).read_bytes())


def git(args, cwd):
    return subprocess.run(
        ["git", *args], cwd=cwd, check=True, capture_output=True, timeout=30
    ).stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--git-dir", type=Path, help="optional existing fixed SDK object store; never fetches")
    parser.add_argument("--restore-sources", type=Path, help="new task-owned directory outside the repository")
    args = parser.parse_args()
    repo = next(p for p in ARCHIVE.parents if (p / "AGENTS.md").is_file())
    sums_path = ARCHIVE / "SHA256SUMS.json"
    sums = read_json(sums_path.name) if sums_path.exists() else {}
    for path, expected in sums.items():
        assert digest((ARCHIVE / path).read_bytes()) == expected, path

    originals = read_json("original-map.json")["files"]
    for item in originals:
        raw = (ARCHIVE / item["stored_path"]).read_bytes()
        assert digest(raw) == item["stored_sha256"] and len(raw) == item["stored_bytes"]
        original = base64.b64decode(raw) if item["encoding"] == "base64" else raw
        assert digest(original) == item["original_sha256"] and len(original) == item["original_bytes"]

    sources = read_json("source-manifest.json")
    locations = read_json("source-files.json")
    assert sources["commit"] == locations["commit"] == COMMIT
    assert len(sources["files"]) == len(locations["files"]) == 21
    commit_raw = base64.b64decode((ARCHIVE / "commit-object.base64").read_bytes())
    assert digest(commit_raw) == sources["commit_object_sha256"]
    assert hashlib.sha1(b"commit " + str(len(commit_raw)).encode() + b"\0" + commit_raw).hexdigest() == COMMIT
    prefix = ["--git-dir=" + str(args.git_dir.resolve())] if args.git_dir else None
    if prefix:
        assert git([*prefix, "cat-file", "commit", COMMIT], repo) == commit_raw

    restored = {}
    manifest = {x["path"]: x for x in sources["files"]}
    for entry in locations["files"]:
        path = entry["source_path"]
        assert path == "LICENSE" or path.startswith("src/openai/")
        assert not Path(path).is_absolute() and ".." not in Path(path).parts
        raw = (ARCHIVE / entry["stored_path"]).read_bytes()
        item = manifest[path]
        assert digest(raw) == item["sha256"] == entry["sha256"]
        assert len(raw) == item["bytes"] == entry["bytes"]
        blob = hashlib.sha1(b"blob " + str(len(raw)).encode() + b"\0" + raw).hexdigest()
        assert blob == item["git_blob"] == entry["git_blob"]
        assert item["url"] == f"https://github.com/openai/openai-python/blob/{COMMIT}/{path}"
        if prefix:
            assert git([*prefix, "rev-parse", COMMIT + ":" + path], repo).decode().strip() == blob
            assert git([*prefix, "cat-file", "blob", COMMIT + ":" + path], repo) == raw
        restored[path] = raw
    assert sum(map(len, restored.values())) == 104254

    fields = read_json("field-manifest.json")
    assert fields["commit"] == COMMIT
    assert fields["source_manifest_sha256"] == digest((ARCHIVE / "source-manifest.json").read_bytes())
    for item in fields["entries"]:
        raw = restored[item["path"]]
        assert digest(raw) == item["sha256"]
        lines = raw.decode().splitlines()
        assert all(lines[d["line"] - 1] == d["text"] for d in item["declarations"])

    inputs = read_json("baseline-inputs.json")
    for item in inputs["paths"]:
        assert digest(git(["show", inputs["baseline"] + ":" + item["path"]], repo)) == item["sha256"]
    acceptance = read_json("acceptance.json")
    card = git(["show", acceptance["accepted_commit"] + ":" + acceptance["card_path"]], repo)
    assert card == (ARCHIVE / "inputs/adopted.md.txt").read_bytes()
    assert digest(card) == acceptance["accepted_card_sha256"]
    technical = (ARCHIVE / "inputs/c0-02.md.txt").read_bytes()
    assert digest(technical) == acceptance["reviewed_technical_sha256"]
    assert card.split(b"## 1.", 1)[1] == technical.split(b"## 1.", 1)[1]

    if args.restore_sources:
        destination = args.restore_sources.resolve()
        assert not destination.exists() and not destination.is_relative_to(repo)
        destination.mkdir(parents=True)
        for path, raw in restored.items():
            target = destination / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(raw)
    print(json.dumps({
        "archive_hashes": len(sums), "original_records": len(originals),
        "official_files": len(restored), "official_bytes": sum(map(len, restored.values())),
        "baseline_paths": len(inputs["paths"]), "accepted_commit": acceptance["accepted_commit"],
        "technical_sections_unchanged": True, "sdk_tree_checked": bool(prefix),
        "sources_restored": bool(args.restore_sources), "network": False,
        "sdk_executed": False, "product_tests_run": False,
    }))


if __name__ == "__main__":
    main()
