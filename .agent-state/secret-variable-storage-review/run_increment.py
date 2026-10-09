#!/usr/bin/env python3
"""Independent tests for the frozen Apply/maintenance delta only."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = Path('/workspace/agenteam-secret-variable-storage')
BASE = 'e0fb80df'
GO = Path('/workspace/toolchains/go1.27.1/bin/go')


def unchanged():
    assert not subprocess.check_output(['git', 'diff', BASE, '--', 'internal/central/secret'], cwd=ROOT), 'frozen Secret sources changed'


def main():
    unchanged()
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
    with tempfile.TemporaryDirectory(prefix='secret-increment-review-', dir=env['GOTMPDIR']) as directory:
        overlay = Path(directory) / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': {
            str(ROOT / 'internal/central/secret/independent_storage_increment_test.go'): str(HERE / 'increment_test.go'),
            str(ROOT / 'internal/central/postgres/independent_storage_rows_bridge.go'): str(HERE.parent / 'knowledge-b02-review/postgres_rows_bridge.go'),
        }}))
        result = subprocess.run([str(GO), 'test', '-race', '-count=1', '-p=1',
                                 '-overlay=' + str(overlay), '-timeout=30s', '-v',
                                 '-run=^TestIndependentStorageIncrement', './internal/central/secret'],
                                cwd=ROOT, env=env, timeout=90)
        unchanged()
        raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
