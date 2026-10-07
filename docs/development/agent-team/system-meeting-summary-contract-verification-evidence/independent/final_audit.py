import hashlib,json,time
from pathlib import Path
BASE=Path('/workspace/scratch/summary-verification/contract-four-source')
AUTHOR=Path('/workspace/scratch/meeting-summary-contract-author')
ROOT=Path('/workspace/agenteam')
def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def read(p): return json.loads(p.read_text())
inputs=read(BASE/'inputs.json')
assert sha(AUTHOR/'freeze.json')==inputs['author_freeze_sha256']
assert sha(AUTHOR/'author-report.md')=='5a52f2ba3b18b215a281f7c8a2a4dbbe39e0a7d75a3100cf1f2e13776cc81dba'
freeze=read(AUTHOR/'freeze.json')
assert len(inputs['dependencies'])==64
for p,h in {**inputs['sources'],**inputs['dependencies'],**inputs['legacy_unchanged']}.items():
 assert sha(ROOT/p)==h,p
for p,h in inputs['sources'].items(): assert sha(AUTHOR/'candidate'/Path(p).name)==h,p
expected={p for p in {**inputs['sources'],**inputs['dependencies']} if p.startswith('internal/central/model/contract/')}
actual={str(p.relative_to(ROOT)) for p in (ROOT/'internal/central/model/contract').glob('*.go')}
assert expected==actual,(expected-actual,actual-expected)
assert sha(AUTHOR/'four-source.diff')==freeze['diff_sha256']
for tool,h in freeze['toolchain_sha256'].items(): assert sha(Path('/workspace/toolchains/go1.27.1/bin')/tool)==h
runs=[]
for c in freeze['commands']:
 p=AUTHOR/c['run']; r=read(p/'result.json')
 for name,key in [('result.json','result_sha256'),('stdout.log','stdout_sha256'),('stderr.log','stderr_sha256')]: assert sha(p/name)==c[key]
 assert r['exit_code']==0 and r['subreaper'] and r['direct_child_joined'] and not r['remaining_descendants'] and not r['actions']
for n,expected_exit in [('format-probe',0),('independent-pure',1),('independent-pure-corrected',0),('independent-race',0)]:
 p=BASE/n; c=read(p/'command.json'); r=read(p/'result.json')
 assert r['exit_code']==expected_exit
 assert r['subreaper'] and r['direct_child_joined'] and not r['timed_out'] and not r['actions'] and not r['remaining_descendants'] and r['elapsed_seconds']<45
 assert c['driver_sha256']==sha(BASE/'driver.py')
 for k,h in c['inputs_before'].items():
  if n=='format-probe' and k.endswith('/probe_test.go'): continue
  assert r['inputs_after'][k]==h,(n,k)
 if n in ['independent-pure-corrected','independent-race']:
  assert r['inputs_after'][str(BASE/'probe_test.go')]==sha(BASE/'probe_test.go')
  assert r['inputs_after'][str(BASE/'overlay.json')]==sha(BASE/'overlay.json')
  out=(p/'stdout.log').read_text()
  assert sum(x.startswith('--- PASS: TestIndependentSummary') for x in out.splitlines())==3
  assert '288 independent owner/role/project/clear/reasoning combinations' in out
  assert (p/'stderr.log').read_bytes()==b''
 for name in ['stdout.log','stderr.log']: assert sha(p/name)==r[name+'_sha256']
 runs.append({'run':n,'exit_code':r['exit_code'],'seconds':r['elapsed_seconds'],'subreaper':r['subreaper'],'joined':r['direct_child_joined'],'actions':r['actions'],'remaining_descendants':r['remaining_descendants'],'driver_pid':c['driver_pid'],'tracked':r['processes_seen'],'sha256':{f:sha(p/f) for f in ['command.json','result.json','stdout.log','stderr.log']}})
def procs():
 out={}
 for p in Path('/proc').iterdir():
  if not p.name.isdecimal(): continue
  try:
   raw=(p/'stat').read_text(); fields=raw[raw.rfind(')')+2:].split()
   out[int(p.name)]={'starttime':fields[19],'ppid':int(fields[1]),'pgid':int(fields[2]),'state':fields[0]}
  except (OSError,ValueError,IndexError): pass
 return out
tails=[]
for turn in range(2):
 table=procs(); alive=[]; drivers=[]
 for run in runs:
  for p in run['tracked']:
   if p['pid'] in table and table[p['pid']]['starttime']==p['starttime']: alive.append(p)
  if run['driver_pid'] in table: drivers.append(run['driver_pid'])
 assert not alive and not drivers
 tails.append({'utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'remaining_pid_starttime_matches':alive,'driver_pids_present':drivers})
 if turn==0: time.sleep(.05)
for r in runs: r['tracked_process_count']=len(r.pop('tracked'))
out={'scope':'four pure contract sources only; not full S1','author_freeze_sha256':sha(AUTHOR/'freeze.json'),'author_report_sha256':sha(AUTHOR/'author-report.md'),'source_sha256':inputs['sources'],'accepted_dependency_count':64,'all_current_dependency_sha256_match':True,'legacy_sha256':inputs['legacy_unchanged'],'contract_directory_set_exact':True,'author_evidence_runs_verified':6,'toolchain_sha256':freeze['toolchain_sha256'],'own_files_sha256':{f:sha(BASE/f) for f in ['driver.py','probe_test.go','probe_test.initial.go.txt','overlay.json','inputs.json','final_audit.py']},'runs':runs,'tail_checks':tails,'initial_probe_failure':'Own assertion incorrectly treated JSON decode rejection as failure. Corrected only probe condition; all frozen sources unchanged. Raw failure retained.'}
(BASE/'final-audit.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'all_checks_pass':True,'final_audit_sha256':sha(BASE/'final-audit.json'),'sources':4,'dependencies':64,'runs':[{k:r[k] for k in ['run','exit_code','seconds','joined','tracked_process_count']} for r in runs],'tail_checks':tails},ensure_ascii=False))
