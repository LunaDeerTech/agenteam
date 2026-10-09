"""Focused tests for ai-team's CLI boundary; no model requests or installed dependencies."""

import contextlib
import ctypes
import importlib.util
import io
import json
import os
from pathlib import Path
import shlex
import signal
import sys
import tempfile
import time
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location("ai_team", Path(__file__).parents[1] / "ai-team.py")
TEAM = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(TEAM)


class TeamTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="agenteam-cli-test-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        (self.root / ".codex/agents").mkdir(parents=True)
        (self.root / ".agents/skills/example").mkdir(parents=True)
        (self.root / ".agents/skills/example/SKILL.md").write_text("---\nname: example\ndescription: Test skill\n---\n")
        (self.root / ".codex/config.toml").write_text(
            'model="gpt-6-astra"\nmodel_reasoning_effort="ultra"\nservice_tier="priority"\n'
            '[agents]\nenabled=true\nmax_depth=6\n'
            'default_subagent_model="gpt-6-astra"\ndefault_subagent_reasoning_effort="ultra"\n'
        )
        self.role = self.root / ".codex/agents/example.toml"
        self.role.write_text('name="example_worker"\ndescription="Example"\ndeveloper_instructions="Read only"\n'
                             'model="gpt-6-astra"\nmodel_reasoning_effort="ultra"\nservice_tier="priority"\n')
        self.codex = self.root / "codex"
        self.log = self.root / "rpc.jsonl"

    def fake_cli(self, mode="ok"):
        # All calls are local subprocesses; the protocol deliberately rejects model APIs.
        source = r'''
import json, os, pathlib, signal, sys, time
MODE = __MODE__
root = pathlib.Path(__file__).parent
if MODE == "version_child_pipe" or (MODE == "child_pipe" and "--version" not in sys.argv):
    child = os.fork()
    if child == 0:
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        time.sleep(30)
        os._exit(0)
    (root / "owned-child.pid").write_text(str(child))
if "--version" in sys.argv:
    print("codex-cli test")
    raise SystemExit(0)
if MODE == "unsupported":
    raise SystemExit(2)
values = {}
for index, arg in enumerate(sys.argv):
    if arg == "-c":
        key, value = sys.argv[index + 1].split("=", 1)
        values[key] = json.loads(value)
config = {key: value for key, value in values.items() if not key.startswith("agents.")}
config["agents"] = {}
for key, value in values.items():
    if key.startswith("agents."):
        parts = key.split(".")
        if len(parts) == 2:
            config["agents"][parts[1]] = value
        else:
            config["agents"].setdefault(parts[1], {})[parts[2]] = value
if MODE == "wrong_setting":
    config["service_tier"] = None
if MODE == "invalid_agents":
    config["agents"] = ["unsupported shape"]
if MODE == "inherited_limit":
    config["agents"]["max_concurrent_threads_per_session"] = 9
if MODE == "diagnostic":
    print("failed to deserialize agent role file: sensitive test detail", file=sys.stderr, flush=True)
for line in sys.stdin:
    request = json.loads(line)
    with (root / "rpc.jsonl").open("a") as log:
        log.write(json.dumps(request) + "\n")
    method = request["method"]
    if method == "initialized":
        continue
    if method not in {"initialize", "config/read", "skills/list"} or MODE == "rejected":
        print(json.dumps({"id": request["id"], "error": {"code": -32601, "message": "sensitive"}}), flush=True)
        continue
    if method == "config/read":
        result = {"config": config, "layers": [{"name": {"type": "project", "dotCodexFolder": str(root / ".codex")},
                  "disabledReason": "private path", "config": {"secret": "never print this"}}]}
    elif method == "skills/list":
        skills = [] if MODE == "missing_skill" else [{"name": "example", "path": str(root / ".agents/skills/example/SKILL.md"), "scope": "repo", "enabled": True}]
        result = {"data": [{"cwd": str(root), "skills": skills, "errors": []}]}
    else:
        result = {"userAgent": "test"}
    print(json.dumps({"id": request["id"], "result": result}), flush=True)
'''.replace("__MODE__", repr(mode))
        self.codex.write_text(f"#!{sys.executable}\n" + source)
        self.codex.chmod(0o700)
        return str(self.codex)

    def inspect(self, mode="ok"):
        settings, roles = TEAM.configuration(self.root)
        return TEAM.inspect_cli(self.root, self.fake_cli(mode), settings, roles)

    def test_reads_effective_settings_and_skills_without_model_calls_or_secrets(self):
        result = self.inspect()
        self.assertEqual(result["project_config_layer"], "disabled")
        self.assertEqual(result["effective_settings"]["service_tier"], "priority")
        self.assertEqual(result["explicit_role_mappings"], ["example_worker"])
        self.assertEqual(result["enabled_project_skills"], ["example"])
        self.assertNotIn("secret", json.dumps(result))
        methods = [json.loads(line)["method"] for line in self.log.read_text().splitlines()]
        self.assertEqual(methods, ["initialize", "initialized", "config/read", "skills/list"])

    def test_omitted_concurrency_does_not_send_override_or_claim_unlimited(self):
        settings, roles = TEAM.configuration(self.root)
        self.assertNotIn(TEAM.CONCURRENCY_KEY, settings)
        self.assertFalse(any(TEAM.CONCURRENCY_KEY in argument for argument in TEAM.overrides(settings, roles)))
        report = self.inspect()
        self.assertEqual(report["concurrency"]["source"], "runtime-default")
        self.assertIsNone(report["concurrency"]["requested_spawned_threads"])
        self.assertEqual(report["concurrency"]["actual_available_slots"], "not_measured")

    def test_omitted_concurrency_reports_inherited_configuration_separately(self):
        report = self.inspect("inherited_limit")
        self.assertEqual(report["concurrency"]["source"], "runtime-default")
        self.assertIsNone(report["concurrency"]["requested_spawned_threads"])
        self.assertEqual(report["concurrency"]["observed_config_value"], 9)

    def test_repository_concurrency_remains_optional_and_validated(self):
        path = self.root / ".codex/config.toml"
        original = path.read_text()
        path.write_text(original + 'max_concurrent_threads_per_session=12\n')
        self.assertEqual(self.inspect()["concurrency"]["source"], "repo")
        for invalid in ("0", "-1", "true"):
            with self.subTest(invalid=invalid):
                path.write_text(original + f'max_concurrent_threads_per_session={invalid}\n')
                with self.assertRaisesRegex(TEAM.TeamError, "positive integer"):
                    TEAM.configuration(self.root)

    def test_explicit_concurrency_reaches_check_and_start_argv(self):
        with mock.patch.object(TEAM, "ROOT", self.root), mock.patch.object(TEAM, "find_codex", return_value=self.fake_cli()):
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(TEAM.main(["check", "--max-agents", "32"]), 0)
            report = json.loads(output.getvalue())
            self.assertEqual(report["concurrency"]["source"], "explicit")
            self.assertEqual(report["concurrency"]["requested_spawned_threads"], 32)
            self.assertEqual(report["concurrency"]["observed_config_value"], 32)
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(TEAM.main(["start", "--max-agents", "32", "--dry-run"]), 0)
            self.assertIn(f"{TEAM.CONCURRENCY_KEY}=32", shlex.split(output.getvalue()))

    def test_cli_concurrency_rejects_zero_negative_and_noninteger_values(self):
        for command in ("check", "start"):
            for value in ("0", "-1", "unlimited", "1.5"):
                with self.subTest(command=command, value=value), contextlib.redirect_stderr(io.StringIO()):
                    with self.assertRaises(SystemExit) as caught:
                        TEAM.main([command, "--max-agents", value])
                    self.assertEqual(caught.exception.code, 2)

    def test_rejects_missing_cli(self):
        with mock.patch.object(TEAM.shutil, "which", return_value=None):
            with self.assertRaisesRegex(TEAM.TeamError, "not installed"):
                TEAM.find_codex()

    def test_rejects_platform_without_owned_process_groups(self):
        with mock.patch.object(TEAM.os, "killpg", None):
            with self.assertRaisesRegex(TEAM.TeamError, "require POSIX"):
                TEAM.require_process_groups()

    def test_rejects_unsupported_cli_flags(self):
        with self.assertRaisesRegex(TEAM.TeamError, "unsupported|exited"):
            self.inspect("unsupported")

    def test_rejects_rpc_error_without_echoing_details(self):
        with self.assertRaisesRegex(TEAM.TeamError, "was rejected") as caught:
            self.inspect("rejected")
        self.assertNotIn("sensitive", str(caught.exception))

    def test_rejects_effective_settings_different_from_toml(self):
        with self.assertRaisesRegex(TEAM.TeamError, "service_tier.*not reflected"):
            self.inspect("wrong_setting")

    def test_rejects_unsupported_response_shape_cleanly(self):
        with self.assertRaisesRegex(TEAM.TeamError, "invalid agents configuration"):
            self.inspect("invalid_agents")

    def test_rejects_missing_discovered_skill(self):
        with self.assertRaisesRegex(TEAM.TeamError, "skills not discovered"):
            self.inspect("missing_skill")

    def test_rejects_role_loader_diagnostic_even_with_successful_rpc(self):
        with self.assertRaisesRegex(TEAM.TeamError, "diagnostics"):
            self.inspect("diagnostic")

    def test_rejects_duplicate_or_downgraded_role(self):
        other = self.role.with_name("duplicate.toml")
        other.write_text(self.role.read_text())
        with self.assertRaisesRegex(TEAM.TeamError, "Duplicate role"):
            TEAM.configuration(self.root)
        other.unlink()
        self.role.write_text(self.role.read_text().replace('"ultra"', '"max"'))
        with self.assertRaisesRegex(TEAM.TeamError, "must use"):
            TEAM.configuration(self.root)

    def test_rpc_timeout_reaps_the_process(self):
        server = TEAM.AppServer([sys.executable, "-c", "import time; time.sleep(10)"], self.root, timeout=0.05)
        try:
            with self.assertRaisesRegex(TEAM.TeamError, "timed out"):
                server.call(1, "initialize", {})
        finally:
            server.close()
        self.assertIsNotNone(server.process.poll())
        self.assertTrue(all(not thread.is_alive() for thread in server.readers))

    @contextlib.contextmanager
    def adopt_fixture_child(self):
        """Linux subreaper lets this test actually wait its orphan fixture."""
        libc = ctypes.CDLL(None, use_errno=True)
        previous = ctypes.c_int()
        self.assertEqual(libc.prctl(37, ctypes.byref(previous), 0, 0, 0), 0)
        self.assertEqual(libc.prctl(36, 1, 0, 0, 0), 0)
        try:
            yield
        finally:
            pid_path = self.root / "owned-child.pid"
            if pid_path.exists():
                pid = int(pid_path.read_text())
                try:
                    waited, _ = os.waitpid(pid, os.WNOHANG)
                    if waited == 0:
                        os.kill(pid, signal.SIGKILL)
                        os.waitpid(pid, 0)
                except ChildProcessError:
                    pass
            self.assertEqual(libc.prctl(36, previous.value, 0, 0, 0), 0)

    def assert_fixture_child_stopped(self):
        pid = int((self.root / "owned-child.pid").read_text())
        deadline = time.monotonic() + 1
        while time.monotonic() < deadline:
            waited, status = os.waitpid(pid, os.WNOHANG)
            if waited:
                self.assertTrue(os.WIFSIGNALED(status))
                return
            time.sleep(0.01)
        self.fail("Owned descendant remained alive after shutdown")

    @unittest.skipUnless(sys.platform.startswith("linux"), "Linux subreaper verifies the orphan fixture")
    def test_normal_parent_exit_with_child_holding_pipes_is_bounded(self):
        with self.adopt_fixture_child():
            started = time.monotonic()
            self.assertEqual(self.inspect("child_pipe")["project_config_layer"], "disabled")
            self.assertLess(time.monotonic() - started, TEAM.CLOSE_TIMEOUT + 1)
            self.assert_fixture_child_stopped()

    @unittest.skipUnless(sys.platform.startswith("linux"), "Linux subreaper verifies the orphan fixture")
    def test_version_timeout_stops_child_holding_pipes(self):
        with self.adopt_fixture_child(), mock.patch.object(TEAM, "TIMEOUT", 0.15):
            started = time.monotonic()
            with self.assertRaisesRegex(TEAM.TeamError, "--version timed out"):
                TEAM.cli_version(self.fake_cli("version_child_pipe"))
            self.assertLess(time.monotonic() - started, TEAM.CLOSE_TIMEOUT + 1)
            self.assert_fixture_child_stopped()

    def test_dry_run_preserves_literal_prompt_without_invoking_cli(self):
        prompt = '$(touch /tmp/must-not-run) `echo nope` --config service_tier="flex"'
        output = io.StringIO()
        with mock.patch.object(TEAM, "ROOT", self.root), mock.patch.object(TEAM, "find_codex", return_value=self.fake_cli()), contextlib.redirect_stdout(output):
            self.assertEqual(TEAM.main(["start", "--dry-run", "--", prompt]), 0)
        command = shlex.split(output.getvalue())
        self.assertEqual(command[-2:], ["--", prompt])
        self.assertFalse(self.log.exists())
        self.assertNotIn("sandbox", output.getvalue())
        self.assertNotIn("trust_level", output.getvalue())

    def test_start_execs_argv_only_after_readonly_preflight(self):
        with mock.patch.object(TEAM, "ROOT", self.root), mock.patch.object(TEAM, "find_codex", return_value=self.fake_cli()), mock.patch.object(TEAM.os, "execv") as execute, contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(TEAM.main(["start", "--", "complete this task"]), 0)
        command = execute.call_args.args[1]
        self.assertEqual(command[-2:], ["--", "complete this task"])
        self.assertIn(f'agents.example_worker.config_file={json.dumps(str(self.role))}', command)


if __name__ == "__main__":
    unittest.main()
