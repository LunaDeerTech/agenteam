"""Synchronous diagnostics for the supervisor's existing TCP samples only.

Rows and acceptance decisions remain owned by the original supervisor. An fd
scan follows a TCP snapshot and is not atomic with it; no visible holder is
therefore unknown ownership. Retired PID numbers are never live ownership.
"""
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import sys
import time


STATES = dict(zip(range(1, 13), (
    "ESTABLISHED", "SYN_SENT", "SYN_RECV", "FIN_WAIT1", "FIN_WAIT2",
    "TIME_WAIT", "CLOSE", "CLOSE_WAIT", "LAST_ACK", "LISTEN", "CLOSING",
    "NEW_SYN_RECV")))


def endpoint(value):
    address, port = value.split(":")
    packed = b"".join(int(address[i:i + 8], 16).to_bytes(4, sys.byteorder)
                      for i in range(0, len(address), 8))
    parsed = ipaddress.ip_address(packed)
    if isinstance(parsed, ipaddress.IPv6Address) and parsed.ipv4_mapped is not None:
        parsed = parsed.ipv4_mapped
    if parsed.is_unspecified:
        scope = "unspecified"
    elif parsed.is_loopback:
        scope = "loopback"
    elif parsed.is_private:
        scope = "private"
    else:
        scope = "public"
    return {"scope": scope, "port": int(port, 16)}


def inode_holders(inodes, owned, proc=Path("/proc")):
    """Bound work; an incomplete scan or an absent fd never proves absence."""
    found = {inode: [] for inode in inodes if inode != "0"}
    if not found:
        return found, True
    complete, inspected = True, 0
    pids = sorted(proc.glob("[0-9]*"), key=lambda p: int(p.name))
    for directory in pids[:512]:
        try:
            for fd in (directory / "fd").iterdir():
                inspected += 1
                if inspected > 4096:
                    return found, False
                try:
                    match = re.fullmatch(r"socket:\[(\d+)\]", os.readlink(fd))
                except (FileNotFoundError, ProcessLookupError):
                    continue
                if match and match[1] in found:
                    pid = int(directory.name)
                    item = {"pid": pid, "owned_now": pid in owned}
                    if item not in found[match[1]]:
                        found[match[1]].append(item)
        except (FileNotFoundError, ProcessLookupError):
            continue
        except PermissionError:
            complete = False
    return found, complete and len(pids) <= 512


class TCPDiagnostics:
    def __init__(self, output, descendants):
        self.output, self.descendants = Path(output), descendants
        self.baseline = None
        self.previous = set()
        self.last_delta = set()
        self.samples = 0
        self.last_sample = None
        self.last_fixture_port = None
        self.started = time.monotonic()
        self.key = secrets.token_bytes(32)
        self.stream = None

    def record(self, value):
        if self.stream is None:
            self.stream = (self.output / "tcp-diagnostics.jsonl").open("x", buffering=1)
        self.stream.write(json.dumps(value, sort_keys=True) + "\n")

    def fixture(self):
        logs = list(self.output.glob("pg-*.log"))
        if len(logs) != 1 or logs[0].stat().st_size > 1024 * 1024:
            return None, []
        text = logs[0].read_text()
        port = re.search(r"^OWNED .* port=(\d+) PostgreSQL=", text, re.M)
        # These have already been actually waited. PID reuse must not turn
        # a historical number into evidence of a currently owned process.
        retired = re.findall(r"^CHILD actual_wait pid=(\d+)\b", text, re.M)
        retired += re.findall(r"^SUPERVISOR actual_driver_wait pid=(\d+) actual=True\b", text, re.M)
        return int(port[1]) if port else None, sorted({int(pid) for pid in retired})

    def describe(self, row, fixture_port, holders):
        family, local, remote, state, inode = row
        identity = "|".join((family, local, remote))
        same_endpoint = [r for r in self.baseline if r[:3] == row[:3]]
        local_info, remote_info = endpoint(local), endpoint(remote)
        return {
            "endpoint_tag": hashlib.blake2s(identity.encode(), key=self.key, digest_size=12).hexdigest(),
            "family": family, "local": local_info, "remote": remote_info,
            "state_hex": state, "state": STATES.get(int(state, 16), "UNKNOWN"),
            "inode": inode, "holders": holders.get(inode, []),
            "holder_interpretation": "kernel_time_wait" if state == "06" and inode == "0" else
                "visible_fd" if holders.get(inode) else "unknown",
            "fixture_port_match": None if fixture_port is None else
                fixture_port in (local_info["port"], remote_info["port"]),
            "baseline_endpoint_states": sorted({r[3] for r in same_endpoint}),
            "baseline_same_inode_endpoint": any(r[4] == inode for r in same_endpoint),
        }

    def observe(self, rows):
        self.samples += 1
        common = {"sample": self.samples, "utc_ns": time.time_ns(),
                  "elapsed": round(time.monotonic() - self.started, 6)}
        self.last_sample = common
        if self.baseline is None:
            self.baseline = set(rows)
            # No raw IP, process command, environment or fd path is persisted.
            self.record(dict(common, kind="baseline", row_count=len(rows), rows=[
                self.describe(row, None, {}) for row in sorted(rows)]))
            return
        delta = rows - self.baseline
        # Preserve the actual current snapshot before fd/log diagnostics can
        # fail. previous denotes the last successfully recorded change only.
        self.last_delta = set(delta)
        if delta == self.previous:
            return
        fixture_port, retired = self.fixture()
        self.last_fixture_port = fixture_port
        owned = {os.getpid()} | self.descendants(os.getpid())
        holders, complete = inode_holders({r[4] for r in delta}, owned)
        self.record(dict(common, kind="delta_change", fixture_port=fixture_port,
                         retired_owned_pid_numbers=retired, owned_now=sorted(owned),
                         fd_scan_complete=complete, row_count=len(delta), rows=[
                             self.describe(row, fixture_port, holders) for row in sorted(delta)],
                         removed=[self.describe(row, fixture_port, {}) for row in sorted(self.previous - delta)]))
        self.previous = set(delta)

    def finish(self):
        self.record({"kind": "diagnostic_end", "samples": self.samples,
                     "elapsed": round(time.monotonic() - self.started, 6),
                     "last_original_sample": self.last_sample,
                     "last_delta_count": len(self.last_delta),
                     # This describes the last *original* sample. Do not take
                     # a new TCP/fd snapshot or reuse earlier holder evidence
                     # as though it were current ownership at the final sample.
                     "last_delta": [self.describe(row, self.last_fixture_port, {})
                                    for row in sorted(self.last_delta)]})
        if self.stream is not None:
            self.stream.close()


def observe_tcp(original, diagnostics):
    def sample():
        rows = original()  # Preserve original exceptions and the exact set.
        try:
            diagnostics.observe(rows)
        except Exception as error:
            # Diagnostics never turn the original rejection into acceptance.
            # Only a safe class name is emitted; no endpoint-bearing exception.
            print("TCP_DIAGNOSTIC_ERROR " + type(error).__name__, file=sys.stderr, flush=True)
        return rows
    return sample
