#!/usr/bin/env python3
"""Rebuild fixed S2 source inputs without Go, Docker, network or Git writes."""

import argparse
import hashlib
import io
import json
import pathlib
import subprocess
import tarfile
import tempfile

evidence = pathlib.Path(__file__).resolve().parent
index = json.loads((evidence / "inputs.json").read_text())
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--repo", type=pathlib.Path, required=True)
parser.add_argument("--revision", choices=sorted(index["revisions"]), default="final")
parser.add_argument("--output", type=pathlib.Path, help="new directory; defaults to a private temporary directory")
parser.add_argument("--verify-only", action="store_true", help="verify all five 28-file inputs without creating a tree")
args = parser.parse_args()


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def git(*arguments):
    return subprocess.check_output(["git", *arguments], cwd=args.repo)


final = json.loads((evidence / index["revisions"]["final"]).read_text())
assert digest((evidence / index["revisions"]["final"]).read_bytes()) == index["final_manifest_sha256"]
final_bytes = {}
for path, expected in final["files"].items():
    raw = git("show", index["accepted_source_commit"] + ":" + path)
    assert digest(raw) == expected, path
    final_bytes[path] = raw


def material(revision):
    manifest = json.loads((evidence / index["revisions"][revision]).read_text())
    assert manifest["baseline"] == index["baseline"]
    assert set(manifest["files"]) == set(final_bytes)
    result = {}
    for path, expected in manifest["files"].items():
        raw = final_bytes[path]
        if digest(raw) != expected:
            raw = (evidence / index["historical_fragments"][expected]["file"]).read_bytes()
        assert digest(raw) == expected, (revision, path)
        result[path] = raw
    return result


verified = {revision: len(material(revision)) for revision in index["revisions"]}
probe = (evidence / index["probe"]["path"]).read_bytes()
assert digest(probe) == index["probe"]["sha256"]
if args.verify_only:
    print(json.dumps({"verified_byte_exact_inputs": verified, "probe_sha256": digest(probe)}, indent=2))
    raise SystemExit(0)

if args.output is None:
    output = pathlib.Path(tempfile.mkdtemp(prefix="agenteam-d05-s2-replay-"))
else:
    output = args.output.resolve()
    output.mkdir(mode=0o700, parents=False, exist_ok=False)

with tarfile.open(fileobj=io.BytesIO(git("archive", "--format=tar", index["baseline"]))) as archive:
    archive.extractall(output, filter="data")
for path, raw in material(args.revision).items():
    destination = output / path
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(raw)
if args.revision == "final":
    (output / index["probe"]["destination"]).write_bytes(probe)

record = {
    "revision": args.revision,
    "baseline": index["baseline"],
    "accepted_source_commit": index["accepted_source_commit"],
    "manifest": index["revisions"][args.revision],
    "files_verified": verified[args.revision],
    "independent_probe_included": args.revision == "final",
    "scope": "source reconstruction only; no Go, Docker, fixture or network action",
}
(output / "s2-reconstructed-input.json").write_text(json.dumps(record, indent=2) + "\n")
print(json.dumps({"output": str(output), **record}, indent=2))
