#!/usr/bin/env python3
"""Limited source/contract check; never creates canonical database evidence."""
from pathlib import Path
import json
import os
import subprocess

here = Path(__file__).resolve().parent
subject = Path('/workspace/agenteam-knowledge-independent')
source_path = 'tests/knowledge/b02_independent_tree_reference_test.go'
old = subprocess.check_output(['git', 'show', 'b7798cd5:' + source_path], cwd=subject, text=True)
new = (subject / source_path).read_text()
assert old[:old.index('\tt.Run("revoked_original_receipt')] == new[:new.index('\tt.Run("revoked_persisted_public_receipt')]
content = 'tests/knowledge/b02_independent_content_test.go'
assert subprocess.check_output(['git', 'show', 'b7798cd5:' + content], cwd=subject) == (subject / content).read_bytes()
assert not subprocess.check_output(['git', 'diff', 'b7798cd5', '--', 'internal', 'db', 'cmd'], cwd=subject)
print('first five subtests/product unchanged; actual contract controls follow', flush=True)
output = here.parents[1] / 'output/ai/work-owner-planning-ui/implementation/knowledge-reference-review'
output.mkdir(parents=True, exist_ok=True)
virtual = subject / '.agent-state/independent_public_receipt_test.go'
overlay = output / 'overlay.json'
overlay.write_text(json.dumps({'Replace': {str(virtual): str(here / 'receipt_test.go')}}))
env = dict(os.environ, GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOPROXY='off', GOSUMDB='off', GOTELEMETRY='off', GOMAXPROCS='2',
           GOCACHE='/workspace/agenteam-work-ui/output/ai/work-owner-planning-ui/implementation/gocache',
           GOMODCACHE='/workspace/agenteam/output/ai/model-ui-recovery/go-mod')
result = subprocess.run(['/workspace/toolchains/go1.27.1/bin/go', 'test', '-overlay', str(overlay), '-mod=readonly', '-p=1', '-race', '-count=1', '-timeout=15s', '-v', str(virtual)], cwd=subject, env=env)
raise SystemExit(result.returncode)
