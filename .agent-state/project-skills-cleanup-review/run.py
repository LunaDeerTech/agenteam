#!/usr/bin/env python3
"""Offline overlay only. Run after root lifts the small-Go compile hold."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = Path('/workspace/agenteam-project-skills-cleanup')
GO = Path('/workspace/toolchains/go1.27.1/bin/go')


def main():
    space = os.statvfs(ROOT)
    available = space.f_bavail * space.f_frsize
    print(json.dumps({'statvfs_available': available}), flush=True)
    if available < 5368709120:
        raise SystemExit(78)
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
    assert Path(env['GOCACHE']).is_dir()
    assert not subprocess.check_output(['git', 'diff', '415e0df5', '--',
        'internal/central/project/lifecycle_authority.go'], cwd=ROOT), 'frozen product differs'
    before = (ROOT / 'internal/central/project/lifecycle_authority.go').read_bytes()
    with tempfile.TemporaryDirectory(prefix='skills-cleanup-review-', dir=env['GOTMPDIR']) as directory:
        overlay = Path(directory) / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': {
            str(ROOT / 'internal/central/project/independent_skills_cleanup_test.go'): str(HERE / 'authority_test.go'),
            str(ROOT / 'internal/central/postgres/independent_cleanup_rows_bridge.go'): str(HERE.parent / 'knowledge-b02-review/postgres_rows_bridge.go'),
        }}))
        result = subprocess.run([str(GO), 'test', '-race', '-count=1', '-p=1',
                                 '-overlay=' + str(overlay), '-timeout=30s', '-v',
                                 '-run=^TestIndependentSkillsCleanupOriginalTxAndRows$', './internal/central/project'],
                                cwd=ROOT, env=env, timeout=90)
        assert before == (ROOT / 'internal/central/project/lifecycle_authority.go').read_bytes(), 'frozen product changed'
        raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
