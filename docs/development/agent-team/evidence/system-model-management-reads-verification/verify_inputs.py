#!/usr/bin/env python3
"""Verify archive bytes and reconstruct all five fixed inputs from Git."""

import hashlib
import json
from pathlib import Path
import subprocess


ROOT = Path(__file__).resolve().parent
COMMIT = "ecd733711caff5df46e423cadab52b32c34f785e"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    checked = 0
    for line in (ROOT / "SHA256SUMS").read_text().splitlines():
        expected, relative = line.split("  ", 1)
        assert sha((ROOT / relative).read_bytes()) == expected, relative
        checked += 1
    originals = json.loads((ROOT / "original-map.json").read_text())
    for relative, record in originals.items():
        data = (ROOT / relative).read_bytes()
        assert len(data) == record["bytes"] and sha(data) == record["sha256"], relative
    overrides = {}
    for item in json.loads((ROOT / "historical-overrides.json").read_text()):
        data = (ROOT / item["archive_path"]).read_bytes()
        assert sha(data) == item["sha256"], item["archive_path"]
        overrides[(item["repository_path"], item["sha256"])] = data
    git_sources = {}
    counts = {}
    for revision in range(1, 6):
        name = f"input{revision:02d}.json"
        manifest = json.loads((ROOT / "author" / name).read_text())
        for path, expected in manifest["paths"].items():
            if path not in git_sources:
                git_sources[path] = subprocess.check_output(
                    ["git", "show", f"{COMMIT}:{path}"], cwd=ROOT
                )
            data = overrides.get((path, expected), git_sources[path])
            assert sha(data) == expected, f"{name}: {path}"
        counts[name] = len(manifest["paths"])
    print(json.dumps({"archive_files": checked, "originals": len(originals),
                      "source_commit": COMMIT, "reconstructed_inputs": counts}))


if __name__ == "__main__":
    main()
