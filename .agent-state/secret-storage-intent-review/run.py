#!/usr/bin/env python3
"""Independent pure controls: immutable 92aca721 contracts, no active additions."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
SOURCE = Path('/workspace/agenteam-secret-variable-storage')
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
BASE = '92aca721'


def git(*args):
    return subprocess.check_output(['git', *args], cwd=SOURCE)


def main():
    replacements = {}
    # Do not pull Model's next prepared/stage work into this two-source review.
    for package in ('internal/central/secret/contract', 'internal/central/projectvariable/contract'):
        frozen = {p for p in git('ls-tree', '-r', '--name-only', BASE, '--', package).decode().splitlines() if p.endswith('.go')}
        for name in frozen:
            if (SOURCE / name).read_bytes() != git('show', BASE + ':' + name):
                raise RuntimeError('frozen contract dependency changed: ' + name)
        for actual in (SOURCE / package).glob('*.go'):
            if str(actual.relative_to(SOURCE)) not in frozen:
                replacements[str(actual)] = ''
    env = os.environ.copy()
    env.update({
        'PATH': str(GO.parent) + os.pathsep + env.get('PATH', ''),
        'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOTELEMETRY': 'off',
        'GOMAXPROCS': '2', 'GOFLAGS': '-mod=readonly',
        'GOMODCACHE': '/workspace/agenteam/output/ai/model-ui-recovery/go-mod',
        'GOCACHE': '/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache',
        'GOTMPDIR': '/workspace/agenteam-skills/output/ai/skills/compile/tmp',
    })
    with tempfile.TemporaryDirectory(prefix='secret-intent-review-', dir=env['GOTMPDIR']) as directory:
        replacements[str(SOURCE / 'internal/central/secret/contract/independent_intent_review_test.go')] = str(HERE / 'intent_test.go')
        overlay = Path(directory) / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': replacements}))
        result = subprocess.run([str(GO), 'test', '-race', '-count=1', '-p=1',
                                 '-overlay=' + str(overlay), '-timeout=30s', '-v',
                                 '-run=^TestIndependentSecretIntent', './internal/central/secret/contract'],
                                cwd=SOURCE, env=env, timeout=90)
        if result.returncode:
            raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
