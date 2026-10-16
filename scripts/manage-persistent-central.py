#!/usr/bin/env python3
"""Manage only one prepared production Central process; never delete its data.

The operator starts/stops the separately owned database, object store and TLS
proxy. This manager does not manipulate those services, frontend or frp.
"""

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import stat
import subprocess
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


def run(args):
    filename = Path(args.runtime).absolute()
    root = filename.parent
    info = root.lstat()
    if not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o700 or info.st_uid != os.getuid() or root.resolve() != root:
        raise RuntimeError("owned_private_runtime_directory_required")
    runtime = read_private(filename)
    binary = Path(runtime["central_binary"])
    if not binary.is_file() or binary.is_symlink():
        raise RuntimeError("regular_central_binary_required")
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    if digest != runtime["central_binary_sha256"]:
        raise RuntimeError("central_binary_digest_mismatch")
    record_file = root / "central-process.json"
    record = read_private(record_file) if record_file.exists() else None
    if args.action == "status":
        return {"origin": runtime["origin"], "source_revision": runtime["source_revision"],
                "process": same_process(record) if record else None, "running": bool(record and active(record))}
    if args.action == "check":
        checked = subprocess.run([str(binary), "--check-config"], env=configuration(runtime), capture_output=True, timeout=20)
        if checked.returncode != 0:
            raise RuntimeError("central_check_config_failed")
        result = json.loads(checked.stdout)
        if result.get("valid") is not True:
            raise RuntimeError("central_check_config_invalid")
        return {"configuration_valid": True, "product_ready": result.get("ready"), "binary_sha256": digest}
    if args.action == "stop":
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
    host, port = runtime["central_address"].rsplit(":", 1)
    if host != "127.0.0.1":
        raise RuntimeError("loopback_central_required")
    with socket.socket() as listener:
        listener.bind((host, int(port)))
    log_file = root / "central.log"
    fd = os.open(log_file, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW, 0o600)
    offset = os.fstat(fd).st_size
    with os.fdopen(fd, "ab") as output:
        child = subprocess.Popen([str(binary)], cwd=root, env=configuration(runtime), stdin=subprocess.DEVNULL,
                                 stdout=output, stderr=subprocess.STDOUT, start_new_session=True, close_fds=True)
    current = process(child.pid)
    if current is None:
        raise RuntimeError("central_exited_during_launch")
    record = dict(current, started_at=now(), binary_sha256=digest, log_file=str(log_file), log_offset=offset,
                  source_revision=runtime["source_revision"])
    write_private(record_file, record)
    return {"started": True, "process": current, "source_revision": runtime["source_revision"],
            "log_file": str(log_file), "health_verified": False}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["check", "start", "status", "stop"])
    parser.add_argument("--runtime", required=True)
    arguments = parser.parse_args()
    try:
        print(json.dumps(run(arguments)))
    except Exception as error:
        print(json.dumps({"failed": str(error) if type(error) is RuntimeError else type(error).__name__}))
        raise SystemExit(1)
