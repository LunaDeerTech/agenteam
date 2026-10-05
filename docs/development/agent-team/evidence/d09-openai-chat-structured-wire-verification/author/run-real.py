import datetime,hashlib,json,os,pathlib,re,subprocess,sys,threading,time
from resources import live_baseline,same_baseline,owned_processes,process_info
root=pathlib.Path(__file__).resolve().parent
def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def write(p,value): p.write_text(json.dumps(value,indent=2)+'\n')
label=sys.argv[1]
assert re.fullmatch('[a-z0-9-]+',label)
for name in ['docker-config','docker-bin','runtime']: (root/name).mkdir(exist_ok=True)
log=root/'evidence'/(label+'.log'); inputs=root/'evidence'/(label+'-input.json'); result=root/'evidence'/(label+'.json')
assert not log.exists() and not inputs.exists() and not result.exists()
wrapper='''#!/usr/bin/python3
import json,os,re,signal,subprocess,sys,time
args=sys.argv[1:]
p=subprocess.Popen(['/usr/local/bin/docker',*args],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
def forward(sig,frame):
 try:p.send_signal(sig)
 except ProcessLookupError:pass
signal.signal(signal.SIGTERM,forward);signal.signal(signal.SIGINT,forward)
out,err=p.communicate()
entry={'at':time.time(),'operation':args[:2],'exit_code':p.returncode}
if args and (args[0]=='run' or args[:2]==['network','create']):
 value=out.decode('utf-8',errors='replace').strip()
 if re.fullmatch('[0-9a-f]{64}',value):
  entry['created_id']=value;entry['kind']='network' if args[0]=='network' else 'container'
 if '--name' in args:entry['name']=args[args.index('--name')+1]
 entry['labels']=[args[i+1] for i,v in enumerate(args[:-1]) if v=='--label']
 if 'created_id' in entry:
  inspect=['inspect','--format','{"id":{{json .Id}},"name":{{json .Name}},"labels":{{json .Config.Labels}}}',entry['created_id']]
  if entry['kind']=='network':inspect=['network','inspect','--format','{"id":{{json .Id}},"name":{{json .Name}},"labels":{{json .Labels}}}',entry['created_id']]
  try:
   check=subprocess.run(['/usr/local/bin/docker',*inspect],capture_output=True,timeout=20)
   entry['actual_inspect_exit']=check.returncode
   if check.returncode==0:entry['actual_resource']=json.loads(check.stdout)
   else:entry['actual_inspect_error']=check.stderr.decode(errors='replace')
  except Exception as e:entry['actual_inspect_error']=type(e).__name__

fd=os.open(OPLOG,os.O_WRONLY|os.O_CREAT|os.O_APPEND,0o600)
os.write(fd,(json.dumps(entry)+'\\n').encode());os.close(fd)
os.write(1,out);os.write(2,err);sys.exit(p.returncode)
'''.replace('OPLOG',repr(str(root/'evidence'/(label+'-docker.jsonl'))))
(root/'docker-bin/docker').write_text(wrapper);(root/'docker-bin/docker').chmod(0o700)
env=os.environ.copy();removed=[]
for name in ['AGENTEAM_PG_FIXTURE','AGENTEAM_PG_UNSUPPORTED_FIXTURE','AGENTEAM_OBJECT_FIXTURE','AGENTEAM_OUTBOUND_FIXTURE','DOCKER_CONTEXT','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH']:
 if name in env: removed.append(name);env.pop(name)
overrides={'GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off','GOENV':'off','GOWORK':'off','GOTELEMETRY':'off','CGO_ENABLED':'1','GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':'/workspace/agenteam-d09-final-build-k1jf1jcx/gocache','TMPDIR':str(root/'runtime'),'GOFLAGS':'-mod=readonly -v','AGENTEAM_GO':'/workspace/toolchains/go1.27.1/bin/go','AGENTEAM_MINIO_BINARY':'/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio','DOCKER_HOST':'unix:///var/run/docker.sock','DOCKER_CONFIG':str(root/'docker-config'),'PATH':str(root/'docker-bin')+':'+env['PATH']}
env.update(overrides)
assert sha(root/'evidence/trusted-resource-baseline.json')=='42a656cca5d3734df8826a44e5712b7ebffeaa1e6deca894650c68205826562d'
manifest=root/'candidate-review-02/manifest.json';candidate=json.loads(manifest.read_text())
for e in candidate['files']:assert sha(root/'tree'/e['path'])==e['sha256'],e['path']
owned={e['path'] for e in candidate['files']}
basefile=root/'evidence/baseline-source.json';base=json.loads(basefile.read_text())
for e in base['files']:
 if e['path'] not in owned:assert sha(root/'tree'/e['path'])==e['sha256'],e['path']
asset=root/'evidence/baseline-assets-supplement.json';a=json.loads(asset.read_text());assert sha(root/'tree'/a['path'])==a['sha256']
minio=sha(pathlib.Path(overrides['AGENTEAM_MINIO_BINARY']));assert minio=='dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'
selection_path=root/'evidence/real-selection-01.json';selection=json.loads(selection_path.read_text())
assert sha(manifest)==selection['candidate_sha256']
before=live_baseline(env);write(root/'evidence'/(label+'-baseline.json'),before)
assert same_baseline(before),'unexpected exact resource baseline; no fixture started'
argv=selection['argv']
marker=root.name+':'+label
env['AGENTEAM_STRUCTURED_AUTHOR_MARKER']=marker
record={'argv':argv,'cwd':str(root/'tree'),'environment':overrides,'removed_inherited_keys':removed,'candidate_manifest_sha256':sha(manifest),'baseline_manifest_sha256':sha(basefile),'asset_supplement_sha256':sha(asset),'selection_sha256':sha(selection_path),'baseline':candidate['baseline'],'minio_binary_sha256':minio,'resource_baseline':label+'-baseline.json','started_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'log':log.name,'driver_pid':os.getpid(),'runner_process':process_info(os.getpid()),'owned_process_marker':marker,'runner_sha256':sha(pathlib.Path(__file__)),'observer_sha256':sha(root/'resources.py'),'trusted_baseline_sha256':sha(root/'evidence/trusted-resource-baseline.json')}
write(inputs,record)
print(json.dumps({'status':'starting','pid':os.getpid(),'argv':argv,'input':str(inputs),'log':str(log)}),flush=True)
seen={};stop=threading.Event()
def observe_processes():
 while not stop.is_set():
  for info in owned_processes(marker):
   key=(info['pid'],info['start_ticks']);seen[key]=info
  stop.wait(0.1)
monitor=threading.Thread(target=observe_processes);monitor.start()
try:
 with log.open('wb') as output:
  p=subprocess.run(argv,cwd=root/'tree',env=env,stdout=output,stderr=subprocess.STDOUT)
finally:
 stop.set();monitor.join()
 write(root/'evidence'/(label+'-processes.json'),{'marker':marker,'observed_processes':list(seen.values()),'remaining':owned_processes(marker)})
record.update(exit_code=p.returncode,finished_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),log_sha256=sha(log))
write(result,record)
print(json.dumps({'exit':p.returncode,'log':str(log),'metadata':str(result)}),flush=True)
print(log.read_text(errors='replace')[-6000:])
sys.exit(p.returncode)
