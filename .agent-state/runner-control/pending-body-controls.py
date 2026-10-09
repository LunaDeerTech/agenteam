#!/usr/bin/env python3
"""Actual pending-body helper + memory TLS cancellation; no OS sockets/PG."""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
GO = '/workspace/toolchains/go1.27.1/bin/go'


def main():
    source = (ROOT / 'tests/runnercontrol/process_crash_linux_test.go').read_text()
    start = source.index('func runnerCrashPendingBody(')
    end = source.index('\n}\n', start) + 3
    helper = '''package pending_test
import (
    "io"
    "net/http"
    p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)
''' + source[start:end]
    control = (Path(__file__).with_name('pending_request_body_test.go')).read_text()
    original = '''			n, err := io.Copy(io.Discard, io.LimitReader(r.Body, 1025))
			closeErr := r.Body.Close()
			bodyDone <- err == nil && closeErr == nil && n == r.ContentLength && n == 7'''
    assert control.count(original) == 1
    control = control.replace(original, '\t\t\tbodyDone <- runnerCrashPendingBody(r)')
    env = os.environ.copy()
    env.update({
        'PATH': str(Path(GO).parent) + ':' + env.get('PATH', ''),
        'AGENTEAM_GO': GO, 'GOTOOLCHAIN': 'local', 'GOENV': 'off',
        'GOWORK': 'off', 'GOPROXY': 'off', 'GOSUMDB': 'off',
        'GOTELEMETRY': 'off', 'GOFLAGS': '-mod=readonly -p=1',
        'GOMAXPROCS': '2',
        'GOCACHE': str(ROOT / 'output/ai/runner-control/gocache'),
        'GOMODCACHE': str(ROOT / 'output/ai/runner-control/go-mod'),
        'GOTMPDIR': str(ROOT / 'output/ai/runner-control/tmp'),
        'XDG_CONFIG_HOME': str(ROOT / 'output/ai/runner-control/go-config'),
    })
    with tempfile.TemporaryDirectory(prefix='pending-body-', dir=env['GOTMPDIR']) as directory:
        path = Path(directory)
        h, c, b = (path / n for n in ('actual_helper_test.go', 'memory_tls_test.go', 'bounds_test.go'))
        h.write_text(helper)
        c.write_text(control)
        b.write_text(Path(__file__).with_name('pending_body_bounds_test.go').read_text())
        command = [GO, 'test', '-race', '-count=1', '-timeout=10s', '-v',
                   '-run=^TestPending(CancellationAfterBodyEOF|BodyBounds)$', str(h), str(c), str(b)]
        return subprocess.run(command, cwd=ROOT, env=env, timeout=45, check=False).returncode


if __name__ == '__main__':
    raise SystemExit(main())
