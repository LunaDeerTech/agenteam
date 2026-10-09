#!/usr/bin/env python3
"""Check or launch the Codex team. Requires Python 3.11+ and POSIX process groups."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import queue
import re
import select
import shlex
import shutil
import signal
import subprocess
import sys
import threading
import time

if sys.version_info < (3, 11):
    raise SystemExit("ai-team requires Python 3.11 or newer (tomllib).")
import tomllib


ROOT = Path(__file__).resolve().parents[1]
TIMEOUT = 8
CLOSE_TIMEOUT = 2
TARGET = {"model": "gpt-6-astra", "model_reasoning_effort": "ultra", "service_tier": "priority"}
CONCURRENCY_KEY = "agents.max_concurrent_threads_per_session"


class TeamError(Exception):
    """An actionable configuration or CLI compatibility problem."""


def require_process_groups() -> None:
    if os.name != "posix" or not callable(getattr(os, "killpg", None)):
        raise TeamError("ai-team check/start require POSIX process groups (Linux, macOS or WSL); native Windows is not supported.")


def read_toml(path: Path) -> dict:
    try:
        return tomllib.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise TeamError(f"Cannot read valid TOML: {path}") from exc


def configuration(root: Path) -> tuple[dict, dict[str, Path]]:
    config = read_toml(root / ".codex/config.toml")
    if any(config.get(key) != value for key, value in TARGET.items()):
        raise TeamError("Project defaults must be gpt-6-astra / ultra / priority.")
    agents = config.get("agents", {})
    if not isinstance(agents, dict) or agents.get("enabled") is not True:
        raise TeamError("agents.enabled must be true.")
    expected_defaults = {
        "default_subagent_model": TARGET["model"],
        "default_subagent_reasoning_effort": TARGET["model_reasoning_effort"],
    }
    if any(agents.get(key) != value for key, value in expected_defaults.items()):
        raise TeamError("Subagent defaults must be gpt-6-astra / ultra.")
    for key in ("max_depth",):
        if type(agents.get(key)) is not int or agents[key] < 1:
            raise TeamError(f"agents.{key} must be a positive integer.")
    concurrency = agents.get("max_concurrent_threads_per_session")
    if "max_concurrent_threads_per_session" in agents and (type(concurrency) is not int or concurrency < 1):
        raise TeamError("agents.max_concurrent_threads_per_session must be a positive integer when specified.")
    settings = dict(TARGET)
    for key in ("enabled", "max_depth", *expected_defaults):
        settings[f"agents.{key}"] = agents[key]
    if concurrency is not None:
        settings[CONCURRENCY_KEY] = concurrency
    roles = {}
    for path in sorted((root / ".codex/agents").rglob("*.toml")):
        role = read_toml(path)
        name = role.get("name")
        if not isinstance(name, str) or not re.fullmatch(r"[a-z][a-z0-9_]*", name):
            raise TeamError(f"Invalid role name in {path.name}; use lowercase letters, digits and underscores.")
        if name in roles:
            raise TeamError(f"Duplicate role name: {name}")
        if any(role.get(key) != value for key, value in TARGET.items()):
            raise TeamError(f"Role {name} must use gpt-6-astra / ultra / priority.")
        if not all(isinstance(role.get(key), str) and role[key].strip()
                   for key in ("description", "developer_instructions")):
            raise TeamError(f"Role {name} needs description and developer_instructions.")
        roles[name] = path.resolve()
    if not roles:
        raise TeamError("No .codex/agents/*.toml roles found.")
    return settings, roles


def overrides(settings: dict, roles: dict[str, Path]) -> list[str]:
    values = dict(settings)
    # The CLI de-duplicates the same file and merges roles across config layers.
    # Per-invocation paths also work when project-local configuration is disabled.
    for name, path in roles.items():
        values[f"agents.{name}.config_file"] = str(path)
    arguments = []
    for key, value in values.items():
        arguments.extend(["-c", f"{key}={json.dumps(value, ensure_ascii=False)}"])
    return arguments


def find_codex() -> str:
    path = shutil.which("codex")
    if not path:
        raise TeamError("Codex CLI is not installed or not on PATH; install it separately.")
    return path


def cli_version(codex: str) -> str:
    server = AppServer([codex, "--version"], Path.cwd())
    output = []
    try:
        deadline = time.monotonic() + TIMEOUT
        while True:
            line = server.next_line(deadline, "--version")
            if line is None:
                break
            output.append(line)
    finally:
        server.close()
    version = "".join(output)
    if server.process.returncode or not version.startswith("codex-cli "):
        raise TeamError("The codex executable did not report a supported CLI version string.")
    return version.splitlines()[0]


class AppServer:
    """Short-lived configuration RPC client; never sends thread/start or turn/start."""

    def __init__(self, command: list[str], root: Path, timeout: float = TIMEOUT):
        require_process_groups()
        self.timeout = timeout
        self.lines: queue.Queue[str | None] = queue.Queue()
        self.diagnostic_failure = False
        self.stop_reading = threading.Event()
        self.process = subprocess.Popen(command, cwd=root, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                        text=True, encoding="utf-8", errors="replace",
                                        start_new_session=True)
        for stream in (self.process.stdout, self.process.stderr):
            os.set_blocking(stream.fileno(), False)
        self.readers = [threading.Thread(target=self._stdout, daemon=True),
                        threading.Thread(target=self._stderr, daemon=True)]
        for reader in self.readers:
            reader.start()

    def _read_lines(self, stream):
        # Nonblocking reads can stop even if a descendant keeps a pipe open.
        pending = b""
        descriptor = stream.fileno()
        while not self.stop_reading.is_set():
            if not select.select([descriptor], [], [], 0.05)[0]:
                continue
            try:
                chunk = os.read(descriptor, 65536)
            except BlockingIOError:
                continue
            if not chunk:
                break
            pending += chunk
            while b"\n" in pending:
                line, pending = pending.split(b"\n", 1)
                yield line.decode("utf-8", errors="replace") + "\n"
        if pending:
            yield pending.decode("utf-8", errors="replace")

    def _stdout(self) -> None:
        try:
            for line in self._read_lines(self.process.stdout):
                self.lines.put(line)
        finally:
            self.lines.put(None)

    def _stderr(self) -> None:
        # Never print full CLI diagnostics or config/read responses: they may contain secrets.
        indicators = ("failed to deserialize agent role", "malformed agent role",
                      "duplicate agent role", "unknown field", "failed to load agent role")
        for line in self._read_lines(self.process.stderr):
            if any(indicator in line.lower() for indicator in indicators):
                self.diagnostic_failure = True

    def send(self, message: dict) -> None:
        try:
            self.process.stdin.write(json.dumps(message) + "\n")
            self.process.stdin.flush()
        except (BrokenPipeError, OSError) as exc:
            raise TeamError("Codex app-server exited; check support for --strict-config and the supplied settings.") from exc

    def call(self, request_id: int, method: str, params: dict) -> dict:
        self.send({"id": request_id, "method": method, "params": params})
        # Notifications must not reset the timeout of a stalled RPC.
        deadline = time.monotonic() + self.timeout
        while True:
            line = self.next_line(deadline, f"RPC {method}")
            if line is None:
                raise TeamError(f"Codex app-server closed during {method}; required flags or configuration may be unsupported.")
            try:
                response = json.loads(line)
            except ValueError as exc:
                raise TeamError("Codex app-server returned invalid JSON.") from exc
            if not isinstance(response, dict):
                raise TeamError("Codex app-server returned an invalid RPC response.")
            if response.get("id") == request_id:
                if "error" in response:
                    raise TeamError(f"Codex RPC {method} was rejected; this CLI may not support the required API.")
                if not isinstance(response.get("result"), dict):
                    raise TeamError(f"Codex RPC {method} returned an invalid result.")
                return response["result"]

    def next_line(self, deadline: float, operation: str) -> str | None:
        try:
            return self.lines.get(timeout=max(0, deadline - time.monotonic()))
        except queue.Empty as exc:
            raise TeamError(f"Codex {operation} timed out.") from exc

    def _signal_group(self, signum: int) -> None:
        try:
            # start_new_session makes this PID the unique group we own. Signal
            # it even after its leader exits: descendants may still hold pipes.
            os.killpg(self.process.pid, signum)
        except ProcessLookupError:
            pass

    def close(self) -> None:
        deadline = time.monotonic() + CLOSE_TIMEOUT
        try:
            self.process.stdin.close()
        except (BrokenPipeError, OSError):
            pass
        try:
            self.process.wait(timeout=0.25)
        except subprocess.TimeoutExpired:
            pass
        self._signal_group(signal.SIGTERM)
        for reader in self.readers:
            reader.join(timeout=0.1)
        self._signal_group(signal.SIGKILL)
        self.stop_reading.set()
        try:
            self.process.wait(timeout=max(0, deadline - time.monotonic()))
        except subprocess.TimeoutExpired as exc:
            raise TeamError("Codex process group did not stop within the shutdown budget.") from exc
        for reader in self.readers:
            reader.join(timeout=max(0, deadline - time.monotonic()))
        if any(reader.is_alive() for reader in self.readers):
            # Closing TextIO while another thread is reading can block forever.
            raise TeamError("Codex pipe readers did not stop within the shutdown budget.")
        self.process.stdout.close()
        self.process.stderr.close()


def inspect_cli(root: Path, codex: str, settings: dict, roles: dict[str, Path], concurrency_source: str | None = None) -> dict:
    version = cli_version(codex)
    server = AppServer([codex, "--strict-config", "app-server", "--stdio", *overrides(settings, roles)], root)
    try:
        server.call(1, "initialize", {"clientInfo": {"name": "agenteam_check", "version": "1"},
                                      "capabilities": {"experimentalApi": True}})
        server.send({"method": "initialized"})
        response = server.call(2, "config/read", {"cwd": str(root), "includeLayers": True})
        effective = response.get("config", {})
        if not isinstance(effective, dict):
            raise TeamError("Codex returned an invalid effective configuration.")
        effective_agents = effective.get("agents") or {}
        if not isinstance(effective_agents, dict):
            raise TeamError("Codex returned an invalid agents configuration.")
        for key, expected in settings.items():
            actual = effective_agents.get(key.removeprefix("agents.")) if key.startswith("agents.") else effective.get(key)
            if actual != expected:
                raise TeamError(f"Explicit setting {key} was not reflected by config/read.")
        for name, path in roles.items():
            entry = effective_agents.get(name)
            if not isinstance(entry, dict) or entry.get("config_file") != str(path):
                raise TeamError(f"Explicit role mapping {name} was not reflected by config/read.")
        layers = response.get("layers") or []
        if not isinstance(layers, list) or not all(isinstance(layer, dict) and isinstance(layer.get("name"), dict) for layer in layers):
            raise TeamError("Codex returned invalid configuration layer metadata.")
        project_layers = [layer for layer in layers
                          if layer.get("name", {}).get("type") == "project"
                          and layer["name"].get("dotCodexFolder") == str(root / ".codex")]
        project_state = ("disabled" if any(layer.get("disabledReason") for layer in project_layers)
                         else "enabled" if project_layers else "not_reported")
        result = server.call(3, "skills/list", {"cwds": [str(root)], "forceReload": True})
        data = result.get("data")
        if not isinstance(data, list) or not all(isinstance(entry, dict) for entry in data):
            raise TeamError("Codex returned invalid skill discovery data.")
        entries = [entry for entry in data if entry.get("cwd") == str(root)]
        if len(entries) != 1 or entries[0].get("errors"):
            raise TeamError("Codex skill discovery failed or reported errors for this repository.")
        skills = entries[0].get("skills")
        if not isinstance(skills, list) or not all(isinstance(skill, dict) and isinstance(skill.get("name"), str) for skill in skills):
            raise TeamError("Codex returned invalid skill entries.")
        discovered = {skill.get("path"): skill for skill in skills
                      if skill.get("enabled") is True and skill.get("scope") == "repo"}
        expected_skills = sorted((root / ".agents/skills").glob("*/SKILL.md"))
        if not expected_skills:
            raise TeamError("No project skills found under .agents/skills.")
        missing = [path.parent.name for path in expected_skills if str(path) not in discovered]
        if missing:
            raise TeamError("Project skills not discovered as enabled: " + ", ".join(missing))
    finally:
        server.close()
    if server.process.returncode != 0 or server.diagnostic_failure:
        raise TeamError("Codex reported invalid role/configuration diagnostics or failed during shutdown.")
    return {"cli_version": version, "project_config_layer": project_state,
            "effective_settings": settings, "explicit_role_mappings": sorted(roles),
            "enabled_project_skills": [discovered[str(path)]["name"] for path in expected_skills],
            "concurrency": {"source": concurrency_source or ("repo" if CONCURRENCY_KEY in settings else "runtime-default"),
                            "requested_spawned_threads": settings.get(CONCURRENCY_KEY),
                            "observed_config_value": effective_agents.get("max_concurrent_threads_per_session"),
                            "actual_available_slots": "not_measured"},
            "limits": ["Role mappings and configuration were read; no agent instance was started.",
                       "Directory auto-discovery and actual request-level Fast service were not tested.",
                       "Omitting the concurrency setting inherits runtime configuration/backend defaults, not unlimited capacity.",
                       "The configured count excludes the primary. A V2-specific setting or service limit may take precedence or be lower.",
                       "max_depth applies to V1; V2 ignores it. Configured concurrency does not add runtime slots."]}


def positive_agent_count(value: str) -> int:
    try:
        count = int(value)
    except ValueError as exc:
        raise argparse.ArgumentTypeError("--max-agents requires a positive integer.") from exc
    if count < 1:
        raise argparse.ArgumentTypeError("--max-agents requires a positive integer.")
    return count


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    check = commands.add_parser("check", help="Read effective CLI configuration and discover skills without model requests.")
    start = commands.add_parser("start", help="Start Codex with explicit repository team settings.")
    for command_parser in (check, start):
        command_parser.add_argument("--max-agents", type=positive_agent_count, metavar="N",
                                    help="Override concurrent spawned threads for this invocation; excludes the primary. Omit to inherit runtime defaults.")
    start.add_argument("--dry-run", action="store_true", help="Only print the invocation; do not launch Codex.")
    start.add_argument("prompt", nargs=argparse.REMAINDER, help="Optional prompt, after --.")
    args = parser.parse_args(argv)
    try:
        require_process_groups()
        settings, roles = configuration(ROOT)
        concurrency_source = "repo" if CONCURRENCY_KEY in settings else "runtime-default"
        if args.max_agents is not None:
            settings[CONCURRENCY_KEY] = args.max_agents
            concurrency_source = "explicit"
        codex = find_codex()
        command = [codex, "--strict-config", "-C", str(ROOT), *overrides(settings, roles)]
        if args.command == "start":
            prompt = args.prompt[1:] if args.prompt[:1] == ["--"] else args.prompt
            if prompt:
                command.extend(["--", " ".join(prompt)])
            if args.dry_run:
                print(shlex.join(command))
                return 0
        report = inspect_cli(ROOT, codex, settings, roles, concurrency_source)
        print(json.dumps(report, ensure_ascii=False, indent=2), flush=True)
        if args.command == "start":
            os.execv(codex, command)
        return 0
    except (TeamError, OSError) as exc:
        print(f"ai-team: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
