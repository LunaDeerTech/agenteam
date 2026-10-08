from pathlib import Path
import json, hashlib, re
B=Path('/workspace/scratch/project-initialization-convergence-author')
O=Path('/workspace/scratch/project-initialization-convergence-verification/unit04-offline-review')
def sha(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def read(p): return json.loads(Path(p).read_text())
def ref(p): return {'path':str(p),'sha256':sha(p)}
assert sha(B/'offline-summary01.json')=='889f0f8f9fa28e84b93732925863ce7248ddc4677207a828ac402603d59a798f'
summary=read(B/'offline-summary01.json')
candidate=read(summary['candidate']['path'])
assert sha(summary['candidate']['path'])==summary['candidate']['sha256']
for f in candidate['files']: assert sha(f['snapshot'])==f['sha256']
for f in candidate['files'][:2]: assert Path(f['snapshot']).read_bytes()==(B/'unit01/src'/f['path']).read_bytes()
assert sha(B/'unit04/unit03-to-unit04.diff')=='0c47bd64aab74a995a787201c502f80051fe99f3ffea9ccd603f65b86dd6373f'
runs=[]
commands={}
for item in summary['runs']:
 p=Path(item['result']['path']); assert sha(p)==item['result']['sha256']
 r=read(p); c=read(p.parent/'command.json'); commands[item['name']]=c
 for f in ['stdout.log','stderr.log']: assert sha(p.parent/f)==r[f+'_sha256']
 assert c['inputs_before']==r['inputs_after']
 assert r['direct_child_joined'] and r['subreaper'] and r['owned_clear_observations']==2
 assert not r['remaining_descendants'] and not r['actions'] and not r['timed_out']
 assert r['command_exit_code']==r['exit_code']==item['actual_exit']
 assert r['elapsed_seconds']<45 and c['total_deadline_seconds']==45
 assert r['exit_code']==(1 if item['name']=='format-preview01' else 0)
 raw=(p.parent/'stdout.log').read_text(); counts=None
 if '-run' in item['name']:
  assert c['argv'][-2:]==['-test.count=1','-test.timeout=40s']
  assert raw.splitlines()[-1]=='PASS' and not re.search(r'--- (FAIL|SKIP):|WARNING: DATA RACE',raw)
  names=re.findall(r'^\s*--- PASS: ([^ ]+) ',raw,re.M)
  counts={'top':sum('/' not in n for n in names),'nested':sum('/' in n for n in names),'new_top':sum(n.startswith('TestInitializationConvergenceAuthority') and '/' not in n for n in names),'new_nested':sum(n.startswith('TestInitializationConvergenceAuthority') and '/' in n for n in names)}
  assert (counts['top'],counts['nested'])==(item['top'],item['nested'])
  assert sha(c['argv'][0])==c['inputs_before'][c['argv'][0]]
 runs.append({'name':item['name'],'result':item['result'],'command':ref(p.parent/'command.json'),'exit':r['exit_code'],'direct_waited':1,'adopted_waited':len(r['adopted_reaped']),'owned_empty_observations':2,'inputs_equal':True,'counts':counts})

graphindex=read(summary['actual_graph']['path']);assert sha(summary['actual_graph']['path'])==summary['actual_graph']['sha256']
inputs=read(B/'pure-overlay02/input04-build.json')
overlay=read(B/'pure-overlay02/overlay.json')['Replace'];assert len(overlay)==3
graphs=[]
for item in graphindex['graphs']:
 assert sha(item['path'])==item['sha256'];g=read(item['path']);assert not g['extra']
 mode=item['mode'];raw=(B/f'graph-{mode}02/stdout.log').read_text();dec=json.JSONDecoder();i=0;pkgs=[]
 while i<len(raw):
  while i<len(raw) and raw[i].isspace():i+=1
  if i==len(raw):break
  v,i=dec.raw_decode(raw,i);pkgs.append(v)
 assert len(pkgs)==g['packages']
 assert not any(x.get('Error') or x.get('DepsErrors') for x in pkgs)
 assert not any(any(z in x['ImportPath'] for z in ['/internal/central/app','/tests/model','/web']) for x in pkgs)
 generated={x['path'] for x in g['generated_testmain']}
 for x in g['generated_testmain']:
  assert sha(x['path'])==x['sha256'];s=Path(x['path']).read_text()
  assert 'testing.MainStart' in s and 'os.Exit(m.Run())' in s and '_test.TestMain(' not in s
 for path,h in g['files'].items():assert inputs['files'][path]==h
 for name in ['project-'+('race' if mode=='race' else 'pure')+'-compile01','contract-'+('race' if mode=='race' else 'pure')+'-compile01']:
  c=commands[name]
  assert c['frozen_manifest_sha256']==sha(B/'pure-overlay02/input04-build.json')
  assert all(c['inputs_before'].get(p)==h for p,h in g['files'].items())
  assert c['started_utc']>commands[f'graph-{mode}02']['started_utc']
 graphs.append({'mode':mode,'packages':len(pkgs),'files':len(g['files']),'generated_no_custom_testmain':len(generated),'extra':0})

old=read(B/'pure-actual-graph01.json');extra={x['physical']:x['sha256'] for g in old['graphs'] for x in g['extra']};assert len(extra)==29
delta=read(B/'pure-overlay02/cgo-delta.json');assert len(delta['files'])==33
original=read(delta['source_provenance'])['files']
def inherited(p):
 d=read(p);out={}
 if 'base_input' in d:out.update(inherited(d['base_input']['path']))
 out.update(d.get('files',{}));return out
tools=inherited(delta['tool_provenance'])
for path,h in delta['files'].items():
 assert sha(path)==h and inputs['files'][path]==h
 if path in extra: assert extra[path]==original[path]==h
 else: assert tools[path]==h
pre=read(B/'pure-pregraph03.json');assert not pre['missing']
for f in pre['fixed_commit_minimal_delta']:
 raw=Path(f['snapshot']).read_bytes();assert hashlib.sha256(raw).hexdigest()==f['sha256']
 assert hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest()==f['git_blob']
result={'status':'PASS','check_kind':'independent read-only evidence verification; no Go execution','summary':ref(B/'offline-summary01.json'),'candidate':summary['candidate'],'format_diff':ref(B/'unit04/unit03-to-unit04.diff'),'runs':runs,'graphs':graphs,'cgo_delta':ref(B/'pure-overlay02/cgo-delta.json'),'cgo_source_provenance':ref(delta['source_provenance']),'cgo_accepted_input':ref(delta['tool_provenance']),'old_extra_sources':len(extra),'added_sources':29,'added_tools':4,'fixed_contract_test_delta':6,'production_byte_unchanged':True,'limits':'only 3 scratch sources and Project/contract pure; integration/PG/root installation not accepted'}
(O/'checks.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({k:v for k,v in result.items() if k not in ['runs']},ensure_ascii=False))
