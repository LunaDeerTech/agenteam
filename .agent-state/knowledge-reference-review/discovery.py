#!/usr/bin/env python3
"""Independent offline lower-fixture discovery: actual -list, never business run."""
import ast
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path('/workspace/agenteam-knowledge-independent')
OWN = Path('/workspace/agenteam-work-ui')
GO = '/workspace/toolchains/go1.27.1/bin/go'
source = ROOT / '.agent-state/knowledge-independent/fixture-discovery-controls.py'
tree = ast.parse(source.read_text())
probe = next(ast.literal_eval(n.value) for n in tree.body if isinstance(n, ast.Assign)
             and any(isinstance(t, ast.Name) and t.id == 'PROBE' for t in n.targets))
probe += r'''
func TestIndependentDiscoveryOriginalCancellation(t *testing.T) {
    full := "^TestKnowledgeB02IndependentTreeReference$/^revoked_persisted_public_receipt_identity_and_old_attachment$"
    target, err := selectedTestTarget(os.Getenv("AGENTEAM_DISCOVERY_PROBE_BINARY"), os.Getenv("AGENTEAM_DISCOVERY_PROBE_CWD"), full)
    if err != nil { t.Fatal(err) }
    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    cmd := target.command(ctx, true)
    raw, err := cmd.Output()
    if err == nil || cmd.Process != nil || target.matchesListing(string(raw)) { t.Fatal("cancelled discovery launched or accepted") }
    for _, filter := range []string{full + "$", full + "/^extra$", "^(?:"+full+")$", strings.Replace(full, "^revoked", "^other", 1)} {
        value, err := selectedTestTarget(target.binary, target.directory, filter)
        if err != nil { t.Fatal(err) }
        if value.listFilter != filter || value.filter != filter || value.command(context.Background(), true).Args[1] != "-test.list="+filter { t.Fatal("unapproved literal was generalized") }
    }
}
'''
env = os.environ.copy()
for key in list(env):
    if key.startswith('AGENTEAM_') and ('FIXTURE' in key or 'DISCOVERY_PROBE' in key):
        env.pop(key)
env.update({
    'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off', 'GOPROXY': 'off',
    'GOSUMDB': 'off', 'GOTELEMETRY': 'off', 'GOMAXPROCS': '2', 'GOFLAGS': '-mod=readonly',
    'GOCACHE': str(OWN / 'output/ai/work-owner-planning-ui/implementation/gocache'),
    'GOMODCACHE': '/workspace/agenteam/output/ai/model-ui-recovery/go-mod',
    'AGENTEAM_DISCOVERY_PROBE_BINARY': str(ROOT / 'output/ai/knowledge-independent/knowledge-independent-receipt-fixed-race.test'),
    'AGENTEAM_DISCOVERY_PROBE_CWD': str(ROOT / 'tests/knowledge'),
})
output = OWN / 'output/ai/work-owner-planning-ui/knowledge-discovery-review'
output.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(dir=output) as tmp:
    tmp = Path(tmp)
    path = tmp / 'probe_test.go'
    path.write_text(probe)
    overlay = tmp / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {str(ROOT / 'tests/testsupport/postgres/cmd/fixture/independent_discovery_test.go'): str(path)}}))
    selector = '^TestIndependentDiscoveryOriginalCancellation$' if '--cancel-only' in sys.argv else '^(TestExplicitFixtureDiscoveryActualReceiptBinary|TestIndependentDiscoveryOriginalCancellation)$'
    result = subprocess.run([GO, 'test', '-race', '-p=1', '-count=1', '-timeout=30s', '-v',
                             '-overlay='+str(overlay), '-run='+selector,
                             './tests/testsupport/postgres/cmd/fixture'], cwd=ROOT, env=env, timeout=45)
    raise SystemExit(result.returncode)
