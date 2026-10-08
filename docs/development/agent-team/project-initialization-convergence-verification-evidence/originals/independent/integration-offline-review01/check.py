from pathlib import Path
import json,hashlib
A=Path('/workspace/scratch/project-initialization-convergence-author')
R=Path(__file__).parent
H=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
J=lambda p:json.loads(Path(p).read_bytes())
s=J(A/'integration-offline-summary01.json')
assert H(A/'integration-offline-summary01.json')=='c0b204b35fc368068a579025669131a0724f00ef97e7f7f466779bbfece7178e'
for k in ['candidate','format_delta','input','list_input','actual_graph','driver','overlay','unchanged_pure_reuse']:
 assert H(s[k]['path'])==s[k]['sha256'],k
m=J(s['candidate']['path']);m2=J(A/'candidate02/manifest.json');m2map={x['path']:x for x in m2['files']};changed=[]
for f in m['files']:
 assert H(f['snapshot'])==f['sha256']
 prev=m2map[f['path']]
 if prev['sha256']!=f['sha256']:
  changed.append(f['path'])
  b=Path(prev['snapshot']).read_bytes();n=Path(f['snapshot']).read_bytes()
  assert b.replace(b'NormalizedName: "converge-"+projectID.String()',b'NormalizedName: "converge-" + projectID.String()')==n
assert changed==['tests/project/initialization_convergence_fixture_test.go']
commands={};results={};checks=[]
for entry in s['runs']:
 for k in ['command','result','stdout','stderr']:
  assert H(entry[k]['path'])==entry[k]['sha256'],(entry['name'],k)
 c=J(entry['command']['path']);r=J(entry['result']['path']);commands[entry['name']]=c;results[entry['name']]=r
 assert r['exit_code']==entry['actual_exit']==r['command_exit_code']
 assert r['inputs_match'] and c['inputs_before']==r['inputs_after']
 assert r['direct_child_joined'] and r['subreaper'] and c['subreaper']
 assert not r['timed_out'] and not r['actions'] and not r['remaining_descendants'] and r['owned_clear_observations']==2
 assert r['elapsed_seconds']<45 and c['total_deadline_seconds']==45 and c['child_timeout_seconds']==42
 assert r['stdout.log_sha256']==entry['stdout']['sha256'] and r['stderr.log_sha256']==entry['stderr']['sha256']
 env=c['environment_overrides'];assert env['GOPROXY']=='off' and env['GOSUMDB']=='off' and env['GOMAXPROCS']=='2'
 assert '-mod=readonly' in env['GOFLAGS'] and '-p=1' in env['GOFLAGS'] and '-overlay=' in env['GOFLAGS']
 driver= A/('integration-overlay02' if entry['name']=='integration-format-preview01' else 'integration-overlay03')/'driver.py'
 assert H(driver)==c['driver_sha256']
 checks.append({'name':entry['name'],'exit':r['exit_code'],'seconds':r['elapsed_seconds'],'direct_wait_count':1,'adopted_wait_count':len(r['adopted_reaped']),'input_dictionary_identical':True,'owned_clear_observations':2,'argv':c['argv']})
assert len(checks)==9 and [c['exit'] for c in checks]==[1,0,0,0,0,0,0,0,0]
assert Path(s['runs'][0]['stdout']['path']).read_text() and not Path(s['runs'][0]['stderr']['path']).read_text()
for name,b in s['binaries'].items(): assert H(b['path'])==b['sha256']
listpath=A/'convergence-integration-list01/stdout.log';assert listpath.read_text().splitlines()==s['listed_exact'];assert len(s['listed_exact'])==4
li=J(s['list_input']['path']);bin=s['binaries']['convergence-project-integration.test'];assert li['files'][bin['path']]==bin['sha256'];assert commands['convergence-integration-list01']['inputs_before'][bin['path']]==bin['sha256']
bi=J(s['input']['path']);ov=J(s['overlay']['path'])['Replace'];assert ov==bi['overlay_replace'] and len(ov)==7;excluded={p for p,v in ov.items() if not v};assert len(excluded)==2
idx=J(s['actual_graph']['path']);graphchecks=[]
fields=['GoFiles','CgoFiles','CFiles','CXXFiles','MFiles','HFiles','FFiles','SFiles','SwigFiles','SwigCXXFiles','SysoFiles','EmbedFiles']
union={}
for ent in idx['graphs']:
 assert H(ent['path'])==ent['sha256'];g=J(ent['path']);assert not g['extra'];run=A/('convergence-'+ent['mode']+'-graph01')
 raw=(run/'stdout.log').read_text();dec=json.JSONDecoder();i=0;rows=[]
 while i<len(raw):
  while i<len(raw) and raw[i].isspace():i+=1
  if i==len(raw):break
  x,i=dec.raw_decode(raw,i);rows.append(x)
 actual={}
 for x in rows:
  assert not x.get('Error') and not x.get('DepsErrors')
  for field in fields:
   for n in x.get(field,[]):
    v=str(Path(x['Dir'])/n);assert v not in excluded
    p=ov.get(v,v);assert p and '/web/' not in p and 'project_owner_web' not in p
    actual[p]=H(p)
 assert len(rows)==ent['packages'] and actual==g['files'];assert len(actual)==ent['files'];union.update(actual)
 for z in g['generated_testmain']:
  assert H(z['path'])==z['sha256'];raw=Path(z['path']).read_text();assert 'testing.MainStart' in raw
 if ent['mode']=='full':
  p=g['project_generated_main'];assert p and not p['has_testmain'];raw=Path(p['path']).read_text();assert '_test.TestMain(' not in raw and '_xtest.TestMain(' not in raw
  assert len(g['generated_testmain'])==20
 graphchecks.append({'mode':ent['mode'],'packages':len(rows),'files':len(actual),'generated':len(g['generated_testmain']),'extra':0,'excluded_UI_consumed':False})
assert union==idx['all_effective_files']
assert all(bi['files'].get(p)==v for p,v in union.items())
compilecmd=commands['convergence-integration-compile01'];assert all(compilecmd['inputs_before'].get(p)==v for p,v in union.items())
assert compilecmd['started_utc']>=commands['convergence-full-graph01']['started_utc'] and compilecmd['started_utc']>=commands['convergence-dynamic-graph01']['started_utc']
out={'status':'PASS_FIXED_EVIDENCE_REVIEW','source_change':'only spacing in one fixture expression','commands':checks,'graphs':graphchecks,'list_exact':s['listed_exact'],'actual_new_test_bodies':False,'resources':False,'reviewer_reran_go':False,'repo_installation':False}
(R/'checks.json').write_text(json.dumps(out,indent=2)+'\n')
print(json.dumps({'status':out['status'],'runs':len(checks),'graphs':graphchecks,'private_go_run':False},indent=2))
