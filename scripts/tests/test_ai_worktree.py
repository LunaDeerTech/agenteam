"""Small local-repository checks; no network, product builds or shared caches."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "ai-worktree.py"
EVIDENCE = "docs/development/agent-team/example-verification-evidence"


class WorktreeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="ai-worktree-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        self.env.update({"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
                         "GIT_AUTHOR_NAME": "Worktree Test", "GIT_AUTHOR_EMAIL": "test@example.invalid",
                         "GIT_COMMITTER_NAME": "Worktree Test", "GIT_COMMITTER_EMAIL": "test@example.invalid"})
        self.git("init", "--initial-branch=main")
        self.kept = ["AGENTS.md", "internal/source.go", "docs/architecture/contract.md",
                     EVIDENCE + "/README.md", EVIDENCE + "/revisions/rev1.1/spec.md",
                     "tests/testdata/objects/input.txt"]
        self.excluded = [EVIDENCE + "/objects/old.bin", EVIDENCE + "/revisions/rev1.1/objects/old.bin"]
        for name in self.kept + self.excluded:
            path = self.repo / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("preserved in Git\n")
        self.git("add", ".")
        self.git("commit", "-m", "test: base")
        self.base = self.git("rev-parse", "HEAD").stdout.strip()

    def run_command(self, argv, expected=0):
        result = subprocess.run(argv, cwd=self.repo, env=self.env, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        return result

    def git(self, *args, expected=0):
        return self.run_command(["git", *args], expected)

    def create(self, destination, *options, branch="ai/test", expected=0):
        return self.run_command([sys.executable, str(SCRIPT), "create", "--base", self.base,
                                 "--branch", branch, *options, str(destination)], expected)

    def test_plan_has_no_mutation(self):
        destination = self.root / "new tree"
        result = self.create(destination)
        self.assertIn("Plan only", result.stdout)
        self.assertFalse(destination.exists())
        self.git("show-ref", "--verify", "--quiet", "refs/heads/ai/test", expected=1)

    def test_apply_preserves_source_and_history_without_materializing_objects(self):
        destination = self.root / "new tree"
        self.create(destination, "--apply")
        for name in self.kept:
            self.assertEqual((destination / name).read_bytes(), (self.repo / name).read_bytes())
        for name in self.excluded:
            self.assertFalse((destination / name).exists())
            self.assertTrue((self.repo / name).is_file())
            self.assertEqual(self.git("-C", str(destination), "show", "HEAD:" + name).stdout,
                             "preserved in Git\n")
        self.assertEqual(self.git("-C", str(destination), "status", "--porcelain").stdout, "")
        self.assertEqual(self.git("-C", str(destination), "rev-parse", "HEAD").stdout.strip(), self.base)
        # Sparse settings belong to the new worktree, not the retained main checkout.
        self.assertEqual(self.git("status", "--porcelain").stdout, "")

    def test_existing_paths_branches_and_nested_worktrees_are_not_overwritten(self):
        destination = self.root / "occupied"
        destination.mkdir()
        sentinel = destination / "keep"
        sentinel.write_text("unrelated work")
        self.create(destination, "--apply", expected=1)
        self.assertEqual(sentinel.read_text(), "unrelated work")
        self.create(self.repo / "nested", "--apply", expected=1)
        self.assertFalse((self.repo / "nested").exists())
        self.git("branch", "ai/occupied", self.base)
        self.create(self.root / "other", "--apply", branch="ai/occupied", expected=1)
        self.create(self.root / "other", "--apply", branch="main", expected=1)
        self.assertFalse((self.root / "other").exists())


if __name__ == "__main__":
    unittest.main()
