#!/usr/bin/env python3
"""Verify fixed D26 specification evidence; optionally restore it outside the repo."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    if not args.check and args.output is None:
        parser.error("use --check or --output NEW_PRIVATE_DIRECTORY")
    evidence = Path(__file__).resolve().parent
    repo = evidence.parents[4]
    index = json.loads((evidence / "SHA256SUMS.json").read_text())
    provenance = json.loads((evidence / "provenance.json").read_text())
    for entry in index["files"]:
        path = evidence / entry["path"]
        data = path.read_bytes()
        assert digest(data) == entry["sha256"], entry["path"]
        assert len(data) == entry["bytes"], entry["path"]
    for entry in provenance["raw"]:
        data = (evidence / entry["path"]).read_bytes()
        assert digest(data) == entry["sha256"], entry["path"]

    def git_bytes(commit, path):
        return subprocess.run(
            ["git", "show", commit + ":" + path],
            cwd=repo,
            check=True,
            capture_output=True,
        ).stdout

    git_materials = []
    for entry in provenance["git_materials"]:
        data = git_bytes(entry["commit"], entry["path"])
        assert digest(data) == entry["sha256"], entry["output"]
        assert len(data) == entry["bytes"], entry["output"]
        git_materials.append((entry, data))
    inputs = json.loads((evidence / "rev1/independent/inputs.json").read_text())
    for entry in inputs["files"]:
        assert digest(git_bytes(inputs["base"], entry["path"])) == entry["sha256"], entry["path"]
    if args.output is not None:
        target = args.output.resolve()
        if target.exists() or target == repo or repo in target.parents:
            parser.error("output must be a new private directory outside the repo")
        target.mkdir(parents=True)
        for entry in provenance["raw"]:
            destination = target / "archive" / entry["path"]
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes((evidence / entry["path"]).read_bytes())
        for entry, data in git_materials:
            destination = target / entry["output"]
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(data)
    print(json.dumps({
        "result": "PASS",
        "indexed_files": len(index["files"]),
        "raw_files": len(provenance["raw"]),
        "git_materials": len(git_materials),
        "fixed_selected_sources": len(inputs["files"]),
        "output": str(args.output.resolve()) if args.output is not None else None,
        "scope": "bytes and local Git only; no validators or product code executed",
    }, indent=2))


if __name__ == "__main__":
    main()
