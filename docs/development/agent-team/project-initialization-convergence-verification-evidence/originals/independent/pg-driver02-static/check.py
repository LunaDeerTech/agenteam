from pathlib import Path
import ast, hashlib, json
A=Path('/workspace/scratch/project-initialization-convergence-author'); V=Path('/workspace/scratch/project-initialization-convergence-verification'); R=Path(__file__).parent
H=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
J=lambda p:json.loads(Path(p).read_bytes())
def ref(p):return {'path':str(p),'sha256':H(p)}
f=J(A/'pg-driver-v02/freeze.json')
assert H(A/'pg-driver-v02/freeze.json')=='5e45849062912497bdd0c9844c3b2376f33dd667763985a4a6e197cf1673da7a'
for n,s in f['files'].items():assert H(A/'pg-driver-v02'/n)==s,n
for k in ['candidate','offline']:assert H(f[k]['path'])==f[k]['sha256']
def funcs(p):return {x.name:ast.dump(x,include_attributes=False) for x in ast.parse(p.read_text()).body if isinstance(x,(ast.FunctionDef,ast.ClassDef))}
a,b=funcs(A/'pg-driver-v01/driver.py'),funcs(A/'pg-driver-v02/driver.py')
changed=sorted(n for n in set(a)|set(b) if a.get(n)!=b.get(n));assert changed==['retire_tcp_observation']
def closure(p):
 d=J(p)
 if 'base_closure' in d:
  q=d['base_closure'];assert H(q['path'])==q['sha256'];base=closure(q['path'])
  base['files'].update(d.get('files',{}));base['package_file_sets'].update(d.get('package_file_sets',{}));base.update({k:v for k,v in d.items() if k not in ('files','package_file_sets')});return base
 return d
c1=closure(A/'pg-driver-v01/closure.json');c2=closure(A/'pg-driver-v02/closure.json')
for k in ['files','package_file_sets','overlay_replace','candidate','actual_union','cgo0_reuse']:assert c1[k]==c2[k],k
pr=J(A/'pg-driver-v02/proposal-freeze.json');assert pr['root_authorized_resources'] is False
for p,s in pr['driver_runtime_files'].items():assert H(p)==s
fg=J(A/'pg-inputgate02/freeze.json')
for n,s in fg['files'].items():assert H(A/'pg-inputgate02'/n)==s
for k in ['wrapper','pg_proposal']:assert H(fg[k]['path'])==fg[k]['sha256']
cmd=J(A/'pg-inputgate02/command.json');assert cmd['argv'][-1]=='--check-input-only' and cmd['seconds']==45 and cmd['no_resources']
rt=J(A/'pg-inputcheck02/result.json');assert H(A/'pg-inputcheck02/result.json')=='1b8018942e7483ce4745a8220c98a06c7abed0f5e86d81e75641d74102031de3'
assert rt['exit_code']==rt['command_exit_code']==0 and rt['direct_child_joined'] and rt['inputs_match'] and rt['owned_clear_observations']==2
assert not rt['timed_out'] and not rt['actions'] and not rt['remaining_descendants']
for n in ['stdout.log','stderr.log']:assert H(A/'pg-inputcheck02'/n)==rt[n+'_sha256']
raw=J(A/'pg-inputcheck02/stdout.log');assert raw['accepted'] and raw['files_count']==3380 and not raw['resources_started'] and not raw['missing'] and not raw['mismatches'] and not raw['package_set_changes']
assert raw['closure_sha256']==H(A/'pg-driver-v02/closure.json') and raw['candidate_sha256']==f['candidate']['sha256']
refs=[A/'pg-driver-v02'/n for n in ['freeze.json','driver.py','closure.json','proposal-freeze.json','driver.diff','plan.md']]+[A/'pg-inputgate02'/n for n in ['freeze.json','command.json','input.json']]+[A/'pg-inputcheck02'/n for n in ['command.json','result.json','stdout.log','stderr.log']]+[V/'pg-driver01-static/check.py',V/'pg-driver01-static/checks.json']
out={'status':'PASS_STATIC_EXECUTION_READINESS','changed_AST':changed,'effective_closure_unchanged':True,'effective_files':len(c2['files']),'effective_sets':len(c2['package_file_sets']),'overlay_entries':len(c2['overlay_replace']),'findings_closed':['IC-DRIVER-01'],'source_only_result':{k:rt[k] for k in ['command_exit_code','elapsed_seconds','direct_child_joined','adopted_reaped','owned_clear_observations','remaining_descendants','actions','inputs_match']},'source_only_child':raw,'references':[ref(p) for p in refs],'independent_execution':{'Go':False,'source_gate':False,'resources':False},'limitations':['Readiness only; no PG product execution conclusion.','Host TCP polling is supplementary; not complete short-connection tracing or ownership proof.','Deadline bounds new reads and sleeps, not a hard real-time scheduler guarantee.','Fresh resource/image/space/PID baseline required per authorized actual round.']}
(R/'checks.json').write_text(json.dumps(out,indent=2)+'\n');print(json.dumps({'status':out['status'],'checks_sha256':H(R/'checks.json'),'closure_files':len(c2['files']),'gate_actual0':True},indent=2))
