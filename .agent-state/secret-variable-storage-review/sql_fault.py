#!/usr/bin/env python3
"""Exercise actual PG adapter lock poison/projection without opening a DB."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path('/workspace/agenteam-secret-variable-storage')
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
PROBE = r'''package postgres

import (
    "context"
    "errors"
    "testing"
    f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestIndependentStorageMissingLockCommitProjection(t *testing.T) {
    token := f.NewTx()
    tx := &transaction{token: token, active: true, gate: make(chan struct{}, 1), held: make(map[string]f.LockMode)}
    tx.gate <- struct{}{}
    state := &storeState{txs: map[f.Tx]*transaction{token: tx}}
    store := &Store{state: func() *storeState { return state }}
    tx.owner = store
    key, err := f.ProjectLock("01900000-0000-7000-8000-000000000001")
    if err != nil { t.Fatal(err) }
    err = store.RequireHeldLocks(context.Background(), token, []f.LockRequest{{Key: key, Mode: f.Exclusive}})
    var pg *Error
    if !errors.As(err, &pg) || pg.Code() != LockNotHeld || tx.poison != err { t.Fatal("real lock requirement did not poison") }
    // WithinTx gives this original poison precedence over callbackErr.
    // Test its actual rejection projection separately from callback wrapping.
    result := rejected(tx.poison)
    if result.State() != f.NotCommitted || result.Fault().Code != f.InternalError || !errors.Is(result.Fault(), err) { t.Fatal("poison projection changed") }
    callback := f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(err)
    if rejected(callback).Fault().Code != f.DependencyUnavailable { t.Fatal("callback control differs") }
}
'''

source = (ROOT / 'internal/central/postgres/transaction.go').read_text()
assert 'if poisoned != nil {\n\t\t\treturn rejected(poisoned)\n\t\t}\n\t\tif callbackErr != nil {' in source
env = os.environ.copy()
env.update({
    'PATH': str(GO.parent) + os.pathsep + env.get('PATH', ''),
    'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off',
    'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOTELEMETRY': 'off',
    'GOMAXPROCS': '2', 'GOFLAGS': '-mod=readonly',
    'GOMODCACHE': '/workspace/agenteam/output/ai/model-ui-recovery/go-mod',
    'GOCACHE': '/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache',
    'GOTMPDIR': '/workspace/agenteam-skills/output/ai/skills/compile/tmp',
})
with tempfile.TemporaryDirectory(dir=env['GOTMPDIR'], prefix='d04-sql-fault-') as directory:
    path = Path(directory)
    probe = path / 'probe_test.go'
    probe.write_text(PROBE)
    overlay = path / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {str(ROOT / 'internal/central/postgres/independent_storage_fault_test.go'): str(probe)}}))
    result = subprocess.run([str(GO), 'test', '-race', '-count=1', '-p=1',
                             '-overlay=' + str(overlay), '-timeout=30s', '-v',
                             '-run=^TestIndependentStorageMissingLockCommitProjection$',
                             './internal/central/postgres'], cwd=ROOT, env=env, timeout=90)
    raise SystemExit(result.returncode)
