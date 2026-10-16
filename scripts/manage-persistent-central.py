#!/usr/bin/env python3
"""Manage only one prepared production Central process; never delete its data.

The operator starts/stops the separately owned database, object store and TLS
proxy. This manager does not manipulate those services, frontend or frp.

stage --candidate-binary PATH --source-revision FULL_COMMIT creates a separate
runtime manifest without stopping Central or changing its original preparation.
Stop with the current manifest, then start with the returned staged manifest.
Once a new binary launch is attempted, earlier manifests cannot restart it:
migrations may already have committed. Keep Central stopped on upgrade failure
until a compatible forward repair is ready; this tool never restores old data.
"""

import argparse
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import stat
import subprocess
import tempfile
import time


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def read_private(path):
    path = Path(path)
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600 or info.st_uid != os.getuid():
        raise RuntimeError("private_regular_file_required")
    return json.loads(path.read_text())


def write_private(path, value):
    temporary = path.with_name(path.name + ".next")
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(value, stream)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    finally:
        if temporary.exists():
            temporary.unlink()


def process(pid):
    try:
        fields = (Path("/proc") / str(pid) / "stat").read_text().split(") ", 1)[1].split()
    except FileNotFoundError:
        return None
    return {"pid": pid, "state": fields[0], "parent_pid": int(fields[1]), "session_id": int(fields[3]),
            "start_ticks": int(fields[19]), "exit_status": int(fields[49])}


def same_process(record):
    current = process(record["pid"])
    if current is None or current["start_ticks"] != record["start_ticks"]:
        return None
    return current


def active(record):
    current = same_process(record)
    return current is not None and current["state"] not in ("Z", "X")


def configuration(runtime):
    environment = read_private(runtime["environment_file"])
    if not isinstance(environment, dict) or any(not isinstance(k, str) or not k.startswith("AGENTEAM_CENTRAL_") or not isinstance(v, str) for k, v in environment.items()):
        raise RuntimeError("invalid_central_environment")
    if environment.get("AGENTEAM_CENTRAL_DEV_MODE") != "false" or environment.get("AGENTEAM_CENTRAL_PUBLIC_ORIGIN") != runtime["origin"] or environment.get("AGENTEAM_CENTRAL_HTTP_ADDR") != runtime["central_address"]:
        raise RuntimeError("central_identity_mismatch")
    merged = {k: v for k, v in os.environ.items() if not k.startswith("AGENTEAM_")}
    merged.update(environment)
    return merged


def release(runtime):
    return {"source_revision": runtime["source_revision"], "binary_sha256": runtime["central_binary_sha256"]}


def check(binary, runtime):
    checked = subprocess.run([str(binary), "--check-config"], env=configuration(runtime), capture_output=True, timeout=20)
    if checked.returncode != 0:
        raise RuntimeError("central_check_config_failed")
    result = json.loads(checked.stdout)
    if result.get("valid") is not True:
        raise RuntimeError("central_check_config_invalid")
    return {"configuration_valid": True, "product_ready": result.get("ready")}


def stage(args, root, runtime, current):
    if current and release(runtime) != current:
        raise RuntimeError("stage_requires_current_runtime_manifest")
    if not args.candidate_binary or not re.fullmatch(r"[0-9a-f]{40}", args.source_revision or ""):
        raise RuntimeError("candidate_binary_and_full_source_revision_required")
    if args.source_revision == runtime["source_revision"]:
        raise RuntimeError("candidate_requires_new_source_revision")
    source = Path(args.candidate_binary).absolute()
    if not source.is_file() or source.is_symlink() or not os.access(source, os.X_OK):
        raise RuntimeError("regular_executable_candidate_required")
    digest = hashlib.sha256(source.read_bytes()).hexdigest()
    directory = root / "bin"
    info = directory.lstat()
    if not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o700 or info.st_uid != os.getuid():
        raise RuntimeError("owned_private_binary_directory_required")
    binary = directory / ("agenteam-" + args.source_revision)
    filename = root / ("runtime-staged-" + args.source_revision + ".json")
    candidate = dict(runtime, source_revision=args.source_revision, central_binary=str(binary),
                     central_binary_sha256=digest, upgrade_from=release(runtime))
    if filename.exists() and read_private(filename) != candidate:
        raise RuntimeError("staged_revision_already_has_different_inputs")
    if binary.exists():
        info = binary.lstat()
        if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o700 or info.st_uid != os.getuid() or hashlib.sha256(binary.read_bytes()).hexdigest() != digest:
            raise RuntimeError("staged_binary_identity_mismatch")
    else:
        temporary = None
        try:
            with tempfile.NamedTemporaryFile(dir=directory, prefix=".central-stage-", delete=False) as output:
                temporary = Path(output.name)
                with source.open("rb") as input_file:
                    shutil.copyfileobj(input_file, output)
                output.flush()
                os.fchmod(output.fileno(), 0o700)
                os.fsync(output.fileno())
            if hashlib.sha256(temporary.read_bytes()).hexdigest() != digest:
                raise RuntimeError("candidate_changed_during_staging")
            os.link(temporary, binary)
            fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        finally:
            if temporary is not None:
                temporary.unlink(missing_ok=True)
    checked = check(binary, candidate)
    write_private(filename, candidate)
    return dict(checked, staged=True, started=False, active_runtime_unchanged=True,
                runtime_file=str(filename), source_revision=args.source_revision, binary_sha256=digest)


def run_locked(args, filename, root):
    runtime = read_private(filename)
    binary = Path(runtime["central_binary"])
    if not binary.is_file() or binary.is_symlink():
        raise RuntimeError("regular_central_binary_required")
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    if digest != runtime["central_binary_sha256"]:
        raise RuntimeError("central_binary_digest_mismatch")
    record_file = root / "central-process.json"
    record = read_private(record_file) if record_file.exists() else None
    active_file = root / "central-release.json"
    selected = read_private(active_file) if active_file.exists() else None
    current = ({k: selected[k] for k in ("source_revision", "binary_sha256")} if selected else
               {k: record[k] for k in ("source_revision", "binary_sha256")} if record else None)
    if args.action == "status":
        return {"origin": runtime["origin"], "source_revision": record["source_revision"] if record else runtime["source_revision"],
                "configured_source_revision": runtime["source_revision"],
                "active_runtime_file": selected["runtime_file"] if selected else str(filename),
                "process": same_process(record) if record else None, "running": bool(record and active(record))}
    if args.action == "check":
        return dict(check(binary, runtime), binary_sha256=digest)
    if args.action == "stage":
        return stage(args, root, runtime, current)
    if args.action == "stop":
        if current and release(runtime) != current:
            raise RuntimeError("stop_requires_current_runtime_manifest")
        if not record or not active(record):
            return {"already_stopped": True, "process": same_process(record) if record else None}
        if Path("/proc", str(record["pid"]), "exe").resolve() != binary.resolve():
            raise RuntimeError("central_executable_identity_mismatch")
        os.kill(record["pid"], signal.SIGTERM)
        deadline = time.monotonic() + 35
        while active(record) and time.monotonic() < deadline:
            time.sleep(0.2)
        if active(record):
            raise RuntimeError("central_did_not_exit_no_force_sent")
        current = same_process(record)
        record.update(stopped_at=now(), observed_exit=current)
        write_private(record_file, record)
        return {"stopped": True, "process": current, "data_preserved": True}
    if record and active(record):
        raise RuntimeError("original_central_process_still_running")
    if current and release(runtime) != current and runtime.get("upgrade_from") != current:
        raise RuntimeError("earlier_or_unstaged_release_start_forbidden")
    host, port = runtime["central_address"].rsplit(":", 1)
    if host != "127.0.0.1":
        raise RuntimeError("loopback_central_required")
    with socket.socket() as listener:
        # Match Go's listener behavior: completed connections may remain in
        # TIME_WAIT after a graceful stop, without an active service owning it.
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind((host, int(port)))
    log_file = root / "central.log"
    fd = os.open(log_file, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW, 0o600)
    offset = os.fstat(fd).st_size
    with os.fdopen(fd, "ab") as output:
        environment = configuration(runtime)
        # Persist before Popen: even a failed launch may already have migrated
        # the database. An old binary must never be selected automatically.
        write_private(active_file, dict(release(runtime), runtime_file=str(filename), launch_attempted_at=now()))
        child = subprocess.Popen([str(binary)], cwd=root, env=environment, stdin=subprocess.DEVNULL,
                                 stdout=output, stderr=subprocess.STDOUT, start_new_session=True, close_fds=True)
    try:
        current = process(child.pid)
        if current is None:
            raise RuntimeError("central_exited_during_launch")
        record = dict(current, started_at=now(), binary_sha256=digest, log_file=str(log_file), log_offset=offset,
                      source_revision=runtime["source_revision"], runtime_file=str(filename))
        write_private(record_file, record)
    except BaseException:
        # A launch without its process record must not survive this manager.
        # Only this newly spawned child is ours to reap; the selected release
        # remains durable because its startup may already have migrated data.
        if child.poll() is None:
            child.terminate()
        try:
            child.wait(timeout=35)
        except subprocess.TimeoutExpired:
            child.kill()
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                raise RuntimeError("central_registration_failed_child_not_reaped") from None
        raise
    return {"started": True, "process": current, "source_revision": runtime["source_revision"],
            "log_file": str(log_file), "health_verified": False}


def run(args):
    filename = Path(args.runtime).absolute()
    root = filename.parent
    info = root.lstat()
    if not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o700 or info.st_uid != os.getuid() or root.resolve() != root:
        raise RuntimeError("owned_private_runtime_directory_required")
    if args.action in ("status", "check"):
        return run_locked(args, filename, root)
    fd = os.open(root / ".central-manager.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600 or info.st_uid != os.getuid():
            raise RuntimeError("private_manager_lock_required")
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("central_manager_busy") from None
        return run_locked(args, filename, root)
    finally:
        os.close(fd)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["check", "start", "status", "stop", "stage"])
    parser.add_argument("--runtime", required=True)
    parser.add_argument("--candidate-binary")
    parser.add_argument("--source-revision")
    arguments = parser.parse_args()
    try:
        print(json.dumps(run(arguments)))
    except Exception as error:
        print(json.dumps({"failed": str(error) if type(error) is RuntimeError else type(error).__name__}))
        raise SystemExit(1)
