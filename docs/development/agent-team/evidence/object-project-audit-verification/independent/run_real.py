import os,pathlib,subprocess,json,time,threading,hashlib,sys
p=pathlib.Path('/tmp/agenteam-object-audit-verifier-frxgtjbx')
for d in ['docker-config','runtime']:(p/d).mkdir(exist_ok=True)
env=os.environ.copy()
for k in list(env):
 if k.startswith('AGENTEAM_') or k in ['DOCKER_CONTEXT','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH','DOCKER_AUTH_CONFIG','GOFLAGS']:
  env.pop(k,None)
chosen={'AGENTEAM_GO':'/workspace/toolchains/go1.27.1/bin/go','AGENTEAM_MINIO_BINARY':'/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio','GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':'/workspace/agenteam-object-audit-verifier-build-frxgtjbx/gocache','TMPDIR':'/workspace/agenteam-object-audit-verifier-build-frxgtjbx/runtime','DOCKER_CONFIG':str(p/'docker-config'),'DOCKER_HOST':'unix:///var/run/docker.sock','GOFLAGS':'-v'}
env.update(chosen)
docker=['docker','--config',str(p/'docker-config'),'--host','unix:///var/run/docker.sock']
def dc(args):return subprocess.check_output(docker+args,env=env,text=True).strip()
def snapshot():
 out={}
 for kind,listing,labels in [('container',['ps','-aq'],'.Config.Labels'),('network',['network','ls','-q'],'.Labels')]:
  ids=dc(listing).split()
  fmt='[{{json .Id}},{{json .Name}},{{json '+labels+'}}]'
  rows=dc([kind,'inspect','--format',fmt]+ids).splitlines() if ids else []
  out[kind]=sorted([{'id':v[0],'name':v[1],'labels':v[2] or {}} for v in map(json.loads,rows)],key=lambda x:x['id'])
 return out
manifest=json.loads((p/'input.json').read_bytes())
for f,h in manifest['files'].items():assert hashlib.sha256((p/'tree'/f).read_bytes()).hexdigest()==h,f
probe=p/'tree/tests/objects/object_audit_independent_test.go'
(p/'evidence/probe.sha256').write_text(hashlib.sha256(probe.read_bytes()).hexdigest()+'  tests/objects/object_audit_independent_test.go\n')
base=snapshot();(p/'evidence/baseline.json').write_text(json.dumps(base,indent=2)+'\n')
print('baseline captured',len(base['container']),'containers',len(base['network']),'networks',flush=True)
created={'container':{},'network':{}}
start=time.time()
eventerr=(p/'logs/docker-events.stderr').open('w')
events=subprocess.Popen(docker+['events','--since',str(int(start)),'--format','{{json .}}'],env=env,text=True,stdout=subprocess.PIPE,stderr=eventerr)
def monitor():
 with (p/'evidence/resource-events.jsonl').open('w') as log:
  for line in events.stdout:
   try:e=json.loads(line)
   except ValueError:continue
   kind=e.get('Type');action=e.get('Action',e.get('status'))
   if kind not in created or action not in ['create','destroy']:continue
   actor=e.get('Actor',{});rid=actor.get('ID',e.get('id'));attrs=actor.get('Attributes',{})
   row={'kind':kind,'action':action,'id':rid,'name':attrs.get('name'),'timeNano':e.get('timeNano'),'labels':{k:v for k,v in attrs.items() if 'nonce' in k or 'fixture' in k}}
   if action=='create':created[kind][rid]=row
   log.write(json.dumps(row)+'\n');log.flush()
thread=threading.Thread(target=monitor);thread.start()
cmd=['sh','scripts/test-objects.sh','-run','^TestObjectAuditIndependent']
record={'command':cmd,'cwd':str(p/'tree'),'env':chosen,'started_epoch':start,'probe_sha256':hashlib.sha256(probe.read_bytes()).hexdigest()}
(p/'evidence/real-command.json').write_text(json.dumps(record,indent=2)+'\n')
with (p/'logs/independent-real-3.log').open('w') as log:
 log.write(json.dumps(record)+'\n');log.flush()
 process=subprocess.Popen(cmd,cwd=p/'tree',env=env,stdout=log,stderr=subprocess.STDOUT)
 print('driver started pid',process.pid,'log',p/'logs/independent-real-3.log',flush=True)
 code=process.wait()
print('driver exit',code,'seconds',round(time.time()-start,3),flush=True)
events.terminate()
try:events.wait(timeout=5)
except subprocess.TimeoutExpired:events.kill();events.wait()
thread.join(timeout=5);eventerr.close()
(p/'evidence/owned-ids.json').write_text(json.dumps(created,indent=2)+'\n')
def final_check(n):
 after=snapshot();absent={};errors={}
 for kind,ids in created.items():
  for rid in ids:
   r=subprocess.run(docker+[kind,'inspect','--format','{{.Id}}',rid],env=env,text=True,capture_output=True)
   absent[rid]=r.returncode!=0 and ('No such' in r.stderr or 'not found' in r.stderr)
   if not absent[rid]:errors[rid]={'exit':r.returncode,'stderr':r.stderr}
 own=[]
 for line in subprocess.check_output(['ps','-eo','pid=,ppid=,args='],text=True).splitlines():
  vals=line.strip().split(None,2)
  if len(vals)==3 and (str(p) in vals[2] or '/workspace/agenteam-object-audit-verifier-build-frxgtjbx' in vals[2]) and int(vals[0])!=os.getpid():
   own.append({'pid':int(vals[0]),'ppid':int(vals[1]),'program':vals[2].split()[0]})
 result={'number':n,'epoch':time.time(),'baseline_equal':after==base,'resources':after,'owned_absent':absent,'inspect_errors':errors,'owned_processes':own,'runtime_entries':sum(1 for _ in pathlib.Path(chosen['TMPDIR']).rglob('*'))}
 (p/f'evidence/resource-zero-{n}.json').write_text(json.dumps(result,indent=2)+'\n')
 return result
checks=[final_check(1),final_check(2)]
result={'exit':code,'seconds':round(time.time()-start,3),'created_counts':{k:len(v) for k,v in created.items()},'checks':[{'baseline_equal':c['baseline_equal'],'all_owned_absent':all(c['owned_absent'].values()),'processes':c['owned_processes'],'runtime_entries':c['runtime_entries']} for c in checks]}
(p/'evidence/real-result.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result),flush=True)
for f,h in manifest['files'].items():assert hashlib.sha256((p/'tree'/f).read_bytes()).hexdigest()==h,f
sys.exit(code)
