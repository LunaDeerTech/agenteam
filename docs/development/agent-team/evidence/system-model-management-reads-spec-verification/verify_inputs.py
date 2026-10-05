#!/usr/bin/env python3
"""Read-only checks for the management-read specification archive."""

import argparse
import difflib
import hashlib
import json
from pathlib import Path
import subprocess


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, required=True)
    args = parser.parse_args()
    repo = args.repo.resolve()
    here = Path(__file__).resolve().parent
    mapping = json.loads((here / "original-map.json").read_text())
    for entry in mapping["raw_files"] + mapping["byte_identical_aliases"]:
        raw = (here / entry["archive"]).read_bytes()
        assert digest(raw) == entry["sha256"], entry["archive"]
        if "bytes" in entry:
            assert len(raw) == entry["bytes"], entry["archive"]
    by_origin = {
        entry["source"]: entry
        for entry in mapping["raw_files"] + mapping["byte_identical_aliases"]
    }
    review = json.loads((here / "review/index.json").read_text())
    for entry in review["files"]:
        origin = str(Path(review["root"]) / entry["path"])
        assert by_origin[origin]["sha256"] == entry["sha256"], origin
    plan = json.loads((here / "plan/index.json").read_text())
    assert digest((here / "plan/plan.md").read_bytes()) == plan["plan"]["sha256"]
    prior = plan["prior_review"]
    assert by_origin[prior["path"]]["sha256"] == prior["sha256"]
    assert by_origin[prior["index"]]["sha256"] == prior["index_sha256"]
    assert by_origin[plan["inputs"]["path"]]["sha256"] == plan["inputs"]["sha256"]

    sources = json.loads((here / "spec/inputs.json").read_text())["inputs"]
    extra = json.loads((here / "review/review-checks.json").read_text())[
        "additional_fixed_sources"
    ]
    for entry in sources + extra:
        result = subprocess.run(
            ["git", "cat-file", "blob", entry["git"]],
            cwd=repo,
            capture_output=True,
            check=True,
        )
        assert digest(result.stdout) == entry["sha256"], entry["path"]
        assert len(result.stdout) == entry["bytes"], entry["path"]
        if "blob" in entry:
            header = ("blob " + str(len(result.stdout)) + "\0").encode()
            assert hashlib.sha1(header + result.stdout).hexdigest() == entry["blob"]

    accepted = mapping["accepted_card"]
    raw = subprocess.run(
        ["git", "cat-file", "blob", accepted["git"]],
        cwd=repo,
        capture_output=True,
        check=True,
    ).stdout
    assert raw == (here / accepted["archive"]).read_bytes()
    assert digest(raw) == plan["accepted_card_sha256"]

    def technical(raw):
        return raw.split(b"## 1. ", 1)[1].split(b"## 10. ", 1)[0]

    original = (here / "spec/rev1.md").read_bytes()
    implementation = (here / "acceptance/implementation-status.md").read_bytes()
    assert technical(original) == technical(raw) == technical(implementation)
    for old, new, patch in [
        ("spec/rev1.md", "acceptance/card.md", "acceptance/rev1-to-accepted.patch"),
        (
            "acceptance/card.md",
            "acceptance/implementation-status.md",
            "acceptance/accepted-to-implementation.patch",
        ),
    ]:
        expected = "".join(
            difflib.unified_diff(
                (here / old).read_text().splitlines(True),
                (here / new).read_text().splitlines(True),
                fromfile=old,
                tofile=new,
                n=0,
            )
        ).encode()
        assert (here / patch).read_bytes() == expected, patch

    checksums = here / "SHA256SUMS"
    indexed = []
    if checksums.exists():
        for line in checksums.read_text().splitlines():
            expected, name = line.split("  ", 1)
            assert digest((here / name).read_bytes()) == expected, name
            indexed.append(name)
        actual = sorted(
            str(path.relative_to(here))
            for path in here.rglob("*")
            if path.is_file() and path != checksums
        )
        assert sorted(indexed) == actual
    print(
        json.dumps(
            {
                "result": "PASS",
                "raw_files": len(mapping["raw_files"]),
                "aliases": len(mapping["byte_identical_aliases"]),
                "git_sources": len(sources) + len(extra),
                "accepted_card_matches_git": True,
                "technical_sections_1_to_9_identical": True,
                "derived_administrative_patches": 2,
                "checksum_files": len(indexed),
                "business_tests_run": False,
            },
            ensure_ascii=False,
        )
    )


if __name__ == "__main__":
    main()
