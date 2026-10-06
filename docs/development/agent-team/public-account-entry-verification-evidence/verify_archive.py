#!/usr/bin/env python3
"""Read-only archival hashes: no business imports, subprocesses or network."""
import argparse
import hashlib
import json
from pathlib import Path


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate-root", type=Path)
    args = parser.parse_args()
    base = Path(__file__).resolve().parent

    def file(name):
        path = (base / name).resolve()
        if not path.is_relative_to(base) or not path.is_file():
            raise ValueError("invalid archive path: " + name)
        return path

    def read(name):
        return json.loads(file(name).read_text(encoding="utf-8"))

    sums = read("SHA256SUMS.json")
    actual = {str(p.relative_to(base)) for p in base.rglob("*") if p.is_file()}
    if actual != set(sums["files"]) | {"SHA256SUMS.json"}:
        raise ValueError("archive file set changed")
    for name, expected in sums["files"].items():
        path = file(name)
        if sha(path) != expected["sha256"] or path.stat().st_size != expected["bytes"]:
            raise ValueError("archive hash mismatch: " + name)
    report = base.parent / "public-account-entry-verification.md"
    if sha(report) != sums["report_sha256"]:
        raise ValueError("report hash mismatch")

    originals = read("original-map.json")["files"]
    for name, row in originals.items():
        path = file(row["archive"])
        if sha(path) != row["sha256"] or path.stat().st_size != row["bytes"]:
            raise ValueError("original bytes changed: " + name)

    def original(name):
        return json.loads(file(originals[name]["archive"]).read_text(encoding="utf-8"))

    candidate = read("candidate-files.json")
    frozen = original(candidate["input_manifest"])
    if originals[candidate["input_manifest"]]["sha256"] != candidate["input_sha256"]:
        raise ValueError("candidate input pointer changed")
    if len(candidate["paths"]) != 25 or len(frozen["files"]) != 25:
        raise ValueError("candidate path count changed")
    seen = set()
    for row in candidate["paths"]:
        name = row["path"]
        if name in seen or frozen["files"].get(name) != row["sha256"] or sha(file(row["archive"])) != row["sha256"]:
            raise ValueError("candidate mismatch: " + name)
        seen.add(name)
        if args.candidate_root:
            root = args.candidate_root.resolve()
            path = (root / name).resolve()
            if not path.is_relative_to(root) or sha(path) != row["sha256"]:
                raise ValueError("existing candidate differs: " + name)
    mapped = 0
    history = read("history-reconstruction.json")
    for name, entry in history.items():
        source = original(entry["manifest_logical"])
        paths = source.get("files", source.get("paths"))
        if isinstance(paths, list):
            paths = {row["path"]: row["sha256"] for row in paths}
        if set(paths) != set(entry["paths"]):
            raise ValueError("historical path set differs: " + name)
        for path, row in entry["paths"].items():
            if paths[path] != row["sha256"] or sha(file(row["archive"])) != row["sha256"]:
                raise ValueError("historical bytes differ: " + name + ": " + path)
            mapped += 1
    dist = read("dist-hashes.json")
    if dist["copied_dist_bytes"] or len(dist["files"]) != 15:
        raise ValueError("dist archival scope changed")
    original08 = original("author/input08-manifest.json")
    for path, digest in dist["files"].items():
        if frozen["fixed_dependencies_and_dist"][path] != digest or original08["fixed_dependencies_and_dist"][path] != digest:
            raise ValueError("dist reuse mismatch: " + path)
    state = read("status.json")
    print(json.dumps({
        "result": "PASS: archive hashes only",
        "archive_files": len(sums["files"]),
        "logical_originals": len(originals),
        "candidate_paths": len(seen),
        "historical_inputs": len(history),
        "historical_path_mappings": mapped,
        "dist_hashes_only": 15,
        "candidate_checkout_checked": bool(args.candidate_root),
        "recorded_status": state["status"],
        "business_or_archived_scripts_executed": False,
    }, indent=2))


if __name__ == "__main__":
    main()
