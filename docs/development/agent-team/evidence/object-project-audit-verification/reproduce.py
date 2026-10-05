#!/usr/bin/env python3
"""Rebuild fixed inputs without running Go, Docker, or changing the repository."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    evidence = Path(__file__).resolve().parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True,
                        help="new, empty task-owned directory; must not exist")
    parser.add_argument("--revision", choices=["final", "new2", "new1", "late-get-red2"],
                        default="final")
    parser.add_argument("--sources-only", action="store_true",
                        help="reconstruct only the 13 reviewed paths for hash verification")
    args = parser.parse_args()
    repo = args.repo.resolve(strict=True)
    out = args.output.resolve()
    if out.exists():
        parser.error("--output already exists")
    if out == repo or repo in out.parents:
        parser.error("--output must be outside the repository")
    for line in (evidence / "SHA256SUMS").read_text().splitlines():
        expected, name = line.split("  ", 1)
        path = (evidence / name).resolve()
        if evidence not in path.parents or sha(path) != expected:
            raise SystemExit("evidence hash mismatch: " + name)
    manifest = json.loads((evidence / "final-input.json").read_text())
    out.mkdir(parents=True)
    tree = out / "tree"
    tree.mkdir()
    if args.sources_only:
        base_paths = set(subprocess.check_output(
            ["git", "-C", str(repo), "ls-tree", "-r", "--name-only", manifest["base"]],
            text=True).splitlines())
        for name in manifest["files"]:
            if name not in base_paths:
                continue
            target = tree / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(subprocess.check_output(
                ["git", "-C", str(repo), "show", manifest["base"] + ":" + name]))
    else:
        # Only source/build inputs needed by the original fixture, no web/cache/docs.
        archived = subprocess.Popen(
            ["git", "-C", str(repo), "archive", manifest["base"],
             "go.mod", "go.sum", "internal", "cmd", "tests", "scripts", "db", "deploy"],
            stdout=subprocess.PIPE)
        extracted = subprocess.run(["tar", "-xf", "-", "-C", str(tree)],
                                   stdin=archived.stdout)
        archived.stdout.close()
        if archived.wait() or extracted.returncode:
            raise SystemExit("fixed base archive failed")

    def patch(name, reverse=False):
        argv = ["patch", "--batch", "-p1", "-i", str(evidence / name)]
        if reverse:
            argv.append("--reverse")
        subprocess.run(argv, cwd=tree, check=True, stdout=subprocess.DEVNULL)

    patch("final-source.patch")
    expected = manifest["files"]
    if args.revision in ("new2", "new1", "late-get-red2"):
        patch("author/inputs/final-vs-new2.patch", reverse=True)
        expected = json.loads((evidence / "author/inputs/integration2-input.json").read_text())
    if args.revision in ("new1", "late-get-red2"):
        patch("author/inputs/new2-test-delta.patch", reverse=True)
        expected = json.loads((evidence / "author/inputs/integration1-input.json").read_text())
    if args.revision == "late-get-red2":
        patch("reviews/get-complete-delta.patch", reverse=True)
        expected = json.loads((evidence / "author/inputs/late-get-red2-input.json").read_text())
    for name, digest in expected.items():
        if sha(tree / name) != digest:
            raise SystemExit("source hash mismatch: " + name)
    probe = None
    if args.revision == "final":
        probe = "tests/objects/object_audit_independent_test.go"
        shutil.copy2(evidence / "independent-probe.go.txt", tree / probe)
    for name in ("gocache", "runtime", "docker-config"):
        (out / name).mkdir(mode=0o700)
    result = {"base": manifest["base"], "card": manifest["card"],
              "revision": args.revision, "sources_only": args.sources_only,
              "tree": str(tree), "matched_files": len(expected),
              "probe_sha256": sha(tree / probe) if probe else None,
              "executed_go_or_docker": False}
    (out / "reconstruction.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
