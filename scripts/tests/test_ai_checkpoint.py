"""Integration tests using only temporary repositories and local bare remotes."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "ai-checkpoint.py"
HANDOFF = ".agent-state/current.md"
BRANCH = "ai/checkpoint-test"


class CheckpointIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="ai-checkpoint-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.remote = self.root / "origin.git"
        self.first = self.root / "first"
        self.second = self.root / "second"
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
        self.env.update({
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_TERMINAL_PROMPT": "0",
            "GIT_AUTHOR_NAME": "Checkpoint Test",
            "GIT_AUTHOR_EMAIL": "checkpoint@example.invalid",
            "GIT_COMMITTER_NAME": "Checkpoint Test",
            "GIT_COMMITTER_EMAIL": "checkpoint@example.invalid",
        })
        self.git(self.root, "init", "--bare", "--initial-branch=main", str(self.remote))
        self.git(self.root, "clone", str(self.remote), str(self.first))
        self.write(self.first, HANDOFF, "Task: checkpoint\nStatus: unverified\nNext: continue\n")
        self.write(self.first, ".gitignore", "secrets/\n.env*\noutput/\n")
        self.write(self.first, "source.txt", "base\n")
        self.write(self.first, "delete me.txt", "delete later\n")
        self.git(self.first, "add", "--", HANDOFF, ".gitignore", "source.txt", "delete me.txt")
        self.git(self.first, "commit", "-m", "test: seed")
        self.git(self.first, "push", "origin", "main")
        self.git(self.first, "switch", "-c", BRANCH)
        self.git(self.first, "push", "-u", "origin", BRANCH)
        self.git(self.root, "clone", "--branch", BRANCH, str(self.remote), str(self.second))

    def run_command(self, cwd, arguments, expected=0):
        result = subprocess.run(arguments, cwd=cwd, env=self.env, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if expected is not None:
            self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        return result

    def git(self, cwd, *arguments, expected=0):
        return self.run_command(cwd, ["git", *arguments], expected)

    def checkpoint(self, *paths, cwd=None, expected=0, message="wip: checkpoint test"):
        return self.run_command(cwd or self.first,
                                [sys.executable, str(SCRIPT), "save", "--message", message, "--", *paths],
                                expected)

    def write(self, repo, name, contents):
        target = repo / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(contents)

    def head(self, repo):
        return self.git(repo, "rev-parse", "HEAD").stdout.strip()

    def remote_head(self):
        return self.git(self.remote, "rev-parse", f"refs/heads/{BRANCH}").stdout.strip()

    def refresh_second(self):
        self.git(self.second, "fetch", "origin")
        self.git(self.second, "merge", "--ff-only", f"origin/{BRANCH}")

    def test_source_harness_and_handoff_recover_on_second_clone(self):
        self.write(self.first, "new source.go", "package broken\nfunc unfinished(\n")
        self.write(self.first, "tests/reproduce.py", "raise RuntimeError('known failure')\n")
        self.write(self.first, HANDOFF, "Task: unfinished implementation\nFailed: compilation\nNext: fix syntax\n")
        result = self.checkpoint("new source.go", "tests/reproduce.py")
        self.assertIn("Remote checkpoint saved", result.stdout)
        self.refresh_second()
        for name in ("new source.go", "tests/reproduce.py", HANDOFF):
            self.assertEqual((self.first / name).read_bytes(), (self.second / name).read_bytes())
        self.assertTrue(self.git(self.second, "log", "-1", "--format=%s").stdout.startswith("wip:"))
        self.assertEqual(self.head(self.first), self.head(self.second))

    def test_deletion_and_no_op_do_not_create_empty_commit(self):
        (self.first / "delete me.txt").unlink()
        self.checkpoint("delete me.txt")
        saved = self.head(self.first)
        self.refresh_second()
        self.assertFalse((self.second / "delete me.txt").exists())
        result = self.checkpoint("delete me.txt")
        self.assertIn("No file changes", result.stdout)
        self.assertEqual(self.head(self.first), saved)
        self.assertEqual(self.remote_head(), saved)

    def test_push_failure_keeps_local_commit_and_no_op_retries_it(self):
        before = self.remote_head()
        self.git(self.first, "remote", "set-url", "--push", "origin", str(self.root / "missing.git"))
        self.write(self.first, "source.txt", "offline work\n")
        failed = self.checkpoint("source.txt", expected=1)
        self.assertIn("Remote save FAILED", failed.stderr)
        saved = self.head(self.first)
        self.assertNotEqual(saved, before)
        self.assertEqual(self.remote_head(), before)
        self.git(self.first, "remote", "set-url", "--push", "origin", str(self.remote))
        result = self.checkpoint("source.txt")
        self.assertIn("No file changes", result.stdout)
        self.assertEqual(self.head(self.first), saved)
        self.assertEqual(self.remote_head(), saved)
        self.refresh_second()
        self.assertEqual((self.second / "source.txt").read_text(), "offline work\n")

    def test_staged_deletion_push_failure_retries_the_same_paths(self):
        self.git(self.first, "rm", "--", "delete me.txt")
        self.git(self.first, "remote", "set-url", "--push", "origin", str(self.root / "missing.git"))
        self.checkpoint("delete me.txt", expected=1)
        saved = self.head(self.first)
        self.git(self.first, "remote", "set-url", "--push", "origin", str(self.remote))
        self.checkpoint("delete me.txt")
        self.assertEqual(self.head(self.first), saved)
        self.assertEqual(self.remote_head(), saved)
        self.refresh_second()
        self.assertFalse((self.second / "delete me.txt").exists())

    def test_handoff_only_save_and_retry(self):
        self.write(self.first, HANDOFF, "Status: still unverified\nNext: resume on another device\n")
        self.checkpoint()
        saved = self.head(self.first)
        result = self.checkpoint()
        self.assertIn("No file changes", result.stdout)
        self.assertEqual(self.head(self.first), saved)
        self.refresh_second()
        self.assertEqual((self.first / HANDOFF).read_bytes(), (self.second / HANDOFF).read_bytes())

    def test_concurrent_remote_work_is_preserved_then_integrated(self):
        self.write(self.second, "other device.txt", "other device work\n")
        self.git(self.second, "add", "--", "other device.txt")
        self.git(self.second, "commit", "-m", "wip: other device")
        self.git(self.second, "push", "origin", BRANCH)
        remote_commit = self.head(self.second)
        self.write(self.first, "source.txt", "first device work\n")
        failed = self.checkpoint("source.txt", expected=1)
        self.assertIn("Remote save FAILED", failed.stderr)
        self.assertEqual(self.remote_head(), remote_commit)
        self.assertEqual((self.first / "source.txt").read_text(), "first device work\n")
        self.git(self.first, "fetch", "origin")
        self.git(self.first, "merge", "--no-edit", f"origin/{BRANCH}")
        integrated = self.head(self.first)
        self.checkpoint("source.txt")
        self.assertEqual(self.head(self.first), integrated)
        self.refresh_second()
        self.assertEqual((self.second / "source.txt").read_text(), "first device work\n")
        self.assertEqual((self.second / "other device.txt").read_text(), "other device work\n")

    def test_protected_branches_and_detached_head_are_rejected(self):
        for branch in ("main", "master", "feature/other"):
            with self.subTest(branch=branch):
                if branch == "main":
                    self.git(self.first, "switch", branch)
                else:
                    self.git(self.first, "switch", "-c", branch)
                before = self.head(self.first)
                result = self.checkpoint("source.txt", expected=1)
                self.assertIn("ai/<task>", result.stderr)
                self.assertEqual(self.head(self.first), before)
        self.git(self.first, "switch", "--detach")
        result = self.checkpoint("source.txt", expected=1)
        self.assertIn("ai/<task>", result.stderr)

    def test_unrelated_staged_changes_are_rejected_without_index_changes(self):
        self.write(self.first, "someone else.txt", "keep staged\n")
        self.git(self.first, "add", "--", "someone else.txt")
        self.write(self.first, "source.txt", "owned edit\n")
        before = self.head(self.first)
        index_before = self.git(self.first, "diff", "--cached", "--binary").stdout
        result = self.checkpoint("source.txt", expected=1)
        self.assertIn("Unrelated staged", result.stderr)
        self.assertEqual(self.head(self.first), before)
        self.assertEqual(self.git(self.first, "diff", "--cached", "--binary").stdout, index_before)
        self.assertEqual((self.first / "source.txt").read_text(), "owned edit\n")

    def test_ignored_and_broad_paths_are_rejected_before_staging(self):
        self.write(self.first, ".env.private", "do not publish\n")
        self.write(self.first, "source.txt", "owned edit\n")
        self.write(self.first, "folder/new.txt", "explicit files only\n")
        before = self.head(self.first)
        for path in (".env.private", ".", str(self.first), "folder", "../outside", ".git/config"):
            with self.subTest(path=path):
                self.checkpoint("source.txt", path, expected=1)
                self.assertEqual(self.head(self.first), before)
                self.assertEqual(self.git(self.first, "diff", "--cached", "--name-only").stdout, "")

    def test_literal_special_filenames_and_subdirectory_invocation(self):
        names = ["odd names/star*.txt", "odd names/star-other.txt", "odd names/[abc].txt",
                 "odd names/line\nbreak.txt", "odd names/--option", "odd names/:(glob)*"]
        for name in names:
            self.write(self.first, name, f"literal {name}\n")
        chosen = [names[0], *names[2:]]
        self.checkpoint(*(Path(name).name for name in chosen), cwd=self.first / "odd names")
        self.refresh_second()
        for name in chosen:
            self.assertEqual((self.second / name).read_bytes(), (self.first / name).read_bytes())
        self.assertFalse((self.second / names[1]).exists())
        self.assertEqual(self.git(self.first, "ls-files", "--", names[1]).stdout, "")

    def test_symlinks_are_rejected_without_changing_index_head_or_remote(self):
        external = self.root / "needed-harness.py"
        external.write_text("print('required reproduction')\n")
        self.write(self.first, "source.txt", "already staged work\n")
        self.git(self.first, "add", "--", "source.txt")
        self.write(self.first, "source.txt", "later working edit\n")
        before = self.head(self.first)
        index_before = self.git(self.first, "diff", "--cached", "--binary").stdout
        remote_before = self.remote_head()
        for name, target in (("external-link.py", external),
                             ("broken-link.py", self.root / "absent-harness.py"),
                             ("internal-link.txt", Path("source.txt"))):
            with self.subTest(name=name):
                (self.first / name).symlink_to(target)
                result = self.checkpoint("source.txt", name, expected=1)
                self.assertIn("Save the required contents as a regular file", result.stderr)
                self.assertEqual(self.head(self.first), before)
                self.assertEqual(self.git(self.first, "diff", "--cached", "--binary").stdout, index_before)
                self.assertEqual(self.remote_head(), remote_before)
                self.assertEqual((self.first / "source.txt").read_text(), "later working edit\n")

    def test_deleting_a_tracked_symlink_can_be_checkpointed(self):
        link = self.first / "legacy-link.txt"
        link.symlink_to("source.txt")
        self.git(self.first, "add", "--", "legacy-link.txt")
        self.git(self.first, "commit", "-m", "test: legacy symlink")
        self.git(self.first, "push", "origin", BRANCH)
        self.refresh_second()
        self.assertTrue((self.second / "legacy-link.txt").is_symlink())
        link.unlink()
        self.checkpoint("legacy-link.txt")
        saved = self.head(self.first)
        self.checkpoint("legacy-link.txt")
        self.assertEqual(self.head(self.first), saved)
        self.refresh_second()
        self.assertFalse(os.path.lexists(self.second / "legacy-link.txt"))

    def test_handoff_is_mandatory_and_wip_subject_is_required(self):
        before = self.head(self.first)
        self.checkpoint("source.txt", expected=1, message="feat: looks complete")
        (self.first / HANDOFF).unlink()
        failed = self.checkpoint("source.txt", expected=1)
        self.assertIn("handoff", failed.stderr)
        self.assertEqual(self.head(self.first), before)

    def test_first_checkpoint_includes_untracked_handoff_in_same_commit(self):
        self.git(self.first, "rm", "--cached", "--", HANDOFF)
        self.git(self.first, "commit", "-m", "test: no handoff in baseline")
        before = self.head(self.first)
        self.write(self.first, "new.txt", "first checkpoint\n")
        self.checkpoint("new.txt")
        self.assertEqual(self.git(self.first, "rev-list", "--count", f"{before}..HEAD").stdout.strip(), "1")
        self.refresh_second()
        self.assertEqual((self.second / HANDOFF).read_bytes(), (self.first / HANDOFF).read_bytes())
        self.assertEqual((self.second / "new.txt").read_text(), "first checkpoint\n")

    def test_selected_staged_and_unstaged_edits_save_full_working_file(self):
        self.write(self.first, "source.txt", "staged\n")
        self.git(self.first, "add", "--", "source.txt")
        self.write(self.first, "source.txt", "staged plus latest working edit\n")
        self.checkpoint("source.txt")
        self.refresh_second()
        self.assertEqual((self.second / "source.txt").read_text(), "staged plus latest working edit\n")
        self.assertEqual(self.git(self.first, "status", "--porcelain").stdout, "")


if __name__ == "__main__":
    unittest.main()
