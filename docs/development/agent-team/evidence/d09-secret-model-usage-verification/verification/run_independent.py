import hashlib,json,os,pathlib,re,signal,subprocess,sys,time
root=pathlib.Path(__file__).parent
if not (root/'run-authorization.json').is_file(): raise SystemExit('root fixture handoff required')
label=sys.argv[1]
groupname='independent-secret-model'
readybytes=(root/'ready.json').read_bytes()
assert hashlib.sha256(readybytes).hexdigest()=='f68ef0f12072a073be65e62a261a68c5e80d894451212871da417b2a958f214f'
ready=json.loads(readybytes)
group={'argv':ready['argv'],'expected_top':ready['expected_top']}
selectors={'input_sha256':ready['candidate_manifest_sha256']}
manifest=root/'candidate-manifest.json'
assert hashlib.sha256(manifest.read_bytes()).hexdigest()==selectors['input_sha256']
inputs=json.loads(manifest.read_text())
for row in inputs['paths']:
 assert hashlib.sha256((root/'src'/row['path']).read_bytes()).hexdigest()==row['sha256'],row['path']
assert hashlib.sha256((root/'src'/ready['probe']['path']).read_bytes()).hexdigest()==ready['probe']['sha256']
assert hashlib.sha256(pathlib.Path(ready['environment']['AGENTEAM_MINIO_BINARY']).read_bytes()).hexdigest()=='dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'
assert hashlib.sha256(pathlib.Path(ready['environment']['AGENTEAM_GO']).read_bytes()).hexdigest()=='30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624'
d=root/'fixture-runs'/label;d.mkdir(parents=True)
config=root/'docker-config';config.mkdir(mode=0o700,exist_ok=True)
env={k:v for k,v in os.environ.items() if not k.startswith(('AGENTEAM_','DOCKER_','PG'))}
settings=dict(ready['environment'])
env.update(settings)
def save(name,data): (d/name).write_text(json.dumps(data,indent=2)+'\n')
def docker(*args): return subprocess.run(['docker',*args],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=25)
def snapshot():
 out={}
 for kind in ['container','network']:
  ls=docker('ps','-aq','--no-trunc') if kind=='container' else docker('network','ls','-q','--no-trunc')
  assert ls.returncode==0,ls.stderr
  rows=[]
  for ident in ls.stdout.split():
   fmt='{"ID":{{json .Id}},"Name":{{json .Name}},"Labels":{{json .Config.Labels}}}' if kind=='container' else '{"ID":{{json .Id}},"Name":{{json .Name}},"Labels":{{json .Labels}}}'
   got=docker(kind,'inspect','--format',fmt,ident);assert got.returncode==0,got.stderr
   rows.append(json.loads(got.stdout))
  out[kind]=sorted(rows,key=lambda x:x['ID'])
 return out
def proc_table():
 out={}
 for p in pathlib.Path('/proc').iterdir():
  if not p.name.isdigit(): continue
  try:
   raw=(p/'stat').read_text();tail=raw.rsplit(')',1)[1].split();out[int(p.name)]={'pid':int(p.name),'state':tail[0],'ppid':int(tail[1]),'pgrp':int(tail[2]),'start_ticks':int(tail[19])}
  except (OSError,ValueError,IndexError): pass
 return out
baseline=snapshot();save('baseline.json',baseline)
assert len(baseline['container'])==2 and len(baseline['network'])==4,'baseline count changed'
assert not list((root/'runtime').iterdir()),'owned runtime not empty'
assert not list((root/'gotmp').iterdir()),'owned gotmp not empty'
assert not list(config.iterdir()),'owned Docker config not empty'
start=time.time();save('command.json',{'argv':group['argv'],'env':settings,'cwd':str(root/'src'),'input_manifest':str(manifest),'input_sha256':selectors['input_sha256'],'start_unix':start,'expected_top':group['expected_top'],'ready_sha256':hashlib.sha256(readybytes).hexdigest(),'probe_sha256':ready['probe']['sha256'],'authorization_sha256':hashlib.sha256((root/'run-authorization.json').read_bytes()).hexdigest(),'runner_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()})
tracked={};groups=set();network_live={};last_network_check=0;eventsfile=(d/'docker-events.jsonl').open('wb');eventerr=(d/'docker-events.stderr').open('wb')
events=subprocess.Popen(['docker','events','--since',str(int(start)),'--filter','type=container','--filter','type=network','--format','{{json .}}'],env=env,stdout=eventsfile,stderr=eventerr,start_new_session=True)
with (d/'driver.log').open('wb') as log:
 child=subprocess.Popen(group['argv'],cwd=root/'src',env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 print(json.dumps({'started':label,'pid':child.pid,'group':groupname,'input_sha256':selectors['input_sha256']}),flush=True)
 while True:
  table=proc_table(); owned={child.pid}
  while True:
   more={p for p,row in table.items() if row['ppid'] in owned or (p,row['start_ticks']) in tracked}
   nxt=owned|more
   if nxt==owned: break
   owned=nxt
  for p in owned:
   if p in table:
    row=table[p];tracked[(p,row['start_ticks'])]=row;groups.add(row['pgrp'])
  if time.time()-last_network_check>1:
   last_network_check=time.time()
   for line in (d/'docker-events.jsonl').read_text().splitlines():
    try: e=json.loads(line)
    except json.JSONDecodeError: continue
    if e.get('Type')!='network' or e.get('Action')!='create':continue
    a=e.get('Actor',{});ident=a.get('ID','');name=a.get('Attributes',{}).get('name','')
    if ident in network_live or not re.fullmatch('agenteam-d(?:03-|04-net-|05-object-)[0-9a-f]{32}',name):continue
    got=docker('network','inspect','--format','{"ID":{{json .Id}},"Name":{{json .Name}},"Labels":{{json .Labels}}}',ident)
    if got.returncode:continue
    info=json.loads(got.stdout); labs=info.get('Labels') or {}
    valid={k:v for k,v in labs.items() if k in {'agenteam.d03.fixture','agenteam.d04.networkfixture','agenteam.d05.objectfixture'} and name.endswith(v)}
    if info['ID']==ident and info['Name']==name and valid:network_live[ident]=info
  (d/'owned-network-live-labels.json').write_text(json.dumps(list(network_live.values()),indent=2)+'\n')
  code=child.poll()
  if code is not None: break
  time.sleep(.15)
time.sleep(.4)
events.terminate()
try: events.wait(timeout=5)
except subprocess.TimeoutExpired: events.kill();events.wait()
eventsfile.close();eventerr.close()
raw=(d/'docker-events.jsonl').read_text();evs=[]
for line in raw.splitlines():
 try: evs.append(json.loads(line))
 except json.JSONDecodeError: raise SystemExit('incomplete Docker event JSON')
save('owned-network-live-labels.json',list(network_live.values()))
fixturelabels={'agenteam.d03.fixture','agenteam.d04.networkfixture','agenteam.d05.objectfixture'}
owned=[]
for e in evs:
 if e.get('Action')!='create' or e.get('Type') not in ['container','network']:continue
 attrs=e.get('Actor',{}).get('Attributes',{})
 labels={k:v for k,v in attrs.items() if k in fixturelabels}
 if e['Type']=='network' and e['Actor']['ID'] in network_live:
  labels=network_live[e['Actor']['ID']]['Labels']
 if labels:
  ident=e['Actor']['ID'];assert re.fullmatch('[0-9a-f]{64}',ident)
  owned.append({'kind':e['Type'],'id':ident,'name':attrs.get('name'),'labels':labels})
owned=list({(r['kind'],r['id']):r for r in owned}.values());save('owned-created.json',owned)
checks=[]
for n in [1,2]:
 resources=[]
 for r in owned:
  got=docker(r['kind'],'inspect','--format','{{.Id}}',r['id'])
  resources.append({**r,'exit_code':got.returncode,'absent':got.returncode!=0 and ('No such' in got.stderr or 'not found' in got.stderr),'stderr':got.stderr.strip()})
 now=snapshot();table=proc_table()
 live=[row for key,row in tracked.items() if key[0] in table and table[key[0]]['start_ticks']==key[1]]
 livegroups=[row for row in table.values() if row['pgrp'] in groups]
 check={'round':n,'resources':resources,'current':now,'baseline_unchanged':now==baseline,'tracked_live':live,'owned_group_live':livegroups,'runtime':[str(p.relative_to(root/'runtime')) for p in (root/'runtime').rglob('*')],'gotmp':[str(p.relative_to(root/'gotmp')) for p in (root/'gotmp').rglob('*')]};checks.append(check)
 if n==1:time.sleep(.5)
save('resource-handoff.json',{'checks':checks,'tracked':list(tracked.values()),'groups':sorted(groups),'events_exit':events.returncode})
log=(d/'driver.log').read_text(errors='replace')
children=re.findall(r'^    --- PASS: (Test\S+) \(',log,re.M)
allfail=re.findall(r'^\s*--- FAIL: (Test\S+) \(',log,re.M)
allskip=re.findall(r'^\s*--- SKIP: (Test\S+) \(',log,re.M)
input_after=[]
for row in inputs['paths']:
 sha=hashlib.sha256((root/'src'/row['path']).read_bytes()).hexdigest()
 input_after.append({'path':row['path'],'sha256':sha,'unchanged':sha==row['sha256']})
save('input-after.json',input_after)
assert hashlib.sha256((root/'src'/ready['probe']['path']).read_bytes()).hexdigest()==ready['probe']['sha256']
passed=re.findall(r'^--- PASS: (Test\S+) \(',log,re.M);failed=re.findall(r'^--- FAIL: (Test\S+) \(',log,re.M);skipped=re.findall(r'^--- SKIP: (Test\S+) \(',log,re.M)
missing=sorted(set(group['expected_top'])-set(passed))
clean=sum(r['kind']=='container' for r in owned)==4 and sum(r['kind']=='network' for r in owned)==3 and all(c['baseline_unchanged'] and not c['tracked_live'] and not c['owned_group_live'] and not c['runtime'] and not c['gotmp'] and all(r['absent'] for r in c['resources']) for c in checks)
final={'driver_exit':code,'driver_log_sha256':hashlib.sha256((d/'driver.log').read_bytes()).hexdigest(),'passed_child':children,'failed_all':allfail,'skipped_all':allskip,'expected_children':ready['expected_sub_count'],'elapsed_seconds':round(time.time()-start,3),'passed_top':passed,'failed_top':failed,'skipped_top':skipped,'missing_expected_top':missing,'resource_clean':clean,'owned_container_count':sum(r['kind']=='container' for r in owned),'owned_network_count':sum(r['kind']=='network' for r in owned),'tracked_process_count':len(tracked),'input_sha256':selectors['input_sha256']}
save('result.json',final);print(json.dumps(final),flush=True)
sys.exit(code or (0 if not missing and not allfail and not allskip and len(children)==ready['expected_sub_count'] and all(x['unchanged'] for x in input_after) and clean else 2))
