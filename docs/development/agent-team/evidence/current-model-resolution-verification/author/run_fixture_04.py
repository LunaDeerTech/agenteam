import hashlib,json,os,pathlib,re,signal,subprocess,sys,time
root=pathlib.Path(__file__).parent
if os.environ.get('CURRENT_MODEL_RESOLUTION_FIXTURE_AUTHORIZED')!='1': raise SystemExit('root fixture handoff required')
groupname,label=sys.argv[1:]
selectors=json.loads((root/'ready-selectors-04.json').read_text())
group=next(x for x in selectors['groups'] if x['name']==groupname)
manifest=root/selectors['input_manifest']
assert hashlib.sha256(manifest.read_bytes()).hexdigest()==selectors['input_sha256']
inputs=json.loads(manifest.read_text())
for row in inputs['paths']:
 for base in ([pathlib.Path('/workspace/agenteam')] if row.get('capability_document_deferred') else [root/'snapshot',pathlib.Path('/workspace/agenteam')]):
  assert hashlib.sha256((base/row['path']).read_bytes()).hexdigest()==row['sha256'],row['path']
d=root/'fixture-runs'/label;d.mkdir(parents=True)
config=root/'docker-config';config.mkdir(mode=0o700,exist_ok=True)
env={k:v for k,v in os.environ.items() if not k.startswith(('AGENTEAM_','DOCKER_','PG'))}
settings={'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOFLAGS':selectors['GOFLAGS'],'GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':'/workspace/agenteam-secret-model-usage-author-l4eactgz/gocache','GOTMPDIR':str(root/'gotmp'),'TMPDIR':str(root/'runtime'),'GOMAXPROCS':'4','AGENTEAM_GO':'/workspace/toolchains/go1.27.1/bin/go','AGENTEAM_MINIO_BINARY':'/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio','CGO_ENABLED':'1','DOCKER_HOST':'unix:///var/run/docker.sock','DOCKER_CONFIG':str(config)}
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
start=time.time();save('command.json',{'argv':group['argv'],'env':settings,'cwd':str(root/'snapshot'),'input_manifest':str(manifest),'input_sha256':selectors['input_sha256'],'start_unix':start,'expected_top':group['expected_top']})
tracked={};groups=set();network_live={};last_network_check=0;eventsfile=(d/'docker-events.jsonl').open('wb');eventerr=(d/'docker-events.stderr').open('wb')
events=subprocess.Popen(['docker','events','--since',str(int(start)),'--filter','type=container','--filter','type=network','--format','{{json .}}'],env=env,stdout=eventsfile,stderr=eventerr,start_new_session=True)
with (d/'driver.log').open('wb') as log:
 child=subprocess.Popen(group['argv'],cwd=root/'snapshot',env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
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
    if ident in network_live or not re.fullmatch('agenteam-d(?:03-|04-net-|05-object-|07-smtp-)[0-9a-f]{32}',name):continue
    got=docker('network','inspect','--format','{"ID":{{json .Id}},"Name":{{json .Name}},"Labels":{{json .Labels}}}',ident)
    if got.returncode:continue
    info=json.loads(got.stdout); labs=info.get('Labels') or {}
    valid={k:v for k,v in labs.items() if k in {'agenteam.d03.fixture','agenteam.d04.networkfixture','agenteam.d05.objectfixture','agenteam.d07.smtpfixture'} and name.endswith(v)}
    if info['ID']==ident and info['Name']==name and valid:network_live[ident]=info
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
fixturelabels={'agenteam.d03.fixture','agenteam.d04.networkfixture','agenteam.d05.objectfixture','agenteam.d07.smtpfixture'}
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
passed=re.findall(r'^--- PASS: (Test\S+) \(',log,re.M);failed=re.findall(r'^--- FAIL: (Test\S+) \(',log,re.M);skipped=re.findall(r'^--- SKIP: (Test\S+) \(',log,re.M)
missing=sorted(set(group['expected_top'])-set(passed))
clean=sum(r['kind']=='container' for r in owned)>=4 and sum(r['kind']=='network' for r in owned)>=3 and all(c['baseline_unchanged'] and not c['tracked_live'] and not c['owned_group_live'] and not c['runtime'] and not c['gotmp'] and all(r['absent'] for r in c['resources']) for c in checks)
final={'driver_exit':code,'elapsed_seconds':round(time.time()-start,3),'passed_top':passed,'failed_top':failed,'skipped_top':skipped,'missing_expected_top':missing,'resource_clean':clean,'owned_container_count':sum(r['kind']=='container' for r in owned),'owned_network_count':sum(r['kind']=='network' for r in owned),'expected_counts_match':sum(r['kind']=='container' for r in owned)==group['expected_containers'] and sum(r['kind']=='network' for r in owned)==group['expected_networks'],'tracked_process_count':len(tracked),'input_sha256':selectors['input_sha256']}
save('result.json',final);print(json.dumps(final),flush=True)
sys.exit(code or (0 if not missing and not skipped and clean and final['expected_counts_match'] else 2))
