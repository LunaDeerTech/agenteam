from pathlib import Path
import difflib
import hashlib
import json
import re
import shutil
import subprocess

ROOT=Path('/workspace/agenteam')
BASE=Path('/workspace/scratch/meeting-summary-contract-author')
AUTHORIZED=['internal/central/model/contract/'+n for n in ('meeting_summary.go','meeting_summary_test.go','references.go','references_test.go')]
digest=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
inputs=json.loads((BASE/'inputs-before.json').read_text())
BASELINE=inputs['baseline']
deps=json.loads((BASE/'dependency-inputs.json').read_text())
paths=sorted(deps['repository_inputs'])+['go.mod','go.sum']
cmd=['git','ls-tree','-r','-z',BASELINE,'--',*paths]
raw=subprocess.run(cmd,cwd=ROOT,check=True,capture_output=True).stdout
(BASE/'baseline-ls-tree.raw').write_bytes(raw)
(BASE/'baseline-ls-tree-command.json').write_text(json.dumps(cmd,indent=2)+'\n')
accepted={}
for record in raw.split(b'\0'):
    if record:
        desc,name=record.split(b'\t',1)
        accepted[name.decode()]=desc.split()[2].decode()
comparison={}
for path in paths:
    data=(ROOT/path).read_bytes()
    blob=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
    comparison[path]={'candidate_blob':blob,'baseline_blob':accepted.get(path),'same':blob==accepted.get(path),'authorized_change':path in AUTHORIZED}
    if path not in AUTHORIZED:
        assert comparison[path]['same'], path+' unaccepted dependency'
    if path in deps['repository_inputs']:
        assert digest(ROOT/path)==deps['repository_inputs'][path], path+' changed after graph freeze'
(BASE/'accepted-dependency-comparison.json').write_text(json.dumps(comparison,indent=2)+'\n')
# Freeze only four owned sources, never a repository copy.
candidate=BASE/'candidate'
candidate.mkdir(exist_ok=False)
for p in AUTHORIZED:
    shutil.copyfile(ROOT/p,candidate/Path(p).name)
diff=''
for p in AUTHORIZED:
    old=BASE/'before'/Path(p).name
    oldtext=old.read_text() if old.exists() else ''
    diff+=''.join(difflib.unified_diff(oldtext.splitlines(keepends=True),(ROOT/p).read_text().splitlines(keepends=True),fromfile='a/'+p,tofile='b/'+p))
(BASE/'four-source.diff').write_text(diff)
commands=[]
for run in sorted(BASE.glob('[0-9][0-9]-*')):
    metadata=json.loads((run/'command.json').read_text())
    result=json.loads((run/'result.json').read_text())
    assert result['exit_code']==0 and result['direct_child_joined'] and not result['remaining_descendants'] and not result['actions']
    if run.name != '01-gofmt':
        assert metadata['inputs_before']==result['inputs_after'], run.name+' mutated inputs'
    record={'run':run.name,'argv':metadata['argv'],'seconds':result['elapsed_seconds'],'exit_code':result['exit_code'],'joined':result['direct_child_joined'],'remaining_descendants':result['remaining_descendants'],'result_sha256':digest(run/'result.json'),'stdout_sha256':digest(run/'stdout.log'),'stderr_sha256':digest(run/'stderr.log')}
    if run.name in ('04-pure-test','05-race-test'):
        log=(run/'stdout.log').read_text()
        record['top_level_pass']=len(re.findall(r'^--- PASS: ',log,re.M))
        record['subtest_pass']=len(re.findall(r'^\s+--- PASS: ',log,re.M))
        record['meeting_summary_top_level_pass']=len(re.findall(r'^--- PASS: TestMeetingSummary',log,re.M))
    commands.append(record)
freeze={'baseline':BASELINE,'head_at_freeze':subprocess.run(['git','rev-parse','HEAD'],cwd=ROOT,check=True,capture_output=True,text=True).stdout.strip(),'scope':'four pure contract sources only; not full S1 implementation/acceptance','source_sha256':{p:digest(ROOT/p) for p in AUTHORIZED},'accepted_repository_dependency_count':sum(not c['authorized_change'] for c in comparison.values()),'dependency_input_sha256':digest(BASE/'dependency-inputs.json'),'comparison_sha256':digest(BASE/'accepted-dependency-comparison.json'),'driver_sha256':digest(BASE/'driver.py'),'toolchain_sha256':{n:digest(Path('/workspace/toolchains/go1.27.1/bin')/n) for n in ('go','gofmt')},'diff_sha256':digest(BASE/'four-source.diff'),'commands':commands,'resources':'No listener, database, container, HTTP route, background worker, or migration was started. No test command failed or timed out. Preparation observations retained separately.'}
(BASE/'freeze.json').write_text(json.dumps(freeze,indent=2)+'\n')
print(json.dumps({'freeze_sha256':digest(BASE/'freeze.json'),'source_sha256':freeze['source_sha256'],'accepted_dependencies':freeze['accepted_repository_dependency_count'],'checks':commands},indent=2))
