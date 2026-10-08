from pathlib import Path
import ast,difflib,hashlib,json
A=Path('/workspace/scratch/project-initialization-convergence-author');V=Path('/workspace/scratch/project-initialization-convergence-verification');R=Path(__file__).parent
H=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest();J=lambda p:json.loads(Path(p).read_bytes())
def ref(p):return {'path':str(p),'sha256':H(p)}
def write(p,d):p.write_text(json.dumps(d,indent=2)+'\n')
def flat(p):
 d=J(p)
 if 'base_closure'in d:
  q=d['base_closure'];assert H(q['path'])==q['sha256'];z=flat(q['path']);z['files'].update(d.get('files',{}));z['package_file_sets'].update(d.get('package_file_sets',{}));z.update({k:v for k,v in d.items() if k not in ('files','package_file_sets')});return z
 return d
parent=A/'pg-driver-v02';assert H(parent/'driver.py')=='62895c8d30f5f35fbec65521ad0b145c74730d368f8e6bcd57102cb9a4ea0b5e'
p=flat(parent/'closure.json');g=J(V/'pg-offline01/actual-graph01.json');ov=J(V/'pg-probes01/overlay.json')['Replace'];inp=J(V/'pg-offline01/input-build.json')
assert g['extra']==[] and g['excluded_ui_consumed']==[] and g['project_main']['has_custom_TestMain'] is False
for f,s in g['files'].items():
 if f in p['files']:assert p['files'][f]['sha256']==s
added={f:s for f,s in g['files'].items() if f not in p['files']};assert len(added)==2
assert len(ov)==8 and all(ov[k]==v for k,v in p['overlay_replace'].items())
groups={'independent-a':['TestIndependentInitializationConvergenceTransactionBoundary'],'independent-b':['TestIndependentInitializationConvergenceCanonicalFacts']}
write(R/'groups.json',groups)
old=(parent/'driver.py').read_text();lines=old.splitlines(keepends=True);oldgroup=next(x for x in lines if x.startswith('GROUPS = '));new=old.replace(oldgroup,'GROUPS = '+repr(groups)+'\n').replace(str(A/'integration-overlay03/overlay.json'),str(V/'pg-probes01/overlay.json'))
(R/'driver.py').write_text(new);(R/'driver.diff').write_text(''.join(difflib.unified_diff(old.splitlines(True),new.splitlines(True),fromfile=str(parent/'driver.py'),tofile=str(R/'driver.py'))))
def nodes(src):return {x.name:ast.dump(x,include_attributes=False) for x in ast.parse(src).body if isinstance(x,(ast.FunctionDef,ast.ClassDef))}
f1,f2=nodes(old),nodes(new);changed=sorted(k for k in f1.keys()|f2.keys() if f1.get(k)!=f2.get(k));assert changed==['runtime_environment'];assert set(f1)==set(f2)
write(R/'static-delta-check.json',{'parent_driver':ref(parent/'driver.py'),'driver':ref(R/'driver.py'),'changed_function_AST':changed,'all_other_function_and_class_AST_unchanged':True,'changed_assignments':['GROUPS'],'new_functions':[],'no_driver_execution':True})
union={'parent_full20_dynamic':p['actual_union'],'independent_race_project':ref(V/'pg-offline01/actual-graph01.json'),'independent_offline':ref(V/'pg-offline01/summary01.json'),'new_actual_files':added,'mismatch_with_parent':[],'project_generated_main':g['project_main'],'overlay':ref(V/'pg-probes01/overlay.json'),'scope':'Parent full20/dynamic/CGO0 unchanged; only project private test plus its actual generated main added. Retain parent generated artifacts as historical inputs, not current independent TestMain. No new graph execution.'}
write(R/'actual-union-delta.json',union)
refs=[V/'pg-probes01/independent_initialization_convergence_test.go',V/'pg-probes01/overlay.json',V/'pg-offline01/actual-graph01.json',V/'pg-offline01/summary01.json',R/'actual-union-delta.json']
files={f:{'sha256':s} for f,s in added.items()}
files.update({str(f):{'sha256':H(f)} for f in refs})
closure={'base_closure':ref(parent/'closure.json'),'candidate':p['candidate'],'actual_union':ref(R/'actual-union-delta.json'),'files':files,'package_file_sets':{'/workspace/agenteam/tests/project':inp['package_file_sets']['/workspace/agenteam/tests/project']},'overlay_replace':ov,'independent_graph':ref(V/'pg-offline01/actual-graph01.json'),'scope':'Only frozen independent probe/runtime overlay delta; no product installation, no UI sources, no new schemas/runtime reads.'}
write(R/'closure.json',closure)
python=Path('/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3')
proposal={'root_authorized_resources':False,'permitted_groups':list(groups),'driver_sha256':H(R/'driver.py'),'candidate_sha256':p['candidate']['sha256'],'closure':ref(R/'closure.json'),'driver_runtime_files':{str(f):H(f) for f in [R/'driver.py',R/'groups.json',V/'pg-probes01/overlay.json',python]},'check_only_ready':False,'independent_test_bodies_executed':False,'authorization':'PREPARED ONLY. Root must authorize source-only gate and a fresh exclusive resource window. A PASS plus actual wait/full cleanup/input match required before B; no automatic retry.'}
write(R/'proposal-freeze.json',proposal)
# Prepare, but do not execute, the already accepted 45s actual-wait wrapper.
off=R/'source-gate';off.mkdir(exist_ok=True);(off/'tmp').mkdir(exist_ok=True)
baseold=(V/'pg-offline01/driver.py').read_text();basenew=baseold.replace("BASE = Path('"+str(V/'pg-offline01')+"')","BASE = Path('"+str(off)+"')");assert basenew!=baseold
(off/'driver.py').write_text(basenew);(off/'driver.diff').write_text(''.join(difflib.unified_diff(baseold.splitlines(True),basenew.splitlines(True),fromfile=str(V/'pg-offline01/driver.py'),tofile=str(off/'driver.py'))))
assert nodes(baseold)==nodes(basenew)
fc=flat(R/'closure.json');gatefiles={f:v['sha256'] for f,v in fc['files'].items()};gatefiles.update(proposal['driver_runtime_files'])
for f in [R/'closure.json',R/'proposal-freeze.json',R/'static-delta-check.json',off/'driver.py']:gatefiles[str(f)]=H(f)
write(off/'input.json',{'status':'PREPARED_SOURCE_ONLY_NOT_EXECUTED','files':gatefiles,'package_file_sets':fc['package_file_sets'],'overlay_replace':ov,'purpose':'Original45s/actualwait/double-owned; child source-only branch returns before Docker/resource commands.'})
argv=[str(python),str(off/'driver.py'),str(off/'input.json'),'inputcheck01','--',str(python),str(R/'driver.py'),'--name','check-only','--group','independent-a','--frozen',str(R/'proposal-freeze.json'),'--check-input-only']
write(off/'command.json',{'argv':argv,'seconds':45,'intervention_seconds':42,'executed':False,'actual_direct_adopted_wait':True,'double_owned_empty':True,'no_resources':True})
write(R/'commands.json',{'source_gate':argv,'runtime_A':[str(python),str(R/'driver.py'),'--name','independent-a01','--group','independent-a','--frozen',str(R/'execution-freeze.json'),'--execute-authorized'],'runtime_B':[str(python),str(R/'driver.py'),'--name','independent-b01','--group','independent-b','--frozen',str(R/'execution-freeze.json'),'--execute-authorized'],'executed':False,'execution_freeze_exists':False})
write(R/'preparation-checks.json',{'status':'STATIC_PREPARED_ONLY','parent_function_AST_reused':True,'actual_graph_added_files':added,'actual_graph_mismatch':[],'source_closure_count':len(fc['files']),'effective_sets':len(fc['package_file_sets']),'overlay_entries':len(ov),'empty_UI_overlays':len([x for x in ov.values() if not x]),'source_gate_input_count':len(gatefiles),'actual_gate_run':False,'actual_resources':False,'repository_installation':False})
print(json.dumps({'added_actual_files':len(added),'closure_files':len(fc['files']),'sets':len(fc['package_file_sets']),'driver':H(R/'driver.py'),'closure':H(R/'closure.json'),'proposal':H(R/'proposal-freeze.json'),'gate_input_files':len(gatefiles)},indent=2))
