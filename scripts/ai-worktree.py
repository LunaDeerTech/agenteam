#!/usr/bin/env python3
"""Plan a sparse ai/* worktree; only the Git owner runs with --apply."""

import argparse
import os
from pathlib import Path
import shlex
import subprocess
import sys


PATTERNS = (
    "/*\n"
    "!/docs/development/agent-team/*verification-evidence*/objects/\n"
    "!/docs/development/agent-team/*verification-evidence*/**/objects/\n"
)


class WorktreeError(Exception):
    pass


def git(root, *args, check=True, input=None):
    result = subprocess.run(
        ["git", "-C", str(root), *args], input=input,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        encoding="utf-8", errors="surrogateescape",
    )
    if check and result.returncode:
        raise WorktreeError(result.stderr.strip() or result.stdout.strip()
                            or f"git {args[0]} failed ({result.returncode})")
    return result


def create(args):
    root = Path(git(Path.cwd(), "rev-parse", "--show-toplevel").stdout.rstrip("\n")).resolve()
    if not args.branch.startswith("ai/"):
        raise WorktreeError("Use a new ai/<task> branch.")
    git(root, "check-ref-format", f"refs/heads/{args.branch}")
    exists = git(root, "show-ref", "--verify", "--quiet", f"refs/heads/{args.branch}", check=False)
    if exists.returncode == 0:
        raise WorktreeError("Local branch already exists; inspect its owner and resume it explicitly.")
    if exists.returncode != 1:
        raise WorktreeError(exists.stderr.strip() or "Unable to check the branch.")
    base = git(root, "rev-parse", "--verify", "--end-of-options", args.base + "^{commit}").stdout.strip()
    supplied = Path(os.path.abspath(args.path))
    if os.path.lexists(supplied):
        raise WorktreeError("Destination already exists; it was left untouched.")
    destination = supplied.resolve()
    records = git(root, "worktree", "list", "--porcelain", "-z").stdout.split("\0")
    for record in records:
        if record.startswith("worktree "):
            existing = Path(record[len("worktree "):]).resolve()
            if destination == existing or existing in destination.parents:
                raise WorktreeError("Do not create a worktree inside another worktree.")
    commands = [
        (root, ["worktree", "add", "--no-checkout", "--no-track", "-b",
                args.branch, str(destination), base], None),
        (destination, ["sparse-checkout", "set", "--no-cone", "--stdin"], PATTERNS),
        (destination, ["read-tree", "-mu", "HEAD"], None),
    ]
    print(f"Base: {base}\nDestination: {destination}")
    for cwd, argv, stdin in commands:
        print(shlex.join(["git", "-C", str(cwd), *argv]))
        if stdin is not None:
            print("stdin:\n" + stdin, end="")
    if not args.apply:
        print("Plan only. The Git owner may repeat with --apply after confirming ownership.")
        return
    try:
        for cwd, argv, stdin in commands:
            git(cwd, *argv, input=stdin)
    except (WorktreeError, OSError):
        print("Stopped. Inspect the new branch/worktree; partial state is retained, not deleted.",
              file=sys.stderr)
        raise
    print("Created sparse worktree. No fetch, push, dependency copy or cache migration was performed.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    command = parser.add_subparsers(dest="command", required=True).add_parser("create")
    command.add_argument("--branch", required=True, help="new ai/<task> local branch")
    command.add_argument("--base", required=True, help="explicit existing commit or ref; resolved once")
    command.add_argument("--apply", action="store_true", help="create the planned worktree (Git owner only)")
    command.add_argument("path", help="new destination outside existing worktrees")
    args = parser.parse_args()
    try:
        create(args)
    except (WorktreeError, OSError) as error:
        print(f"ai-worktree: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
