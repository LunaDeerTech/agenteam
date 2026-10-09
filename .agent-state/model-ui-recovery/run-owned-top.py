#!/usr/bin/env python3
"""Task-owned full fixture, exact top, actual waits and 75-second TCP tail."""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time
import uuid
from owned_resources import collect, merge, publish

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "output/ai/model-ui-recovery"
DELIVERY = Path("/workspace/agenteam-delivery")
TOPS = {
    "configuration": "TestAccountProjectOwnerModelsWebConfigurationLifecycle",
    "credential": "TestAccountProjectOwnerModelsWebCredentialLifecycle",
    "recovery": "TestAccountProjectOwnerModelsWebOriginalRecovery",
    "read": "TestAccountProjectOwnerModelsWebReadAndPagination",
    "authority": "TestAccountProjectOwnerModelsWebAuthorityAndIdentity",
    "navigation": "TestAccountProjectOwnerModelsWebNavigationAndLayouts",
}


def tcp():
    rows = set()
    for family in ("tcp", "tcp6"):
        for line in Path("/proc/net", family).read_text().splitlines()[1:]:
            fields = line.split()
            rows.add((family, fields[1], fields[2], fields[3], fields[9]))
    return rows


def descendants(root):
    parents = {}
    for path in Path("/proc").glob("[0-9]*/stat"):
        try:
            parents[int(path.parent.name)] = int(path.read_text().rsplit(")", 1)[1].split()[1])
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            pass
    owned = {root}
    while True:
        extra = {pid for pid, parent in parents.items() if parent in owned} - owned
        if not extra:
            return owned - {root}
        owned |= extra


def docker(*args):
    result = subprocess.run(["docker", *args], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
    return result.returncode, result.stdout


def absent(row):
    command = ["container", "inspect"] if row["kind"] == "container" else ["network", "inspect"]
    code, raw = docker(*command, row["id"])
    if code == 0:
        return False
    # A daemon failure is not an absent-resource observation.
    code, raw = docker("info", "--format", "{{.ServerVersion}}")
    if code != 0 or not raw.strip():
        raise RuntimeError("owned Docker absence observation unavailable")
    code, raw = docker(*( ["ps", "-aq", "--no-trunc"] if row["kind"] == "container" else ["network", "ls", "-q", "--no-trunc"] ))
    return code == 0 and row["id"].encode() not in raw.splitlines()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", choices=TOPS, required=True)
    args = parser.parse_args()
    os.umask(0o077)
    available = next(int(row.split()[1]) * 1024 for row in Path("/proc/meminfo").read_text().splitlines() if row.startswith("MemAvailable:"))
    if available < 5 * 1024**3:
        raise RuntimeError("fresh host MemAvailable five GiB gate failed")
    nonce = uuid.uuid4().hex
    evidence = OUTPUT / ("owned-" + args.case + "-" + nonce)
    evidence.mkdir(mode=0o700)
    private = Path("/tmp") / ("models-" + nonce[:12])
    private.mkdir(mode=0o700)
    runtime = private / "browser"
    runtime.mkdir(mode=0o700)
    helpers = [OUTPUT / "helpers" / name for name in ("object-fixture", "outbound-fixture", "outbound-server", "postgres-fixture")]
    sources = [ROOT / "tests/account/project_owner_models_web_fixture_test.go", ROOT / "tests/account/project_owner_models_web_test.go", ROOT / "tests/account-captcha-web/e2e/project-owner-models.spec.ts", ROOT / "tests/account-captcha-web/project-owner-models.config.js", *Path(__file__).parent.glob("*.py"), *Path(__file__).parent.glob("*.ts"), *Path(__file__).parent.glob("*.mjs")]
    binaries = [OUTPUT / "account-delivery.test", OUTPUT / "client-probe/native-client-probe.js", *helpers]
    formal = [ROOT / "api/openapi" / name for name in ("common.json", "project-models.json", "project-model-credentials.json")]
    formal += [ROOT / "docs/development/work-items" / name for name in ("d27-project-owner-model-settings-ui.md", "d27-project-owner-model-settings-ui-endpoints.json")]
    assets = [path for path in (OUTPUT / "dist").rglob("*") if path.is_file()]
    inputs = {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in [*sources, *binaries, *formal, *assets]}
    for source in sources[:2]:
        assert source.read_bytes() == (DELIVERY / source.relative_to(ROOT)).read_bytes()
    frozen_hash = hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest()
    (evidence / "inputs.json").write_text(json.dumps({"files": inputs, "input_hash": frozen_hash, "go_source_tree": str(DELIVERY), "production_dist": str(OUTPUT / "dist"), "host_mem_available": available, "memory_gate_is_not_cgroup_hard_limit": True}, indent=2))
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(36, 1, 0, 0, 0) != 0:
        raise OSError(ctypes.get_errno(), "PR_SET_CHILD_SUBREAPER")
    env = {key: value for key, value in os.environ.items() if not key.startswith(("AGENTEAM_", "MODELS_"))}
    env.update({"AGENTEAM_GO": str(Path(__file__).with_name("fixture-go.py")), "MODELS_PRIVATE_ROOT": str(private), "MODELS_EXACT_SELECTOR": "^" + TOPS[args.case] + "$", "TMPDIR": str(private), "GOTOOLCHAIN": "local", "AGENTEAM_MINIO_BINARY": str(ROOT / "output/ai/deps-minio/bin/minio"), "AGENTEAM_AUTH_WEB_RUNTIME": str(runtime), "AGENTEAM_AUTH_WEB_IMAGES": str(evidence / "images"), "AGENTEAM_PROJECT_MODELS_WEB_DIST": str(OUTPUT / "dist"), "AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE": str(evidence), "AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH": frozen_hash})
    baseline = tcp()
    started = time.monotonic()
    stop = threading.Event()
    expired = threading.Event()
    child = None
    code = 1
    adopted = []
    registered = {}
    capture_failed = threading.Event()
    old = {}
    with (evidence / "runtime.log").open("x", buffering=1) as log:
        def watchdog():
            if not stop.wait(120):
                expired.set()
                if child is not None and child.poll() is None:
                    child.send_signal(signal.SIGTERM)
        def interrupt(signum, frame):
            expired.set()
            if child is not None and child.poll() is None:
                child.send_signal(signal.SIGTERM)
        for sig in (signal.SIGTERM, signal.SIGINT):
            old[sig] = signal.signal(sig, interrupt)
        thread = threading.Thread(target=watchdog, name="models-owned-watchdog")
        def capture():
            try:
                while True:
                    if merge(registered, collect(private)):
                        publish(evidence / "resources.json", registered)
                    if stop.wait(.03):
                        break
            except Exception:
                capture_failed.set()
        capture_thread = threading.Thread(target=capture, name="models-owned-resource-observer")
        try:
            child = subprocess.Popen([str(helpers[0]), "-run", env["MODELS_EXACT_SELECTOR"]], cwd=DELIVERY, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            thread.start()
            capture_thread.start()
            try:
                code = child.wait(timeout=160)
            except subprocess.TimeoutExpired:
                expired.set()
                for pid in descendants(os.getpid()):
                    try:
                        os.kill(pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                code = child.wait()
            stop.set()
            thread.join()
            capture_thread.join()
            log.write(f"SUPERVISOR direct_actual_wait={child.pid} exit={code} watchdog_joined={not thread.is_alive()}\n")
            log.write(f"SUPERVISOR resource_observer_actual_join={not capture_thread.is_alive()}\n")
            survivors = descendants(os.getpid())
            if survivors:
                # Chromium can legitimately leave exited adopted grandchildren;
                # reap actual statuses, and reject only running survivors.
                for pid in survivors:
                    try:
                        state = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()[0]
                        if state != "Z":
                            code = 1
                            os.kill(pid, signal.SIGKILL)
                    except (FileNotFoundError, ProcessLookupError):
                        pass
            while True:
                try:
                    pid, status = os.waitpid(-1, 0)
                    adopted.append({"pid": pid, "status": status})
                    log.write(f"SUPERVISOR adopted_actual_wait={pid} status={status}\n")
                except ChildProcessError:
                    break
            if expired.is_set() or time.monotonic() - started > 120:
                code = 1
            resources_path = evidence / "resources.json"
            adapter = evidence / "adapter-resources.json"
            if adapter.exists():
                merge(registered, json.loads(adapter.read_bytes()))
                publish(resources_path, registered)
            resources = list(registered.values())
            if capture_failed.is_set():
                code = 1
            if len(resources) != 7:
                code = 1
            for observation in (1, 2):
                absent_ids = [row["id"] for row in resources if absent(row)]
                running = sorted(descendants(os.getpid()))
                log.write(json.dumps({"retirement_observation": observation, "exact_absent": absent_ids, "descendants": running}) + "\n")
                if len(absent_ids) != 7 or running:
                    code = 1
            # Helpers own removal of their exact resources. Failure does not
            # authorize guessed cleanup or operation on unrelated containers.
            remaining = [p.name for p in private.iterdir() if p != runtime]
            runtime_empty = not (remaining or list(runtime.iterdir()))
            if not runtime_empty:
                code = 1
            # Retained private diagnostics are never copied to evidence. After
            # every owned process actually retired, remove only this new root.
            import shutil
            shutil.rmtree(private)
            private_removed = not private.exists()
            if not private_removed:
                code = 1
            deadline = time.monotonic() + 75
            empty = 0
            while time.monotonic() < deadline and empty < 2:
                delta = tcp() - baseline
                if delta:
                    empty = 0
                else:
                    empty += 1
                    log.write(f"HOST_TCP empty_delta_observation={empty}\n")
                if empty < 2:
                    time.sleep(.1)
            if empty != 2:
                code = 1
                log.write(f"HOST_TCP tail_not_empty_rows={len(tcp()-baseline)}\n")
            unchanged = all(Path(path).is_file() and hashlib.sha256(Path(path).read_bytes()).hexdigest() == digest for path, digest in inputs.items())
            if not unchanged:
                code = 1
            log.write(f"SUPERVISOR frozen_inputs_unchanged={unchanged} terminal={code}\n")
            terminal = {"case": args.case, "selector": env["MODELS_EXACT_SELECTOR"], "exit": code, "elapsed_seconds": time.monotonic() - started, "direct_actual_wait": child.returncode is not None, "adopted_actual_waits": adopted, "watchdog_joined": not thread.is_alive(), "runtime_empty": runtime_empty, "private_removed": private_removed, "tcp_empty_observations": empty, "inputs_unchanged": unchanged, "resources": len(resources)}
            (evidence / "terminal.json").write_text(json.dumps(terminal, indent=2))
        finally:
            stop.set()
            if thread.ident is not None:
                thread.join()
            if capture_thread.ident is not None:
                capture_thread.join()
            for sig, handler in old.items():
                signal.signal(sig, handler)
    print(json.dumps({"evidence": str(evidence), "exit": code}), flush=True)
    return code


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:
        print("MODELS_OWNED_SUPERVISOR_FAILED", file=sys.stderr)
        sys.exit(1)
