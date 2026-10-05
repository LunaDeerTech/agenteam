#!/usr/bin/env python3
"""Recover and verify fixed public source bytes; never import or run the SDK."""

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess


COMMIT = "becc1d20eed83c1b8d85e15dc131a372d9dc7813"
REPOSITORY = "https://github.com/openai/openai-python.git"
ARCHIVE = Path(__file__).resolve().parent


def digest(body):
    return hashlib.sha256(body).hexdigest()


def git(args):
    return subprocess.run(
        ["git", "-c", "credential.helper=", "-c", "http.extraHeader=", *args],
        check=True,
        capture_output=True,
        env=dict(os.environ, GIT_TERMINAL_PROMPT="0"),
        timeout=45,
    ).stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("destination", type=Path, help="new task-owned directory outside the work repository")
    parser.add_argument("--git-dir", type=Path, help="existing exact object store; omit only with public-network authorization")
    args = parser.parse_args()
    destination = args.destination.resolve()
    repository_root = next((p for p in ARCHIVE.parents if (p / "AGENTS.md").is_file()), None)
    if repository_root and destination.is_relative_to(repository_root):
        parser.error("destination must be outside the work repository")
    if destination.exists():
        parser.error("destination must not already exist")

    source = json.loads((ARCHIVE / "source-manifest.json").read_text())
    excerpts = json.loads((ARCHIVE / "excerpt-manifest.json").read_text())
    assert source["commit"] == excerpts["commit"] == COMMIT
    assert digest((ARCHIVE / "source-manifest.json").read_bytes()) == excerpts["source_manifest_sha256"]
    assert len(source["files"]) == len(excerpts["excerpts"]) == 17
    destination.mkdir(parents=True)
    objects = args.git_dir.resolve() if args.git_dir else destination / "objects.git"
    if args.git_dir is None:
        git(["init", "--bare", str(objects)])
        git(["--git-dir=" + str(objects), "fetch", "--no-tags", "--depth=1", REPOSITORY, COMMIT])
    prefix = ["--git-dir=" + str(objects)]
    commit_bytes = git([*prefix, "cat-file", "commit", COMMIT])
    assert hashlib.sha1(b"commit " + str(len(commit_bytes)).encode() + b"\0" + commit_bytes).hexdigest() == COMMIT
    assert digest(commit_bytes) == source["commit_object_sha256"]
    assert commit_bytes == base64.b64decode((ARCHIVE / "commit-object.base64").read_bytes())

    restored = {}
    for entry in source["files"]:
        path = entry["path"]
        assert path.startswith("src/openai/") and ".." not in Path(path).parts
        body = git([*prefix, "show", COMMIT + ":" + path])
        assert digest(body) == entry["sha256"]
        assert hashlib.sha1(b"blob " + str(len(body)).encode() + b"\0" + body).hexdigest() == entry["git_blob"]
        target = destination / "source" / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(body)
        restored[path] = body

    for entry in excerpts["excerpts"]:
        path = entry["source_path"]
        body = restored[path]
        assert digest(body) == entry["full_file_sha256"]
        lines = body.splitlines(keepends=True)
        excerpt = (
            f"# Evidence excerpt of {path}\n"
            f"# Exact official commit: {COMMIT}\n"
            "# Added evidence headings only; each identified source range is verbatim.\n"
        ).encode()
        for part in entry["ranges"]:
            first, last = part["first_line"], part["last_line"]
            selected = b"".join(lines[first - 1 : last])
            assert digest(selected) == part["sha256"]
            excerpt += f"\n# Original lines {first}-{last} (verbatim):\n".encode() + selected
        excerpt += b"# End of evidence excerpt.\n"
        assert digest(excerpt) == entry["stored_sha256"]
        assert excerpt == (ARCHIVE / entry["stored_path"]).read_bytes()

    license_entry = excerpts["license"]
    license_bytes = git([*prefix, "show", COMMIT + ":LICENSE"])
    assert digest(license_bytes) == license_entry["sha256"]
    assert hashlib.sha1(b"blob " + str(len(license_bytes)).encode() + b"\0" + license_bytes).hexdigest() == license_entry["git_blob"]
    assert license_bytes == (ARCHIVE / license_entry["stored_path"]).read_bytes()
    (destination / "LICENSE.openai").write_bytes(license_bytes)
    result = {
        "commit": COMMIT,
        "source_files_verified": len(restored),
        "excerpt_files_verified": len(excerpts["excerpts"]),
        "license_verified": True,
        "network_fetch": args.git_dir is None,
        "sdk_executed": False,
    }
    (destination / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
