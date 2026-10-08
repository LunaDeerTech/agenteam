from pathlib import Path
import ast,hashlib,json
B=Path('/workspace/scratch/project-initialization-convergence-author/pg-driver-v01');R=Path(__file__).parent
H=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest();J=lambda p:json.loads(Path(p).read_bytes())
assert H(B/'freeze.json')=='62882dd8ad7ad1d911c7ada385ee5fd085e3e42b0218e6181bd3ad68d5dd6a76'
f=J(B/'freeze.json')
for p,sha in f['files'].items():assert H(B/p)==sha
for k in ['candidate','offline']:assert H(f[k]['path'])==f[k]['sha256']
p=J(B/'proposal-freeze.json');assert not p['root_authorized_resources'] and p['driver_sha256']==H(B/'driver.py')
refs=[]
def load(row):
 assert H(row['path'])==row['sha256'];refs.append(row);d=J(row['path'])
 if 'base_closure' in d:
  base=load(d['base_closure']);base['files'].update(d.get('files',{}));base['package_file_sets'].update(d.get('package_file_sets',{}));base.update({k:v for k,v in d.items() if k not in ['files','package_file_sets']});d=base
 return d
c=load(p['closure']);assert c['candidate']['sha256']==p['candidate_sha256'];assert H(c['actual_union']['path'])==c['actual_union']['sha256'];g=J(c['actual_union']['path'])
flat={k:v['sha256'] for k,v in c['files'].items()};assert all(flat.get(k)==v for k,v in g['all_effective_files'].items())
ov=c['overlay_replace'];assert len(ov)==7;assert len([v for v in ov.values() if not v])==2
assert all(k not in flat for k,v in ov.items() if not v)
assert ov==J('/workspace/scratch/project-initialization-convergence-author/integration-overlay03/overlay.json')['Replace']
for x in J(c['candidate']['path'])['files']:assert flat[x['snapshot']]==x['sha256']
cgoref=c['cgo0_reuse'];assert H(cgoref['path'])==cgoref['sha256'];cgo=J(cgoref['path']);assert len(cgo['files'])==1219 and not cgo['extra']
for path,sha in cgo['files'].items():assert flat.get(path)==sha and H(path)==sha
assert not any('/central/project' in x['import'] for x in cgo['packages'])
original=Path('/workspace/scratch/project-owner-audit-http-author/pg-driver-v01/driver.py');assert H(original)=='5e1e27b989c3d4f8879cc23c589ac57c7952d818133e2a9416b75e0b927748b7'
def nodes(p):return {x.name:ast.dump(x,include_attributes=False) for x in ast.parse(Path(p).read_text()).body if isinstance(x,(ast.FunctionDef,ast.ClassDef))}
a,b=nodes(original),nodes(B/'driver.py');claim=J(B/'static-delta-check.json')
for n in claim['unchanged_AST']:assert a[n]==b[n],n
changed=[n for n in a if n in b and a[n]!=b[n]];assert sorted(changed)==sorted(claim['changed_existing_functions']);assert sorted(set(b)-set(a))==sorted(claim['new_functions'])
const={}
for x in ast.parse((B/'driver.py').read_text()).body:
 if isinstance(x,ast.Assign):
  for target in x.targets:
   if isinstance(target,ast.Name) and target.id=='GROUPS':const['GROUPS']=ast.literal_eval(x.value)
assert list(const['GROUPS'])==p['permitted_groups'];assert len(const['GROUPS'])==4 and all(len(v)==1 for v in const['GROUPS'].values())
for path,sha in p['driver_runtime_files'].items():assert H(path)==sha
out={'status':'STATIC_PRECHECK_WITH_ONE_BUDGET_PRECISION_FINDING','freeze_sha256':H(B/'freeze.json'),'driver_sha256':H(B/'driver.py'),'closure_sha256':H(B/'closure.json'),'proposal_sha256':H(B/'proposal-freeze.json'),'effective_files':len(flat),'effective_sets':len(c['package_file_sets']),'runtime_file_count':len(p['driver_runtime_files']),'base_manifest_count':len(refs),'actual_union_all_in_closure':True,'CGO0_source_files_hash_verified':1219,'CGO0_imports_Project':False,'unchanged_AST':claim['unchanged_AST'],'changed_existing_AST':changed,'groups':const['GROUPS'],'overlay_entries':7,'excluded_UI_content_read':False,'inputgate_executed':False,'driver_executed':False,'resources':False}
(R/'checks.json').write_text(json.dumps(out,indent=2)+'\n');print(json.dumps(out,indent=2))
