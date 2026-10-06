#!/usr/bin/env python3
"""Read-only hashes; never runs archived scripts, tests, subprocesses or network."""
import argparse
import hashlib
import json
from pathlib import Path


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate-root", type=Path)
    args = parser.parse_args()
    base = Path(__file__).resolve().parent

    def local(name):
        path = (base / name).resolve()
        if not path.is_relative_to(base) or not path.is_file():
            raise ValueError("invalid archive path: " + name)
        return path

    def read(name):
        return json.loads(local(name).read_text(encoding="utf-8"))

    index = read("SHA256SUMS.json")
    listed = set(index["files"])
    actual = {str(p.relative_to(base)) for p in base.rglob("*") if p.is_file()}
    if actual != listed | {"SHA256SUMS.json"}:
        raise ValueError("archive file set changed")
    for name, item in index["files"].items():
        path = local(name)
        if path.stat().st_size != item["bytes"] or sha256(path) != item["sha256"]:
            raise ValueError("archive hash mismatch: " + name)
    report = base.parent / "openai-chat-tools-wire-verification.md"
    if sha256(report) != index["report_sha256"]:
        raise ValueError("report hash mismatch")
    for item in read("original-map.json"):
        if sha256(local(item["archive"])) != item["sha256"]:
            raise ValueError("original byte mismatch: " + item["archive"])

    frozen = read("author/input08.json")
    candidate = read("candidate-files.json")
    if len(candidate["paths"]) != 14 or len(frozen["paths"]) != 14:
        raise ValueError("candidate path count changed")
    found = set()
    for item in candidate["paths"]:
        name = item["path"]
        if name in found or frozen["paths"].get(name) != item["sha256"]:
            raise ValueError("candidate manifest mismatch: " + name)
        found.add(name)
        if sha256(local(item["archive"])) != item["sha256"]:
            raise ValueError("candidate byte mismatch: " + name)
        if args.candidate_root:
            root = args.candidate_root.resolve()
            path = (root / name).resolve()
            if not path.is_relative_to(root) or sha256(path) != item["sha256"]:
                raise ValueError("candidate checkout mismatch: " + name)
    if candidate["input08_sha256"] != sha256(local("author/input08.json")):
        raise ValueError("candidate input pointer mismatch")

    history = read("history/reconstruction.json")["inputs"]
    historical_paths = 0
    for name, mapping in history.items():
        original = read("author/" + name)
        original = original.get("paths", original)
        if set(original) != set(mapping):
            raise ValueError("history path set mismatch: " + name)
        for path, item in mapping.items():
            if original[path] != item["sha256"] or sha256(local(item["archive"])) != item["sha256"]:
                raise ValueError("history byte mismatch: " + name + ": " + path)
            historical_paths += 1

    state = read("status.json")
    if state["decision"] != "BLOCKED" or state["independent_dynamic"] != "NOT RUN" or state["product_accepted"]:
        raise ValueError("limited result status changed")
    if read("author/real01/command.json")["exit"] != 1 or read("author/real02/command.json")["exit"] != 0:
        raise ValueError("author original exits changed")
    for run in ["real01", "real02"]:
        cleanups = read("author/" + run + "/cleanup.json")
        if len(cleanups) != 2:
            raise ValueError("missing double cleanup")
        for row in cleanups:
            absent = row["exact_absent"]
            if len(absent) != 7 or not all(v["absent"] for v in absent.values()):
                raise ValueError("exact resource cleanup mismatch")
            if not row["baseline_unchanged"] or row["remaining_new"] or row["owned_processes"] or row["runtime_entries"]:
                raise ValueError("resource cleanup incomplete")
    blocked = read("resources/docker-window-tools-independent-01.json")
    if blocked["actual_driver_started"] or blocked["created_resources"] or not blocked["retry_forbidden"]:
        raise ValueError("independent blocked boundary changed")

    print(json.dumps({
        "result": "PASS: archive hashes and recorded limited boundaries only",
        "archive_files": len(listed),
        "original_records": len(read("original-map.json")),
        "candidate_paths": len(found),
        "historical_inputs": len(history),
        "historical_path_mappings": historical_paths,
        "candidate_checkout_checked": bool(args.candidate_root),
        "independent_dynamic": "NOT RUN",
        "go_docker_sql_network_executed": False,
    }, indent=2))


if __name__ == "__main__":
    main()
