#!/usr/bin/env python3
"""Independent offline Skills fixture review, 9e3fc022 -> a5ebdd97.

Run from /workspace/agenteam-knowledge:
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH \
 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 \
 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod \
 GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache \
 python3 .agent-state/skills-fixture-review/controls.py

Actual 37ed09 exit 0: exact inverse of both files and 41 cause controls.
Initial b73fab exit 1 was this control's wrong-module internal-import setup;
correcting cwd retained own temporary source and verified all nine Foundation
production sources byte-identical before running the extracted real helper.

Finite review: no must-fix in these two fixtures. The deleting seed follows
the current owner/version under same-Tx User/Project EX, inserts the exact
accepted Delete operation, manifest and required participants before setting
the deferred FK, and still requires one updated row and committed outcome.
All original read denials, publication facts and absence of Object work remain.
This is not BeginDelete validation. No Skills source edits, PG, socket or
network; original 4315 failure and pending real integration rerun remain.
"""
from pathlib import Path
import os
import re
import subprocess
import tempfile

root = Path('/workspace/agenteam-skills')
control_root = Path('/workspace/agenteam-knowledge')
base = '9e3fc022'
admission = root / 'tests/skills/admission_unknown_test.go'
owner = root / 'tests/skills/owner_read_test.go'
original = lambda name: subprocess.check_output(['git', 'show', base + ':' + name], cwd=root, text=True)
source = admission.read_text()
helper_start = source.index('func sameAdmissionCause(')
helper_end = source.index('\nfunc (s *admissionCommitStore)', helper_start)
helper = source[helper_start:helper_end]
inverse = source[:source.index('// CauseDetails contains')] + source[helper_end+1:]
inverse = inverse.replace('"context"\n', '"context"\n\t"reflect"\n', 1)
inverse = inverse.replace('sameAdmissionCause(unknown.Cause(), observing.original.Cause())', 'reflect.DeepEqual(unknown.Cause().Details(), observing.original.Cause().Details())')
inverse = inverse.replace('sameAdmissionCause(preserved.Cause(), unknown.Cause())', 'reflect.DeepEqual(preserved.Cause().Details(), unknown.Cause().Details())')
inverse, n = re.subn(r't\.Fatalf\("admission Unknown invariant:.*?first\.value\.State == "", proxy\.WriterPID\(\) == observing\.writer\)', 't.Fatal("admission lost its actual physical Unknown or returned completion")', inverse, count=1, flags=re.S)
assert n == 1 and inverse == original('tests/skills/admission_unknown_test.go')
current = owner.read_text()
start = current.index('// This is a valid upstream lifecycle seed')
end = current.index('// Register the same explicit test keys', start)
inverse = current[:start] + current[end:]
inverse = inverse.replace('seedReaderDeletingProject(t, p, v)', 'readerMutation(t, p, v, `UPDATE agenteam_project.projects SET lifecycle=\'deleting\',current_lifecycle_operation_id=$2,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, v.request.ProjectID.String(), testID[pc.Operation](t).String())', 1)
assert inverse == original('tests/skills/owner_read_test.go')
print('two_files_inverse_exact=true', flush=True)

# Run the standalone main inside its own module's internal import boundary.
# Refuse to reuse this control if the actual Foundation inputs diverge.
production = lambda tree: {p.name: p.read_bytes() for p in (tree / 'internal/central/foundation').glob('*.go') if not p.name.endswith('_test.go')}
assert production(root) == production(control_root)
print('foundation_production_byte_equal=true', flush=True)

program = '''package main
import (
 "fmt"
 "reflect"
 f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)
''' + helper + '''
func main(){
 ids:=[]string{"01900000-0000-7000-8000-000000000001","01900000-0000-7000-8000-000000000002","01900000-0000-7000-8000-000000000003"}
 identity:=func(namespace string,owners []string,command,key string)f.CommandIdentity{x,e:=f.NewCommandIdentity(namespace,owners,command,f.IdempotencyKey(key));if e!=nil{panic(e)};return x}
 command:=func(primary f.CommandIdentity,related ...f.CommandIdentity)f.TransactionCause{x,e:=f.NewCommandsCause(primary,related...);if e!=nil{panic(e)};return x}
 job:=func(kind,id,attempt string)f.TransactionCause{x,e:=f.NewJobCause(kind,id,attempt);if e!=nil{panic(e)};return x}
 delivery:=func(id,handler string)f.TransactionCause{x,e:=f.NewDeliveryCause(id,handler);if e!=nil{panic(e)};return x}
 recovery:=func(owner,run,checkpoint string)f.TransactionCause{x,e:=f.NewRecoveryCause(owner,run,checkpoint);if e!=nil{panic(e)};return x}
 count:=0
 check:=func(ok bool){count++;if !ok{panic(fmt.Sprintf("independent cause control %d",count))}}
 primary:=identity("skills",ids[:2],"initialize","original")
 a:=command(primary)
 check(!reflect.DeepEqual(a.Details(),a.Details()))
 check(sameAdmissionCause(a,command(identity("skills",ids[:2],"initialize","original"))))
 for _,other:=range []f.CommandIdentity{
 identity("foreign",ids[:2],"initialize","original"),
 identity("skills",[]string{ids[1],ids[0]},"initialize","original"),
 identity("skills",ids[:1],"initialize","original"),
 identity("skills",ids[:2],"other","original"),
 identity("skills",ids[:2],"initialize","different"),
 }{check(!sameAdmissionCause(a,command(other)))}
 b:=identity("skills",ids[:1],"initialize","b");c:=identity("skills",ids[:1],"initialize","c")
 check(sameAdmissionCause(command(primary,b,c),command(primary,b,c)))
 check(!sameAdmissionCause(command(primary,b,c),command(primary,c,b)))
 check(!sameAdmissionCause(command(primary,b),command(primary,b,c)))
 check(!sameAdmissionCause(command(primary,b),command(primary,c)))
 pairs:=[][2]f.TransactionCause{
 {job("skills",ids[0],ids[1]),job("other",ids[0],ids[1])},
 {job("skills",ids[0],ids[1]),job("skills",ids[2],ids[1])},
 {job("skills",ids[0],ids[1]),job("skills",ids[0],ids[2])},
 {delivery(ids[0],"skills"),delivery(ids[1],"skills")},
 {delivery(ids[0],"skills"),delivery(ids[0],"other")},
 {recovery("skills",ids[0],"checkpoint"),recovery("other",ids[0],"checkpoint")},
 {recovery("skills",ids[0],"checkpoint"),recovery("skills",ids[1],"checkpoint")},
 {recovery("skills",ids[0],"checkpoint"),recovery("skills",ids[0],"different")},
 {a,job("skills",ids[0],ids[1])},
 }
 for _,p:=range pairs{check(sameAdmissionCause(p[0],p[0]));check(!sameAdmissionCause(p[0],p[1]));check(!sameAdmissionCause(p[1],p[0]))}
 check(!sameAdmissionCause(f.TransactionCause{},f.TransactionCause{}));check(!sameAdmissionCause(a,f.TransactionCause{}));check(!sameAdmissionCause(f.TransactionCause{},a))
 fmt.Printf("independent_cause_controls=%d PASS\\n",count)
}
'''
output = Path('/workspace/agenteam-knowledge/output/ai/knowledge/skills-fixture-review')
output.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='cause-', dir=output) as temp:
    path = Path(temp) / 'main.go'
    path.write_text(program)
    subprocess.run(['/workspace/toolchains/go1.27.1/bin/go', 'run', '-mod=readonly', '-p=1', str(path)], cwd=control_root, env=os.environ, check=True)
