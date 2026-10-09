#!/usr/bin/env python3
"""Save explicit WIP files and the handoff on the current ai/* branch."""

import argparse
import os
from pathlib import Path
import stat
import subprocess
import sys


HANDOFF = ".agent-state/current.md"


class CheckpointError(Exception):
    pass


def git(cwd, *args, check=True, input=None, literal=True):
    # Treat every supplied filename literally, including '*', ':' and '['.
    env = os.environ.copy()
    for name in ("GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS", "GIT_ICASE_PATHSPECS"):
        env.pop(name, None)
    if literal:
        env["GIT_LITERAL_PATHSPECS"] = "1"
    else:
        env.pop("GIT_LITERAL_PATHSPECS", None)
    env["GIT_TERMINAL_PROMPT"] = "0"
    result = subprocess.run(
        ["git", *args], cwd=cwd, env=env, input=input,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        encoding="utf-8", errors="surrogateescape",
    )
    if check and result.returncode:
        raise CheckpointError(result.stderr.strip() or result.stdout.strip()
                              or f"git {args[0]} failed ({result.returncode})")
    return result


def nul_paths(output):
    return set(output.rstrip("\0").split("\0")) if output else set()


def explicit_file(root, cwd, supplied):
    if not supplied:
        raise CheckpointError("Empty file paths are not allowed.")
    absolute = Path(os.path.abspath(os.path.join(cwd, supplied)))
    try:
        relative = absolute.relative_to(root)
    except ValueError:
        raise CheckpointError(f"Path is outside this repository: {supplied!r}") from None
    if not relative.parts or ".git" in relative.parts:
        raise CheckpointError(f"Repository root and Git metadata are not allowed: {supplied!r}")
    for ancestor in relative.parents:
        if (root / ancestor).is_symlink():
            raise CheckpointError(f"A parent directory is a symlink: {supplied!r}")
    if absolute.is_symlink():
        raise CheckpointError(
            f"Symlinks cannot preserve checkpoint contents across devices: {supplied!r}. "
            "Save the required contents as a regular file inside the repository and retry."
        )
    if absolute.is_dir():
        raise CheckpointError(f"List individual files, not directories: {supplied!r}")
    if absolute.exists():
        mode = absolute.lstat().st_mode
        if not stat.S_ISREG(mode):
            raise CheckpointError(f"Not a regular file: {supplied!r}")
    name = relative.as_posix()
    tracked = git(root, "ls-files", "--stage", "-z", "--", name).stdout
    tracked_names = {entry.split("\t", 1)[1] for entry in tracked.split("\0") if entry}
    if tracked_names - {name}:
        raise CheckpointError(f"List individual files, not deleted directories: {supplied!r}")
    if not tracked and not absolute.exists():
        # A successfully committed deletion is now absent from both disk and
        # index. Keep that exact save command usable when its push needs retrying.
        if not git(root, "log", "-1", "--format=%H", "--", name).stdout:
            raise CheckpointError(f"File does not exist and has no tracked history: {supplied!r}")
    if tracked.startswith("160000 "):
        raise CheckpointError(f"Submodule paths are not supported: {supplied!r}")
    return name


def save(args):
    if not args.message.startswith("wip:") or not args.message[4:].strip():
        raise CheckpointError("Checkpoint messages must start with 'wip:' and describe the work.")
    cwd = Path.cwd()
    root = Path(git(cwd, "rev-parse", "--show-toplevel").stdout.removesuffix("\n"))
    branch_result = git(root, "symbolic-ref", "--quiet", "--short", "HEAD", check=False)
    branch = branch_result.stdout.removesuffix("\n")
    if branch_result.returncode or not branch.startswith("ai/"):
        raise CheckpointError("Checkpoints require the current branch to be ai/<task>; no branch was changed.")
    git(root, "rev-parse", "--verify", "HEAD")
    git(root, "remote", "get-url", "--push", "origin")
    for marker in ("MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer"):
        marker_path = git(root, "rev-parse", "--git-path", marker).stdout.removesuffix("\n")
        if (root / marker_path).exists():
            raise CheckpointError("Finish the active Git merge/rebase/cherry-pick/revert before saving a checkpoint.")
    if git(root, "ls-files", "--unmerged", "-z").stdout:
        raise CheckpointError("Resolve the unmerged index before saving a checkpoint.")

    paths = {explicit_file(root, cwd, path) for path in args.paths}
    handoff = root / HANDOFF
    if not handoff.is_file() or handoff.is_symlink():
        raise CheckpointError(f"A regular handoff file is required: {HANDOFF}")
    paths.add(explicit_file(root, root, HANDOFF))
    paths = sorted(paths)
    # check-ignore takes filenames rather than pathspecs and rejects the literal
    # flag. Prefix './' so filenames starting with ':' cannot be parsed as magic.
    ignored = git(root, "check-ignore", "--no-index", "-z", "--stdin",
                  check=False, input="\0".join("./" + path for path in paths) + "\0", literal=False)
    if ignored.returncode not in (0, 1):
        raise CheckpointError(ignored.stderr.strip() or "Unable to check ignore rules.")
    if ignored.stdout:
        raise CheckpointError(f"Ignored paths cannot be checkpointed: {sorted(nul_paths(ignored.stdout))!r}")
    staged = nul_paths(git(root, "diff", "--cached", "--name-only", "--no-renames", "-z").stdout)
    unrelated = staged - set(paths)
    if unrelated:
        raise CheckpointError(f"Unrelated staged changes were left untouched: {sorted(unrelated)!r}")

    stageable = [path for path in paths if os.path.lexists(root / path)
                 or git(root, "ls-files", "-z", "--", path).stdout]
    git(root, "add", "--all", "--", *stageable)
    changed = nul_paths(git(root, "diff", "--name-only", "--no-renames", "-z", "HEAD", "--", *paths).stdout)
    if changed:
        # --only also prevents a concurrent unrelated staging operation from
        # silently entering this commit. Writers of the selected files must stop.
        commit = git(root, "commit", "--only", "--message", args.message, "--", *sorted(changed))
        print(commit.stdout.strip(), flush=True)
    else:
        print("No file changes; retrying any unpushed commits without an empty commit.", flush=True)
    commit_id = git(root, "rev-parse", "HEAD").stdout.strip()
    print(f"Local checkpoint: {branch} {commit_id}", flush=True)
    ref = f"refs/heads/{branch}"
    pushed = git(root, "push", "--porcelain", "--no-follow-tags", "--recurse-submodules=no",
                 "origin", f"{ref}:{ref}", check=False)
    if pushed.returncode:
        detail = pushed.stderr.strip() or pushed.stdout.strip()
        raise CheckpointError(
            f"Remote save FAILED; local commit {commit_id} is retained.\n{detail}\n"
            "Fix connectivity/authentication and retry the same save command. If origin has new commits, "
            "fetch and integrate both devices' work on this branch before retrying; never force-push."
        )
    print(f"Remote checkpoint saved: origin/{branch} {commit_id}. WIP; product validation is not implied.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subcommands = parser.add_subparsers(dest="command", required=True)
    command = subcommands.add_parser(
        "save", help="commit explicit files plus the handoff, then push origin/ai/<task>",
        description="Save WIP without requiring product tests. Stop writers first. Use -- before literal file paths; directories are rejected.",
    )
    command.add_argument("--message", required=True, help="a subject starting with 'wip:'")
    command.add_argument("paths", nargs="*", help="explicit files relative to the current directory, including deletions; omit for handoff-only saves or push retries")
    args = parser.parse_args()
    try:
        save(args)
    except (CheckpointError, OSError) as error:
        print(f"ai-checkpoint: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
