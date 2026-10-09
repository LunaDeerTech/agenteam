#!/usr/bin/env python3
"""Run only independent D04 frozen-stage controls; isolate active Apply files."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = Path('/workspace/agenteam-secret-variable-storage')
BASE = '372d1e94'
GO = Path('/workspace/toolchains/go1.27.1/bin/go')


def blob(name):
    return subprocess.check_output(['git', 'show', BASE + ':' + name], cwd=ROOT)


def main():
    frozen = ['internal/central/secret/' + name for name in (
        'envelope.go', 'storage.go', 'project_variable_envelope_test.go',
        'service.go', 'project_variable_prepared.go', 'project_variable_prepared_test.go',
        'project_variable_read.go', 'project_variable_read_test.go',
        'project_variable_prepare.go', 'project_variable_prepare_test.go',
        'contract/project_variable_plan.go', 'contract/project_variable_plan_test.go',
        'contract/project_variable.go', 'contract/project_variable_test.go',
    )]
    snapshots = {name: blob(name) for name in frozen}
    assert all((ROOT / name).read_bytes() == data for name, data in snapshots.items())
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
    with tempfile.TemporaryDirectory(prefix='secret-storage-review-', dir=env['GOTMPDIR']) as directory:
        temp = Path(directory)
        mapping = {}
        # New files from the ongoing Apply phase are deliberately not compiled.
        tracked = set(subprocess.check_output(['git', 'ls-tree', '-r', '--name-only', BASE, '--', 'internal/central/secret'], cwd=ROOT, text=True).splitlines())
        excluded = []
        for package in (ROOT / 'internal/central/secret', ROOT / 'internal/central/secret/contract'):
            stub = temp / (package.name + '.go')
            stub.write_text('package ' + package.name + '\n')
            for path in package.glob('*.go'):
                name = str(path.relative_to(ROOT))
                if name not in tracked:
                    mapping[str(path)] = str(stub)
                    excluded.append(name)
        changed = subprocess.check_output(['git', 'diff', '--name-only', BASE, '--', 'internal/central/secret'], cwd=ROOT, text=True).splitlines()
        # Preserve old shared code if an unrelated later Apply/maintenance phase
        # edits it. Only those actual differences get temporary original blobs.
        for index, name in enumerate(changed):
            if name in tracked and name.endswith('.go') and name not in snapshots:
                original = temp / ('original-' + str(index) + '.go')
                original.write_bytes(blob(name))
                mapping[str(ROOT / name)] = str(original)
        mapping[str(ROOT / 'internal/central/secret/independent_storage_stages_test.go')] = str(HERE / 'stages_test.go')
        overlay = temp / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': mapping}))
        print('Frozen stage inputs match ' + BASE + '; excluded active new Go files: ' + str(len(excluded)), flush=True)
        result = subprocess.run([str(GO), 'test', '-race', '-count=1', '-p=1',
                                 '-overlay=' + str(overlay), '-timeout=30s', '-v',
                                 '-run=' + env.get('STORAGE_REVIEW_RUN', '^TestIndependentStorage'), './internal/central/secret'],
                                cwd=ROOT, env=env, timeout=90)
        assert all((ROOT / name).read_bytes() == data for name, data in snapshots.items()), 'frozen input changed during review'
        raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
