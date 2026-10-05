#!/usr/bin/env python3
"""Check archived bytes and reconstruct a small fixed overlay; never run tests."""

import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--out", type=Path)
    parser.add_argument("--variant", choices=["accepted", "candidate01", "candidate02", "production01", "pure01", "independent01", "independent02"], default="accepted")
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    accepted = json.loads((here / "accepted-input.json").read_text())
    mapping = json.loads((here / "source-map.json").read_text())

    def git_bytes(commit, path):
        result = subprocess.run(["git", "show", commit + ":" + path], cwd=args.repo, capture_output=True, check=True)
        return result.stdout

    for path, row in mapping["raw_files"].items():
        data = (here / path).read_bytes()
        assert sha(data) == row["sha256"] and len(data) == row["bytes"], path
    for row in mapping["omitted_duplicate_sources"]:
        data = git_bytes(row["git_commit"], row["git_path"])
        assert sha(data) == row["sha256"] and len(data) == row["bytes"], row["original_path"]
    index_path = here / "SHA256SUMS.json"
    if index_path.exists():
        for path, row in json.loads(index_path.read_text()).items():
            data = (here / path).read_bytes()
            assert sha(data) == row["sha256"] and len(data) == row["bytes"], path

    variants = {}
    variants["accepted"] = accepted["paths"]
    for name, directory in [("candidate01", "candidate-freeze-01"), ("candidate02", "candidate-freeze-02"), ("production01", "production-review-01")]:
        variants[name] = json.loads((here / "author" / directory / "manifest.json").read_text())["paths"]
    variants["pure01"] = [row for row in json.loads((here / "author/logs/pure-01.json").read_text())["inputs"] if row["sha256"] is not None]
    for version in ["01", "02"]:
        probe = json.loads((here / "verification" / ("probe-freeze-" + version) / "manifest.json").read_text())
        variants["independent" + version] = probe["production"] + [probe["independent_probe"]]

    checked = {}
    selected = None
    for name, rows in variants.items():
        files = {}
        for row in rows:
            path = row["path"]
            if name.startswith("independent") and path == "tests/process/model_root_independent_test.go":
                data = (here / "verification" / ("probe-freeze-" + name[-2:]) / "model_root_independent_test.go.txt").read_bytes()
            elif name == "candidate01" and path in {"internal/central/app/model_process_test.go", "tests/process/model_system_database_test.go"}:
                data = (here / "author/candidate-freeze-01" / (path + ".txt")).read_bytes()
            else:
                commit = accepted["baseline"] if name in {"candidate01", "candidate02"} and path in {"AGENTS.md", "docs/development/backend/README.md"} else accepted["accepted_commit"]
                data = git_bytes(commit, path)
            assert sha(data) == row["sha256"], (name, path)
            files[path] = data
        checked[name] = len(files)
        if name == args.variant:
            selected = files
    if args.out is not None:
        assert not args.out.exists(), "output directory must not exist"
        args.out.mkdir(parents=True)
        for path, data in selected.items():
            dest = args.out / path
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(data)
    print(json.dumps({"raw_files_verified": len(mapping["raw_files"]), "git_source_locators_verified": len(mapping["omitted_duplicate_sources"]), "variant_path_counts": checked, "output_variant": args.variant if args.out else None, "no_go_docker_network": True}))


if __name__ == "__main__":
    main()
