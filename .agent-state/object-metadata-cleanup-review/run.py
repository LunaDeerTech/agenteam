#!/usr/bin/env python3
"""Run independent actual-Service probes against fixed production, no real DB.

Uses a Go overlay for this new test only. Refuses floating Object package input;
Go cache/output remain in this reviewer's existing private Variables tree.
"""
from pathlib import Path
import argparse
import json
import os
import subprocess
import tempfile

home = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser()
parser.add_argument('--repo', type=Path, default=home.parent / 'agenteam-object-metadata-cleanup')
parser.add_argument('--revision', default='eda849dc')
args = parser.parse_args()
repo = args.repo.resolve()
subprocess.run(['git', 'diff', '--exit-code', args.revision, '--', 'internal/central/object'], cwd=repo, check=True, stdout=subprocess.DEVNULL)
extra = subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard', '--', 'internal/central/object'], cwd=repo, text=True)
if any(line.endswith('.go') for line in extra.splitlines()):
    raise SystemExit('Object package has untracked Go input')
output = home / 'output/ai/project-variables-ui/implementation'
env = dict(os.environ, GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off', GOTELEMETRY='off', GOMAXPROCS='2',
           GOMODCACHE=os.environ.get('GOMODCACHE', '/workspace/agenteam/output/ai/model-ui-recovery/go-mod'),
           GOCACHE=os.environ.get('GOCACHE', str(output / 'gocache')),
           GOTMPDIR=os.environ.get('GOTMPDIR', str(output / 'tmp')))
with tempfile.TemporaryDirectory(prefix='metadata-independent-', dir=output) as temporary:
    # Constructor only: actual Rows lifecycle methods stay untouched. Its raw
    # pgx.Rows is the explicitly controlled list, not a live PostgreSQL result.
    rows = Path(temporary) / 'rows.go'
    rows.write_text('package postgres\nimport("context";"github.com/jackc/pgx/v5")\nfunc MetadataIndependentRows(raw pgx.Rows)*Rows{return &Rows{raw:raw,ctx:context.Background(),cancel:func(){},release:func(){}}}\n')
    overlay = Path(temporary) / 'overlay.json' 
    overlay.write_text(json.dumps({'Replace': {str(repo / 'internal/central/object/zz_metadata_independent_test.go'): str(Path(__file__).with_name('implementation_test.go')), str(repo / 'internal/central/postgres/zz_metadata_review_rows.go'): str(rows)}}))
    result = subprocess.run([os.environ.get('AGENTEAM_GO', '/workspace/toolchains/go1.27.1/bin/go'), 'test', '-mod=readonly', '-p=1', '-vet=off', '-count=1', '-timeout=45s', '-overlay', str(overlay), '-run', '^TestIndependentMetadata', './internal/central/object'], cwd=repo, env=env, timeout=60, check=False)
    raise SystemExit(result.returncode)
