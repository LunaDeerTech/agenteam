#!/usr/bin/env python3
"""Linux/amd64 feasibility probe. Running main requires a fresh root window.

No build, server, socket, token, production hook, or controlled signal channel.
The caller supplies a separately built, pinned default cmd/agenteam-runner.
"""
import argparse
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import select
import signal
import stat
import subprocess
import time


class Failure(Exception):
    pass


def require(ok, reason):
    if not ok:
        raise Failure(reason)


def safe_number(value, maximum, base=10):
    # Closed numeric projection: never retain raw proc text or syscall addresses.
    if not isinstance(value, str) or not 1 <= len(value) <= 20:
        return None
    try:
        number = int(value, base)
    except ValueError:
        return None
    return number if 0 <= number <= maximum else None


def safe_syscall(fields):
    if fields == ["running"]:
        return {"state": "running", "number": None, "fd0": None}
    if fields == ["-1"]:
        return {"state": "not_in_syscall", "number": None, "fd0": None}
    number = safe_number(fields[0], 65535) if fields else None
    fd = safe_number(fields[1], 2**31 - 1, 0) if len(fields) >= 7 else None
    return {"state": "sampled" if number is not None else "unavailable",
            "number": number, "fd0": fd == 0 if fd is not None else None}


def safe_thread_sample(tid, before, wchan, after):
    allowed = {"pipe_read", "anon_pipe_read", "fifo_pipe_read", "0",
               "futex_wait_queue", "futex_wait_queue_me", "do_epoll_wait",
               "ep_poll", "ep_poll_callback"}
    return {"tid": safe_number(tid, 2**31 - 1),
            "before": safe_syscall(before), "after": safe_syscall(after),
            "same_syscall_sample": before == after,
            "wchan": wchan if wchan in allowed else "other"}


def safe_proc_error(error):
    if isinstance(error, PermissionError):
        return "permission"
    if isinstance(error, FileNotFoundError):
        return "missing"
    if isinstance(error, OSError):
        return "io"
    if isinstance(error, Failure):
        return "witness_condition"
    return "format"


def process_identity(pid):
    data = Path(f"/proc/{pid}/stat").read_text()
    end = data.rfind(")")
    fields = data[end + 2:].split()
    require(end > 0 and len(fields) >= 20, "proc_stat_shape")
    return int(fields[19]), fields[0]


def lock_owned(path, expected, original=None):
    fd = os.open(path, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        identity = (info.st_dev, info.st_ino)
        require(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600
                and info.st_uid == os.geteuid() and info.st_nlink == 1,
                "lock_file_identity")
        require(original is None or identity == original, "lock_inode_changed")
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as error:
            require(error.errno in (errno.EAGAIN, errno.EACCES), "lock_probe_error")
            owned = True
        else:
            owned = False
            fcntl.flock(fd, fcntl.LOCK_UN)
        require(owned == expected, "lock_owner_mismatch")
        return identity
    finally:
        os.close(fd)


class Child:
    def __init__(self, binary, directory, mode, emit):
        self.proc = None
        self.read_fd = self.write_fd = None
        self.streams = {}
        self.stderr = bytearray()
        self.events = []
        self.stdout_bytes = 0
        self.stderr_bytes = 0
        self.emit = emit
        self.mode = mode
        self.directory = directory
        self.lock_identity = None
        self.start_ticks = None
        self.read_snapshot = None
        self.binary_identity = (binary.stat().st_dev, binary.stat().st_ino)

    def start(self, binary):
        self.read_fd, self.write_fd = os.pipe2(os.O_CLOEXEC)
        # Only the inherited reader is deliberately blocking. No read deadline.
        os.set_blocking(self.read_fd, True)
        self.pipe_inode = os.fstat(self.read_fd).st_ino
        require(not fcntl.fcntl(self.read_fd, fcntl.F_GETFL) & os.O_NONBLOCK,
                "parent_stdin_not_blocking")
        env = {"PATH": os.environ["PATH"], "GOMAXPROCS": "2",
               "AGENTEAM_RUNNER_IDENTITY_FILE": str(self.directory / "identity.json"),
               "AGENTEAM_RUNNER_CENTRAL_URL": "https://runner-probe.invalid",
               "AGENTEAM_RUNNER_ID": "01900000-0000-7000-8000-000000000001",
               "AGENTEAM_RUNNER_ROOT_PATH": "/runner-probe"}
        if self.mode == "deadline":
            env["AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT"] = "3s"
        self.proc = subprocess.Popen([str(binary), "--enroll"], cwd=self.directory,
                                     env=env, stdin=self.read_fd, stdout=subprocess.PIPE,
                                     stderr=subprocess.PIPE, close_fds=True,
                                     start_new_session=True)
        os.close(self.read_fd)
        self.read_fd = None
        # No byte or EOF is supplied before the actual blocked-read witness.
        self.start_ticks, _ = process_identity(self.proc.pid)
        for stream, name in ((self.proc.stdout, "stdout"), (self.proc.stderr, "stderr")):
            os.set_blocking(stream.fileno(), False)
            self.streams[stream.fileno()] = (stream, name)
        self.emit("started", pid=self.proc.pid, start_ticks=self.start_ticks,
                  pipe_inode=self.pipe_inode)

    def live_identity(self):
        require(self.proc.poll() is None, "child_exited_before_witness")
        ticks, state = process_identity(self.proc.pid)
        require(ticks == self.start_ticks and state not in ("Z", "X"), "child_identity_changed")
        executable = Path(f"/proc/{self.proc.pid}/exe").stat()
        require((executable.st_dev, executable.st_ino) == self.binary_identity,
                "child_executable_changed")

    def pump(self, timeout):
        if not self.streams:
            # Poll interval only; never used as proof of entering or joining I/O.
            select.select([], [], [], max(0, timeout))
            return
        ready, _, _ = select.select(list(self.streams), [], [], max(0, timeout))
        for fd in ready:
            stream, name = self.streams[fd]
            try:
                raw = os.read(fd, 8192)
            except BlockingIOError:
                continue
            if not raw:
                stream.close()
                del self.streams[fd]
                continue
            if name == "stdout":
                self.stdout_bytes += len(raw)
                require(self.stdout_bytes == 0, "unexpected_stdout")
                continue
            self.stderr_bytes += len(raw)
            require(self.stderr_bytes <= 65536, "stderr_limit")
            self.stderr.extend(raw)
            while b"\n" in self.stderr:
                line, _, remainder = self.stderr.partition(b"\n")
                self.stderr = bytearray(remainder)
                event = json.loads(line)
                require(isinstance(event, dict), "invalid_log_record")
                # Keep no raw log text/configuration/token in persistent evidence.
                safe = {key: event[key] for key, values in {
                    "event": {"lifecycle", "shutdown"},
                    "phase": {"starting", "stopping"},
                    "outcome": {"drained", "forced"},
                    "code": {"SHUTDOWN_TIMEOUT", "FORCED_SHUTDOWN"},
                }.items() if isinstance(event.get(key), str) and event[key] in values}
                self.events.append(safe)
                require(len(self.events) <= 128, "log_record_limit")
                require(not any(event.get(key) is True for key in ("connected", "authenticated", "ready")),
                        "unexpected_network_readiness")
                self.emit("log_projection", record=safe)

    def blocked_read(self):
        # Reuse only reads in the original witness. No diagnostic re-read,
        # extra task scan, wait, or alternate acceptance condition is added.
        self.read_snapshot = {"pid": self.proc.pid, "start_ticks": self.start_ticks,
                              "identity_checked": False, "fd0_inode": None,
                              "fd0_flags": None, "task_limit": 128,
                              "current_tid": None, "scan_complete": False,
                              "threads": [], "error": None}
        try:
            return self._blocked_read()
        except Exception as error:
            self.read_snapshot["error"] = safe_proc_error(error)
            raise

    def _blocked_read(self):
        self.live_identity()
        self.read_snapshot["identity_checked"] = True
        proc = Path(f"/proc/{self.proc.pid}")
        link = os.readlink(proc / "fd/0")
        if link.startswith("pipe:[") and link.endswith("]"):
            self.read_snapshot["fd0_inode"] = safe_number(link[6:-1], 2**64 - 1)
        require(link == f"pipe:[{self.pipe_inode}]", "stdin_pipe_changed")
        fdinfo = dict(line.split(":", 1) for line in (proc / "fdinfo/0").read_text().splitlines())
        flags = int(fdinfo["flags"].strip(), 8)
        self.read_snapshot["fd0_flags"] = flags if 0 <= flags <= 2**32 - 1 else None
        require(flags & os.O_ACCMODE == os.O_RDONLY and not flags & os.O_NONBLOCK,
                "child_stdin_not_blocking")
        tasks = list((proc / "task").iterdir())
        require(len(tasks) <= 128, "task_scan_limit")
        for task in tasks:
            self.read_snapshot["current_tid"] = safe_number(task.name, 2**31 - 1)
            try:
                before = (task / "syscall").read_text().split()
                wchan = (task / "wchan").read_text().strip()
                after = (task / "syscall").read_text().split()
            except FileNotFoundError:
                self.read_snapshot["threads"].append({"tid": safe_number(task.name, 2**31 - 1),
                                                      "error": "missing"})
                continue  # A Go runtime thread can retire during a bounded scan.
            self.read_snapshot["threads"].append(safe_thread_sample(task.name, before, wchan, after))
            # Linux amd64 SYS_read=0, arg0=fd0. Do not persist buffer addresses.
            if (len(before) >= 7 and before == after and before[0] == "0"
                    and int(before[1], 0) == 0 and int(before[3], 0) > 0
                    and wchan == "pipe_read"):
                self.live_identity()
                require(os.readlink(proc / "fd/0") == f"pipe:[{self.pipe_inode}]",
                        "stdin_pipe_changed_after_snapshot")
                return {"tid": int(task.name), "syscall": "read", "fd": 0,
                        "wchan": wchan, "blocking": True}
        self.read_snapshot["scan_complete"] = True
        return None

    def emit_failed_read_snapshot(self):
        # Called only after original cleanup actually returns, never before kill
        # or Wait. A failed evidence write cannot replace the original failure.
        try:
            self.emit("failed_read_snapshot", available=self.read_snapshot is not None,
                      last_attempt=self.read_snapshot)
        except (OSError, ValueError, TypeError):
            print("runner_os_read_snapshot_unavailable", flush=True)

    def until(self, predicate, deadline, reason):
        while time.monotonic() < deadline:
            self.pump(min(0.01, max(0, deadline - time.monotonic())))
            value = predicate()
            require(time.monotonic() < deadline, reason)
            if value:
                return value
            require(self.proc.poll() is None, "child_exited_before_phase")
        raise Failure(reason)

    def signal(self, sig):
        self.live_identity()
        self.proc.send_signal(sig)
        self.emit("signal_sent", signal=sig.name)

    def wait(self, deadline):
        while self.proc.poll() is None or self.streams:
            require(time.monotonic() < deadline, "wait_or_stdio_deadline")
            self.pump(min(0.01, max(0, deadline - time.monotonic())))
        result = self.proc.wait(timeout=0)  # Actual wait status, also after poll reaped it.
        require(not self.stderr, "partial_log_record")
        self.emit("actual_wait", returncode=result, stdio_eof=True)
        return result

    def close_writer(self):
        if self.write_fd is not None:
            os.close(self.write_fd)
            self.write_fd = None
            self.emit("parent_writer_eof")

    def cleanup(self):
        # Preserve held EOF through the expected exit. Cleanup is never a pass path.
        deadline = time.monotonic() + 3
        forced = False
        if self.proc is not None and self.proc.poll() is None:
            self.proc.kill()  # Only our unreaped direct Popen child, never a guessed PID.
            forced = True
        self.close_writer()
        if self.read_fd is not None:
            os.close(self.read_fd)
            self.read_fd = None
        if self.proc is not None:
            self.proc.wait(timeout=max(0, deadline - time.monotonic()))
        # Closing owned parent pipe ends after Wait leaves no pump thread/copy owner.
        for stream in (self.proc.stdout, self.proc.stderr) if self.proc else ():
            if stream is not None and not stream.closed:
                stream.close()
        self.streams.clear()
        self.emit("cleanup", killed=forced, actual_waited=self.proc is not None,
                  parent_pipe_ends_closed=True)


def run_case(binary, directory, mode, emit):
    directory.mkdir(mode=0o700)
    child = Child(binary, directory, mode, emit)
    failed = False
    try:
        child.start(binary)
        witness = child.until(child.blocked_read, time.monotonic() + 5, "initial_read_not_observed")
        child.lock_identity = lock_owned(directory / "identity.json.lock", True)
        emit("initial_blocked_read", **witness)
        stopped_at = time.monotonic()
        child.signal(signal.SIGTERM)
        child.until(lambda: any(v.get("event") == "lifecycle" and v.get("phase") == "stopping"
                                for v in child.events), stopped_at + 1, "stopping_not_observed")
        witness = child.until(child.blocked_read, stopped_at + 2, "cancelled_read_not_observed")
        lock_owned(directory / "identity.json.lock", True, child.lock_identity)
        emit("blocked_after_stop", **witness)
        if mode == "eof":
            child.close_writer()
            code = child.wait(stopped_at + 5)
            require(code == 0 and any(v.get("outcome") == "drained" for v in child.events)
                    and not any(v.get("outcome") == "forced" for v in child.events), "eof_not_drained")
        else:
            forced_at = stopped_at
            if mode == "second_signal":
                forced_at = time.monotonic()
                child.signal(signal.SIGINT)
            # 3s original drain + 1s force + <=1s scheduling/observation allowance.
            code = child.wait(stopped_at + 5)
            elapsed = time.monotonic() - stopped_at
            require(code == 1 and any(v.get("outcome") == "forced" for v in child.events)
                    and any(v.get("code") == "SHUTDOWN_TIMEOUT" for v in child.events)
                    and not any(v.get("outcome") == "drained" for v in child.events), "held_not_failed")
            require(0.9 <= time.monotonic() - forced_at and elapsed < 5
                    if mode == "second_signal" else 3.9 <= elapsed < 5,
                    "original_shutdown_budget_mismatch")
            emit("forced_exit", elapsed=elapsed, read_joined=False)
        lock_owned(directory / "identity.json.lock", False, child.lock_identity)
        require(not (directory / "identity.json").exists(), "identity_unexpectedly_persisted")
        require(not Path(f"/proc/{child.proc.pid}").exists(), "waited_pid_still_present")
        emit("case_assertions_pass", read_joined=(mode == "eof"), lock_released=True)
    except Exception:
        failed = True
        raise
    finally:
        child.cleanup()
        if failed:
            child.emit_failed_read_snapshot()
    require(sorted(path.name for path in directory.iterdir()) == ["identity.json.lock"],
            "unexpected_private_file")
    (directory / "identity.json.lock").unlink()
    directory.rmdir()
    emit("case_retired", private_directory_absent=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    require(platform.system() == "Linux" and platform.machine() == "x86_64", "linux_amd64_only")
    binary = args.binary.absolute()
    require(not binary.is_symlink() and stat.S_ISREG(binary.stat().st_mode), "binary_not_regular")
    require(hashlib.sha256(binary.read_bytes()).hexdigest() == args.sha256, "binary_digest_mismatch")
    available = os.statvfs(args.output.parent)
    available = available.f_bavail * available.f_frsize
    print(json.dumps({"statvfs_available": available, "utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}), flush=True)
    if available < 5368709120:
        return 78
    os.umask(0o077)
    output = args.output.absolute()
    output.mkdir(mode=0o700)  # Exclusive; no reuse, cleanup, or retry of an old run.
    fd = os.open(output / "events.jsonl", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as evidence:
        def emit(event, **facts):
            evidence.write(json.dumps({"event": event, "monotonic": time.monotonic(), **facts}) + "\n")
            evidence.flush()
            os.fsync(evidence.fileno())
        def interrupted(_sig, _frame):
            signal.signal(signal.SIGTERM, signal.SIG_IGN)
            signal.signal(signal.SIGINT, signal.SIG_IGN)
            raise Failure("probe_interrupted")
        signal.signal(signal.SIGTERM, interrupted)
        signal.signal(signal.SIGINT, interrupted)
        try:
            for mode in ("eof", "second_signal", "deadline"):
                emit("case_start", mode=mode)
                run_case(binary, output / mode, mode, emit)
        except Exception as error:
            reason = str(error) if isinstance(error, Failure) else "probe_io_or_format_error"
            emit("result", passed=False, reason=reason)
            print("runner_os_signal_probe_failed", flush=True)
            return 1
        emit("result", passed=True, cases=3)
        print("runner_os_signal_probe_passed", flush=True)
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
