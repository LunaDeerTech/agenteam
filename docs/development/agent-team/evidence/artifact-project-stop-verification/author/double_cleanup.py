import pathlib,subprocess,os,json,datetime,hashlib,sys
r=pathlib.Path('/tmp/agenteam-artifact-stop-author-y597q6vl');label=sys.argv[1];first=json.loads((r/f'cleanup-{label}.json').read_text());e=os.environ.copy();e.pop('DOCKER_CONTEXT',None);e.update(DOCKER_HOST='unix:///var/run/docker.sock',DOCKER_CONFIG=str(r/'docker-config'));out={'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'first':f'cleanup-{label}.json','second':{}}
def norm(x):
 d={}
 for line in x.splitlines():
  a=line.split(' ',2);d[a[0]]=[a[1],sorted(a[2].split(',')) if len(a)>2 and a[2] else []]
 return d
for kind in ['containers','networks']:
 checks={}
 for uid in first[kind]['owned']:
  p=subprocess.run(['docker','container' if kind=='containers' else 'network','inspect','--format','{{.Id}}',uid],env=e,capture_output=True,text=True);checks[uid]={'exit':p.returncode,'output':(p.stdout+p.stderr).strip()};assert p.returncode!=0
 cmd=['docker','ps','-a','--no-trunc','--format','{{.ID}} {{.Names}} {{.Labels}}'] if kind=='containers' else ['docker','network','ls','--no-trunc','--format','{{.ID}} {{.Name}} {{.Labels}}']
 now=subprocess.check_output(cmd,env=e,text=True);out['second'][kind]={'owned':checks,'baseline_equal':norm(now)==norm((r/f'{label}-before-{kind}.txt').read_text())};assert out['second'][kind]['baseline_equal']
rt=pathlib.Path('/workspace/agenteam-artifact-stop-build-y597q6vl/runtime');out['runtime_entries']=[str(p.relative_to(rt)) for p in rt.rglob('*')];assert not out['runtime_entries'];procs=[]
for p in pathlib.Path('/proc').iterdir():
 if not p.name.isdigit():continue
 try:x=str((p/'exe').readlink())
 except (FileNotFoundError,PermissionError,ProcessLookupError):continue
 if 'objects.test' in x or str(rt) in x or '/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio' in x:procs.append({'pid':p.name,'exe':x})
out['owned_processes']=procs;assert not procs
m=json.loads((r/f'input-{label}.json').read_text());out['source16_end_match']=all(hashlib.sha256((r/'snapshot'/f).read_bytes()).hexdigest()==h and hashlib.sha256((pathlib.Path('/workspace/agenteam')/f).read_bytes()).hexdigest()==h for f,h in m['files'].items());assert out['source16_end_match']
p=r/f'cleanup-double-{label}.json';p.write_text(json.dumps(out,indent=2)+'\n');print('second cleanup owned exactIDs absent, baseline equal, runtime/process 0, source16 match')
for f in [p.name,f'input-{label}.json',f'logs/{label}.log']:print(f,hashlib.sha256((r/f).read_bytes()).hexdigest())
