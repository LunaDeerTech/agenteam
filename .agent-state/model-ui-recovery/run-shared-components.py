#!/usr/bin/env python3
"""Run the two fixed focus selections and retain actual process/fixture results."""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import uuid
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[2]
SCOPES = {
    "representative": (1, r"light dialog 390 no-preference: original editor focus survives confirmation \(blocking=true\)"),
    "focus": (21, "original editor focus survives confirmation|top nonmodal popover with a disabled anchor falls back within its remaining modal|page fallback is not attempted by direct component destruction|page fallback cannot escape a remaining modal or steal focus on non-top removal"),
}


def descendants():
    rows = {}
    for path in Path('/proc').glob('[0-9]*/stat'):
        try:
            fields = path.read_text().rsplit(')', 1)[1].split()
            rows[int(path.parent.name)] = (int(fields[1]), fields[19], fields[0])
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            pass
    owned = {os.getpid()}
    while True:
        more = {pid for pid, row in rows.items() if row[0] in owned} - owned
        if not more:
            return {pid: rows[pid] for pid in owned - {os.getpid()} if pid in rows}
        owned |= more


def signal_owned(sig):
    for pid, identity in descendants().items():
        try:
            fields = Path('/proc', str(pid), 'stat').read_text().rsplit(')', 1)[1].split()
            if fields[19] == identity[1] and fields[0] != 'Z':
                os.kill(pid, sig)
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            pass


def reap(adopted):
    while True:
        try:
            pid, status = os.waitpid(-1, os.WNOHANG)
        except ChildProcessError:
            return True
        if pid == 0:
            return False
        adopted.append({"pid": pid, "status": status})


def settle(adopted):
    deadline = time.monotonic() + 10
    while not reap(adopted) and time.monotonic() < deadline:
        time.sleep(.05)
    if reap(adopted):
        return True
    signal_owned(signal.SIGKILL)
    # After killing only identified descendants, collect their actual statuses.
    # Do not raise or leave the subreaper while adopted children are still live.
    while True:
        try:
            pid, status = os.waitpid(-1, 0)
            adopted.append({"pid": pid, "status": status})
        except ChildProcessError:
            return False


def listening(ports):
    result = []
    for family in ('tcp', 'tcp6'):
        for line in Path('/proc/net', family).read_text().splitlines()[1:]:
            fields = line.split()
            port = int(fields[1].split(':')[1], 16)
            if port in ports and fields[3] == '0A':
                result.append({"family": family, "port": port})
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--scope', choices=SCOPES, required=True)
    args = parser.parse_args()
    count, pattern = SCOPES[args.scope]
    os.umask(0o077)
    evidence = ROOT / 'output/ai/model-ui-recovery' / ('shared-focus-' + args.scope + '-' + uuid.uuid4().hex)
    evidence.mkdir(mode=0o700)
    run = Path(tempfile.mkdtemp(prefix='ml-focus-'))
    environment = dict(os.environ, AGENTEAM_DIALOG_WEB_ROOT=str(ROOT / 'web'), AGENTEAM_DIALOG_RUN_DIR=str(run), AGENTEAM_DIALOG_CHROMIUM='/usr/bin/chromium')
    command = ['node', 'tests/account-captcha-web/node_modules/@playwright/test/cli.js', 'test', '--config=tests/account-captcha-web/dialog-outside-focus.config.js', '--grep', pattern]
    facts = {"scope": args.scope, "run": str(run), "case_seconds": 45, "selected": count, "exit": 1, "direct_actual_wait": False, "adopted_actual_waits": [], "normal_adopted_settlement": False, "server_closed": False, "inputs_unchanged": False}
    source_paths = [Path(__file__), ROOT / 'tests/account-captcha-web/dialog-outside-focus.config.js', ROOT / 'tests/account-captcha-web/e2e/dialog-outside-focus.spec.ts']
    sources = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in source_paths}
    child = None
    started = time.monotonic()
    assert ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) == 0
    try:
        discovery_env = dict(environment, AGENTEAM_DIALOG_RUN_DIR=str(run / 'discovery'))
        discovery = subprocess.run(command + ['--list'], cwd=ROOT, env=discovery_env, capture_output=True, text=True, timeout=45)
        (evidence / 'discovery.log').write_text(discovery.stdout + discovery.stderr)
        assert discovery.returncode == 0 and f'Total: {count} tests in 1 file' in discovery.stdout
        with (evidence / 'run.log').open('x') as log:
            child = subprocess.Popen(command, cwd=ROOT, env=environment, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            facts['pid'] = child.pid
            # Preserve every original 45s case, plus 15s fixture startup/close.
            # Adopted children get a further bounded normal settlement below.
            try:
                code = child.wait(timeout=45 * count + 15)
            except subprocess.TimeoutExpired:
                facts['failure'] = 'COMPONENT_TOTAL_BUDGET_EXCEEDED'
                signal_owned(signal.SIGTERM)
                try:
                    child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    signal_owned(signal.SIGKILL)
                    child.wait()
                code = 1
            facts.update(direct_actual_wait=True, direct_exit=child.returncode)
        facts['normal_adopted_settlement'] = settle(facts['adopted_actual_waits'])
        servers = [json.loads(p.read_text()) for p in run.glob('server-*.json')]
        facts['server_closed'] = bool(servers) and all(s['closed'] for s in servers)
        facts['inputs_unchanged'] = all(hashlib.sha256(Path(p).read_bytes()).hexdigest() == digest for p, digest in sources.items()) and bool(servers) and all(hashlib.sha256((ROOT / 'web' / p).read_bytes()).hexdigest() == digest for s in servers for p, digest in s['inputs'].items())
        ports = {urlparse(s['origin']).port for s in servers}
        facts['retirement_observations'] = [{"descendants": sorted(descendants()), "owned_listeners": listening(ports)} for _ in range(2)]
        report = json.loads((run / 'results.json').read_text())
        facts['stats'] = report['stats']
        stats = report['stats']
        facts['exit'] = 0 if code == 0 and facts['normal_adopted_settlement'] and facts['server_closed'] and facts['inputs_unchanged'] and all(not row['descendants'] and not row['owned_listeners'] for row in facts['retirement_observations']) and stats['expected'] == count and stats['unexpected'] == stats['skipped'] == stats['flaky'] == 0 else 1
    except Exception:
        facts.setdefault('failure', 'COMPONENT_SUPERVISOR_FAILED')
    finally:
        if child is not None and child.poll() is None:
            signal_owned(signal.SIGKILL)
            child.wait()
            facts.update(direct_actual_wait=True, direct_exit=child.returncode)
        if descendants():
            signal_owned(signal.SIGKILL)
        settle(facts['adopted_actual_waits'])
        facts['final_descendants'] = sorted(descendants())
        facts['elapsed_seconds'] = time.monotonic() - started
        (evidence / 'terminal.json').write_text(json.dumps(facts, indent=2) + '\n')
    print(json.dumps({"evidence": str(evidence), **facts}))
    return facts['exit']


if __name__ == '__main__':
    raise SystemExit(main())
