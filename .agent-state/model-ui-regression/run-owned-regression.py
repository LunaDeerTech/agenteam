#!/usr/bin/env python3
"""One scheduled D27 old top, with owned resources and reversible asset lease.

The root must grant an exclusive resource/asset window before invocation. The
local lock serializes this driver; it cannot grant ownership over other tools.
There is no batch, retry, arbitrary selector or restoration-before-retirement.
"""
import argparse
import ctypes
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import signal
import stat
import subprocess
import sys
import threading
import time
import uuid

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
SOURCE = Path(__file__).resolve().parent
ACCEPTED = ROOT / "output/ai/model-ui-recovery"
OUTPUT = ROOT / "output/ai/model-ui-regression"
DELIVERY = ROOT
DIST = ACCEPTED / "dist"
spec = importlib.util.spec_from_file_location("regression_fixture", SOURCE / "fixture-go.py")
adapter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(adapter)
from owned_resources import collect, merge, publish

TOPS = adapter.TOPS
require = adapter.require


def identity(path):
    info = path.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and path.resolve() == path, "REGRESSION_DIRECTORY_IDENTITY_INVALID")
    return [info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid]


def tree(path):
    identity(path)
    result = {}
    for entry in sorted(path.rglob("*")):
        info = entry.lstat()
        require(stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode), "REGRESSION_ASSET_NOT_REGULAR")
        if stat.S_ISREG(info.st_mode):
            result[entry.relative_to(path).as_posix()] = hashlib.sha256(entry.read_bytes()).hexdigest()
    require("index.html" in result and any(name.startswith("assets/") and name.endswith(".js") for name in result), "REGRESSION_ASSETS_MISSING")
    return result


def write_json(path, value):
    temporary = path.with_name(path.name + ".tmp")
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as output:
        json.dump(value, output, indent=2)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, path)


class AssetLease:
    """Only rename the original directory; never replace an unknown identity."""

    def __init__(self, destination, source, nonce, evidence):
        self.destination, self.source = destination, source
        self.backup = destination.with_name(".dist-regression-original-" + nonce)
        self.stage = destination.with_name(".dist-regression-stage-" + nonce)
        self.record = evidence / "asset-lease.json"
        self.parent_id = identity(destination.parent)
        self.original_id = identity(destination)
        self.original = tree(destination)
        self.expected = tree(source)
        self.installed_id = None
        self.state = "prepared"
        require(not self.backup.exists() and not self.backup.is_symlink() and not self.stage.exists() and not self.stage.is_symlink(), "REGRESSION_ASSET_SLOT_EXISTS")
        self.save()

    def save(self):
        write_json(self.record, {"state": self.state, "destination": str(self.destination), "source": str(self.source), "backup": str(self.backup), "stage": str(self.stage), "parent_identity": self.parent_id, "original_identity": self.original_id, "installed_identity": self.installed_id, "original_files": self.original, "installed_files": self.expected})

    def install(self):
        self.stage.mkdir(mode=0o700)
        self.installed_id = identity(self.stage)
        self.save()
        # tree() rejected links/special files. copytree does not retain links.
        shutil.copytree(self.source, self.stage, dirs_exist_ok=True)
        # copytree may copy source directory mode; retain the actual identity.
        self.installed_id = identity(self.stage)
        require(tree(self.stage) == self.expected and tree(self.source) == self.expected, "REGRESSION_ASSET_COPY_CHANGED")
        require(identity(self.destination.parent) == self.parent_id and identity(self.destination) == self.original_id and tree(self.destination) == self.original, "REGRESSION_ORIGINAL_ASSET_CHANGED")
        self.save()
        self.destination.rename(self.backup)
        self.state = "original-backed-up"
        self.save()
        self.stage.rename(self.destination)
        self.state = "installed"
        self.save()
        self.check()

    def check(self):
        require(identity(self.destination.parent) == self.parent_id and identity(self.backup) == self.original_id and tree(self.backup) == self.original, "REGRESSION_ASSET_BACKUP_CHANGED")
        require(identity(self.destination) == self.installed_id and tree(self.destination) == self.expected and tree(self.source) == self.expected, "REGRESSION_INSTALLED_ASSET_CHANGED")

    def restore(self, readers_retired):
        require(readers_retired, "REGRESSION_ASSET_READERS_NOT_RETIRED")
        require(identity(self.destination.parent) == self.parent_id, "REGRESSION_ASSET_PARENT_CHANGED")
        present = self.destination.exists() or self.destination.is_symlink()
        if present and identity(self.destination) == self.original_id:
            require(not self.backup.exists() and tree(self.destination) == self.original, "REGRESSION_ORIGINAL_RESTORE_CHANGED")
        else:
            require(identity(self.backup) == self.original_id and tree(self.backup) == self.original, "REGRESSION_ASSET_BACKUP_CHANGED")
            if present:
                self.check()
                require(not self.stage.exists() and not self.stage.is_symlink(), "REGRESSION_ASSET_STAGE_OCCUPIED")
                self.destination.rename(self.stage)
            self.backup.rename(self.destination)
        require(identity(self.destination) == self.original_id and tree(self.destination) == self.original, "REGRESSION_ASSET_RESTORE_FAILED")
        if self.stage.exists() or self.stage.is_symlink():
            require(identity(self.stage) == self.installed_id, "REGRESSION_ASSET_STAGE_CHANGED")
            # The owned stage can be incomplete if installation failed early.
            # Refuse links and unknown contents; leave them for the owner.
            for entry in self.stage.rglob("*"):
                require(not entry.is_symlink() and (entry.is_dir() or entry.is_file()), "REGRESSION_ASSET_STAGE_UNSAFE")
                if entry.is_file():
                    name = entry.relative_to(self.stage).as_posix()
                    require(name in self.expected and hashlib.sha256(entry.read_bytes()).hexdigest() == self.expected[name], "REGRESSION_ASSET_STAGE_CHANGED")
            shutil.rmtree(self.stage)
        self.state = "restored"
        self.save()


def environment(group, nonce, evidence, private, frozen_hash):
    env = {key: value for key, value in os.environ.items() if not key.startswith(("AGENTEAM_", "MODELS_", "REGRESSION_")) and key not in ("DEBUG", "PWDEBUG")}
    env.update({"AGENTEAM_GO": str(SOURCE / "fixture-go.py"), "REGRESSION_GROUP": group, "REGRESSION_NONCE": nonce, "REGRESSION_EXACT_SELECTOR": adapter.selection(group), "REGRESSION_PRIVATE_ROOT": str(private), "REGRESSION_EVIDENCE": str(evidence), "TMPDIR": str(private), "GOTOOLCHAIN": "local", "PYTHONDONTWRITEBYTECODE": "1", "AGENTEAM_MINIO_BINARY": str(ROOT / "output/ai/deps-minio/bin/minio"), "AGENTEAM_AUTH_WEB_RUNTIME": str(private / "browser"), "AGENTEAM_AUTH_WEB_IMAGES": str(evidence / "images")})
    asset = TOPS[group][3]
    if asset in ("owner", "audit"):
        prefix = "AGENTEAM_PROJECT_" + ("OWNER" if asset == "owner" else "AUDIT") + "_WEB_"
        env.update({prefix + "DIST": str(DIST), prefix + "EVIDENCE": str(evidence), prefix + "INPUT_HASH": frozen_hash})
    elif asset == "summary":
        env["AGENTEAM_MEETING_SUMMARY_WEB_DIST"] = str(DIST)
    return env


def frozen_inputs(group):
    _, family, browser, _ = TOPS[group]
    # These are the nine known old families and their shared fixture methods,
    # not a generated repository/dependency-closure manifest.
    families = sorted({value[1] for value in TOPS.values()})
    go_sources = [ROOT / "tests/account" / (name + "_web" + suffix) for name in families for suffix in ("_fixture_test.go", "_test.go")]
    for path in go_sources:
        require(path.read_bytes() == (DELIVERY / path.relative_to(ROOT)).read_bytes(), "REGRESSION_DELIVERY_SOURCE_MISMATCH")
    paths = [*go_sources, SOURCE / "run-owned-regression.py", SOURCE / "fixture-go.py", ROOT / ".agent-state/model-ui-recovery/owned_resources.py", ROOT / "scripts/test-security.sh", ROOT / "scripts/test-postgres.sh", ROOT / "tests/account-captcha-web" / (browser + ".config.js"), ROOT / "tests/account-captcha-web/e2e" / (browser + ".spec.ts"), ROOT / "tests/account-captcha-web/package-lock.json", ACCEPTED / "account-delivery.test", ROOT / "output/ai/deps-minio/bin/minio", ROOT / "docs/development/work-items/d27-project-owner-model-settings-ui.md"]
    paths += [ACCEPTED / "helpers" / name for name in adapter.BUILD.values()]
    if family == "authentication":
        paths += [ROOT / "tests/account-captcha-web/e2e" / name for name in ("public-solver.ts", "drag-geometry.ts")]
    if family == "personal_settings":
        paths += [ROOT / "internal/central/account/testdata/b04-avatar/lossless.webp"]
    if family in ("project_owner", "project_owner_audit"):
        names = ["client", "account", "system-account", "project-owner"] if family == "project_owner" else ["client", "account", "system-account", "system-audit", "system-audit-metadata", "project-audit", "project-audit-metadata"]
        paths += [ROOT / "web/src/api" / (name + ".ts") for name in names]
        schemas = ["common.json", "project-owner.json", "project-usage.json"] if family == "project_owner" else ["common.json", "project-audit.json"]
        paths += [ROOT / "api/openapi" / name for name in schemas]
    inputs = {}
    for path in dict.fromkeys(paths):
        require(stat.S_ISREG(path.lstat().st_mode), "REGRESSION_INPUT_NOT_REGULAR")
        inputs[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
    assets = tree(DIST)
    for relative, digest in assets.items():
        inputs[str(DIST / relative)] = digest
    return inputs, assets


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
    code, _ = docker(*command, row["id"])
    if code == 0:
        return False
    code, raw = docker("info", "--format", "{{.ServerVersion}}")
    require(code == 0 and raw.strip(), "REGRESSION_DOCKER_ABSENCE_UNAVAILABLE")
    code, raw = docker(*(["ps", "-aq", "--no-trunc"] if row["kind"] == "container" else ["network", "ls", "-q", "--no-trunc"]))
    return code == 0 and row["id"].encode() not in raw.splitlines()


def run_owned(env, private, evidence):
    baseline, started = tcp(), time.monotonic()
    private_id, runtime_id = identity(private), identity(private / "browser")
    stop, expired, capture_failed = threading.Event(), threading.Event(), threading.Event()
    child, code, waited, reaped = None, 1, False, False
    adopted, registered, old = [], {}, {}
    facts = {"started": False, "retirement_complete": False}
    with (evidence / "runtime.log").open("x", buffering=1) as log:
        def terminate():
            if child is not None and child.poll() is None:
                try:
                    child.send_signal(signal.SIGTERM)
                except ProcessLookupError:
                    pass

        def watchdog():
            if not stop.wait(max(0, 120 - (time.monotonic() - started))):
                expired.set()
                terminate()

        def interrupt(signum, frame):
            expired.set()
            terminate()

        def capture():
            try:
                while True:
                    if merge(registered, collect(private)):
                        publish(evidence / "resources.json", registered)
                    if stop.wait(.03):
                        return
            except Exception:
                capture_failed.set()

        watchdog_thread = threading.Thread(target=watchdog, name="regression-owned-watchdog")
        observer_thread = threading.Thread(target=capture, name="regression-resource-observer")
        for sig in (signal.SIGTERM, signal.SIGINT):
            old[sig] = signal.signal(sig, interrupt)
        try:
            child = subprocess.Popen([str(ACCEPTED / "helpers/object-fixture"), "-run", env["REGRESSION_EXACT_SELECTOR"]], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            facts["started"] = True
            watchdog_thread.start()
            observer_thread.start()
            code = child.wait(timeout=160)
            waited = True
        except Exception:
            code = 1
            log.write("SUPERVISOR execution_failed\n")
        finally:
            if child is not None and not waited:
                expired.set()
                terminate()
                try:
                    child.wait(timeout=max(.1, 160 - (time.monotonic() - started)))
                    waited = True
                except subprocess.TimeoutExpired:
                    for pid in descendants(os.getpid()):
                        try:
                            os.kill(pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                    try:
                        child.wait(timeout=10)
                        waited = True
                    except subprocess.TimeoutExpired:
                        pass
            stop.set()
            for thread in (watchdog_thread, observer_thread):
                if thread.ident is not None:
                    thread.join()
            for sig, handler in old.items():
                signal.signal(sig, handler)
        # Reap actual adopted exits. A running survivor makes the round fail,
        # even when its subsequent exact-descendant kill and wait succeed.
        reap_deadline = time.monotonic() + 10
        while time.monotonic() < reap_deadline:
            for pid in descendants(os.getpid()):
                try:
                    state = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()[0]
                    if state != "Z":
                        code = 1
                        os.kill(pid, signal.SIGKILL)
                except (FileNotFoundError, ProcessLookupError):
                    pass
            try:
                pid, status = os.waitpid(-1, os.WNOHANG)
                if pid:
                    adopted.append({"pid": pid, "status": status})
                    log.write(f"SUPERVISOR adopted_actual_wait={pid} status={status}\n")
                else:
                    time.sleep(.02)
            except ChildProcessError:
                reaped = True
                break
        within_budget = not expired.is_set() and time.monotonic() - started <= 120
        if not within_budget or not waited or not reaped or capture_failed.is_set():
            code = 1
        facts.update({"direct_actual_wait": waited, "direct_exit": None if child is None else child.returncode, "adopted_actual_waits": adopted, "adopted_wait_complete": reaped, "watchdog_joined": not watchdog_thread.is_alive(), "observer_joined": not observer_thread.is_alive(), "top_within_120_seconds": within_budget})
        log.write("SUPERVISOR " + json.dumps(facts) + "\n")
        adapter_file = evidence / "adapter-resources.json"
        if adapter_file.exists():
            merge(registered, json.loads(adapter_file.read_bytes()))
            publish(evidence / "resources.json", registered)
        shape = adapter.resource_shape(registered)
        observations = []
        for number in (1, 2):
            gone = [row["id"] for row in registered.values() if absent(row)]
            running = sorted(descendants(os.getpid()))
            observation = {"retirement_observation": number, "exact_absent": gone, "descendants": running}
            log.write(json.dumps(observation) + "\n")
            observations.append(shape and len(gone) == 7 and not running)
        runtime = private / "browser"
        runtime_empty = identity(private) == private_id and identity(runtime) == runtime_id and not list(runtime.iterdir()) and list(private.iterdir()) == [runtime]
        if runtime_empty and reaped and waited:
            runtime.rmdir()
            private.rmdir()
        private_removed = not private.exists()
        # Resource checks and owned runtime removal belong to the original top;
        # the separate TCP observation tail can never turn an overrun into PASS.
        within_budget = within_budget and time.monotonic() - started <= 120
        if not within_budget:
            code = 1
        deadline, empty = time.monotonic() + 75, 0
        while time.monotonic() < deadline and empty < 2:
            if tcp() - baseline:
                empty = 0
            else:
                empty += 1
                log.write(f"HOST_TCP empty_delta_observation={empty}\n")
            if empty < 2:
                time.sleep(.1)
        retired = waited and reaped and all(observations) and runtime_empty and private_removed and empty == 2 and not descendants(os.getpid())
        if not retired:
            code = 1
        log.flush()
        output = (evidence / "runtime.log").read_text()
        top = env["REGRESSION_EXACT_SELECTOR"][1:-1]
        exact_top_pass = len(re.findall(r"^=== RUN   " + re.escape(top) + r"$", output, re.MULTILINE)) == 1 and len(re.findall(r"^--- PASS: " + re.escape(top) + r" \(", output, re.MULTILINE)) == 1 and "--- SKIP:" not in output and "no tests to run" not in output
        if not exact_top_pass:
            code = 1
        facts.update({"resources": len(registered), "resource_shape_valid": shape, "exact_absent_observations": sum(observations), "runtime_empty": runtime_empty, "private_removed": private_removed, "tcp_empty_observations": empty, "retirement_complete": retired, "top_within_120_seconds": within_budget, "exact_top_pass": exact_top_pass, "elapsed_seconds": time.monotonic() - started})
        return code, facts


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--group", choices=TOPS, required=True)
    group = parser.parse_args().group
    os.umask(0o077)
    OUTPUT.mkdir(mode=0o700, parents=True, exist_ok=True)
    identity(OUTPUT)
    lock = os.open(OUTPUT / "driver.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    lock_info = os.fstat(lock)
    require(stat.S_ISREG(lock_info.st_mode) and lock_info.st_uid == os.getuid() and stat.S_IMODE(lock_info.st_mode) == 0o600, "REGRESSION_LOCK_INVALID")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    active = OUTPUT / "active-run.json"
    require(not active.exists() and not active.is_symlink(), "REGRESSION_PREVIOUS_RUN_NOT_RETIRED")
    nonce = uuid.uuid4().hex
    evidence = OUTPUT / ("owned-" + group + "-" + nonce)
    evidence.mkdir(mode=0o700)
    write_json(active, {"group": group, "nonce": nonce, "evidence": str(evidence)})
    active_id = (active.stat().st_dev, active.stat().st_ino)
    private = Path("/tmp") / ("regress-" + nonce[:12])
    private_id = runtime_id = None
    lease, launched, retired, restored, code = None, False, False, False, 1
    facts = {"started": False, "retirement_complete": False}
    try:
        free = shutil.disk_usage(ROOT).free
        available = next(int(row.split()[1]) * 1024 for row in Path("/proc/meminfo").read_text().splitlines() if row.startswith("MemAvailable:"))
        require(free >= 5 * 1024**3 and shutil.disk_usage("/tmp").free >= 5 * 1024**3 and available >= 5 * 1024**3, "REGRESSION_FRESH_FIVE_GIB_GATE_FAILED")
        inputs, assets = frozen_inputs(group)
        frozen_hash = hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest()
        write_json(evidence / "inputs.json", {"files": inputs, "input_hash": frozen_hash, "go_source_tree": str(DELIVERY), "fixture_cwd": str(ROOT), "account_cwd": str(ROOT / "tests/account"), "production_dist": str(DIST), "fresh_disk_bytes": free, "host_mem_available": available, "memory_gate_is_not_cgroup_hard_limit": True})
        if TOPS[group][3] == "global":
            lease = AssetLease(ROOT / "web/dist", DIST, nonce, evidence)
            lease.install()
        private.mkdir(mode=0o700)
        private_id = identity(private)
        (private / "browser").mkdir(mode=0o700)
        runtime_id = identity(private / "browser")
        (evidence / "images").mkdir(mode=0o700)
        libc = ctypes.CDLL(None, use_errno=True)
        require(libc.prctl(36, 1, 0, 0, 0) == 0, "REGRESSION_SUBREAPER_UNAVAILABLE")
        env = environment(group, nonce, evidence, private, frozen_hash)
        launched = True  # Any exception inside run_owned keeps the lease held.
        code, facts = run_owned(env, private, evidence)
        retired = facts["retirement_complete"]
        unchanged = all(Path(path).is_file() and hashlib.sha256(Path(path).read_bytes()).hexdigest() == digest for path, digest in inputs.items()) and tree(DIST) == assets
        if lease is not None:
            lease.check()
        facts["inputs_unchanged"] = unchanged
        if not unchanged:
            code = 1
    except Exception:
        code = 1
        facts["failure"] = "REGRESSION_OWNED_SUPERVISOR_FAILED"
    finally:
        # A pre-launch failure has no readers. Any unproven execution retirement
        # retains the exact original backup, installed assets and active marker.
        can_restore = retired or not launched
        try:
            if lease is not None:
                lease.restore(can_restore)
            restored = lease is None or lease.state == "restored"
            if not launched and private_id is not None:
                require(identity(private) == private_id, "REGRESSION_PRIVATE_IDENTITY_CHANGED")
                runtime = private / "browser"
                if runtime_id is not None:
                    require(identity(runtime) == runtime_id, "REGRESSION_RUNTIME_IDENTITY_CHANGED")
                    runtime.rmdir()
                private.rmdir()
        except Exception:
            code = 1
            facts["asset_failure"] = "REGRESSION_ASSET_LEASE_RETAINED"
        facts.update({"group": group, "selector": adapter.selection(group), "exit": code, "assets_restored": restored, "asset_lease_required": TOPS[group][3] == "global"})
        write_json(evidence / "terminal.json", facts)
        if can_restore and restored and not private.exists():
            require((active.stat().st_dev, active.stat().st_ino) == active_id, "REGRESSION_ACTIVE_MARKER_CHANGED")
            active.unlink()
        os.close(lock)
    print(json.dumps({"evidence": str(evidence), "exit": code}), flush=True)
    return code


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:
        print("REGRESSION_OWNED_SUPERVISOR_FAILED", file=sys.stderr)
        sys.exit(1)
