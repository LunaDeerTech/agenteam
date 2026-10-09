#!/usr/bin/env python3
"""Run the actual C transport helpers against socket-free owner controls."""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'tests/process/runner_failure_test.go'
CONTROL = Path(__file__).with_name('failure_transport_controls_test.go')
GO = '/workspace/toolchains/go1.27.1/bin/go'


def main():
    source = SOURCE.read_text()
    start = source.index('// This transport may hold')
    end = source.index('func newRunnerFailureCentral(', start)
    helpers = '''package process_test
import (
    "bufio"
    "context"
    "errors"
    "io"
    "log"
    "net"
    "net/http"
    "net/http/httptest"
    "net/http/httputil"
    "net/url"
    "sync"
    "sync/atomic"
    "testing"
    "time"
)
''' + source[start:end]
    assert "l.mode='ExclusiveLock'" in source
    assert 'context.WithTimeout(context.Background(), 3*time.Second)' in helpers
    assert helpers.count('context.WithTimeout(') == 1
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
    with tempfile.TemporaryDirectory(prefix='failure-transport-', dir=env['GOTMPDIR']) as directory:
        path = Path(directory)
        helper = path / 'actual_helper_test.go'
        helper.write_text(helpers)
        control = path / 'owner_controls_test.go'
        control.write_text(CONTROL.read_text())
        return subprocess.run([GO, 'test', '-race', '-count=1', '-timeout=15s', '-v', str(helper), str(control)], cwd=ROOT, env=env, timeout=45, check=False).returncode


if __name__ == '__main__':
    raise SystemExit(main())
