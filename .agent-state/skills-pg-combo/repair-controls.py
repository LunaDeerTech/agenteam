#!/usr/bin/env python3
"""Offline original-cause comparison and exact accepted TCP transplant controls."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
sys.dont_write_bytecode = True
mode = sys.argv[1] if len(sys.argv) == 2 else ''
if mode == 'tcp':
    # Reuse the actually reviewed control source from the common Git object
    # database; no remote access or copied whole-source evidence archive.
    source = subprocess.check_output([
        'git', 'show', '57642926:.agent-state/runner-control/tcp-evidence-controls.py'
    ], cwd=ROOT, text=True)
    old = "BASELINE = '3e7fd3bd7aaa661c35ca96b68a103c789118548e'"
    assert source.count(old) == 1
    source = source.replace(old, "BASELINE = '8e7afde8'")
    old_output = "parent = ROOT / 'output/ai/runner-control'"
    assert source.count(old_output) == 1
    source = source.replace(old_output, "parent = ROOT / 'output/ai/skills/compile'")
    sys.argv = [str(Path(__file__))]
    # Its ROOT expression now resolves to this Skills tree. Everything else,
    # including all diagnostic/gate controls, executes unchanged.
    exec(compile(source, __file__, 'exec'), globals())
elif mode == 'cause':
    source = (ROOT / 'tests/skills/admission_unknown_test.go').read_text()
    start = source.index('func sameAdmissionCause(')
    end = source.index('\nfunc (s *admissionCommitStore)', start)
    helper = source[start:end]
    program = '''package main
import (
 "fmt"
 "reflect"
 f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)
''' + helper + '''
func main() {
 id := func(ns, owner, command, key string) f.CommandIdentity {
  x, e := f.NewCommandIdentity(ns, []string{owner}, command, f.IdempotencyKey(key)); if e != nil { panic(e) }; return x
 }
 owner := "01900000-0000-7000-8000-000000000001"
 original := id("skills", owner, "initialize", "original")
 cause := func(primary f.CommandIdentity, related ...f.CommandIdentity) f.TransactionCause {
  x, e := f.NewCommandsCause(primary, related...); if e != nil { panic(e) }; return x
 }
 a := cause(original)
 check := func(ok bool) { if !ok { panic("cause comparison control failed") } }
 check(!reflect.DeepEqual(a.Details(), a.Details()))
 check(sameAdmissionCause(a, a))
 check(sameAdmissionCause(a, cause(id("skills", owner, "initialize", "original"))))
 for _, other := range []f.CommandIdentity{
  id("different", owner, "initialize", "original"),
  id("skills", "01900000-0000-7000-8000-000000000002", "initialize", "original"),
  id("skills", owner, "different", "original"),
  id("skills", owner, "initialize", "different"),
 } { check(!sameAdmissionCause(a, cause(other))) }
 b := id("skills", owner, "initialize", "related-b")
 c := id("skills", owner, "initialize", "related-c")
 check(sameAdmissionCause(cause(original,b,c), cause(original,b,c)))
 check(!sameAdmissionCause(cause(original,b,c), cause(original,c,b)))
 check(!sameAdmissionCause(cause(original,b), cause(original,b,c)))
 recovery, e := f.NewRecoveryCause("skills", owner, "original"); if e != nil { panic(e) }
 changed, e := f.NewRecoveryCause("skills", owner, "different"); if e != nil { panic(e) }
 check(!sameAdmissionCause(a, recovery))
 check(sameAdmissionCause(recovery, recovery))
 check(!sameAdmissionCause(recovery, changed))
 check(!sameAdmissionCause(f.TransactionCause{}, f.TransactionCause{}))
 fmt.Println("actual_cause_controls=14 original_deepequal_self_false=true")
}
'''
    output = ROOT / 'output/ai/skills/compile'
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='cause-control-', dir=output) as directory:
        path = Path(directory) / 'main.go'
        path.write_text(program)
        subprocess.run(['/workspace/toolchains/go1.27.1/bin/go', 'run', '-mod=readonly', '-p=1', str(path)],
                       cwd=ROOT, env=os.environ, check=True)
else:
    raise SystemExit('choose cause or tcp')
