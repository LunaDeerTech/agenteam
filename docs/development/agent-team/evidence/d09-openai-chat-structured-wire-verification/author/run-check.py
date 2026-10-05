import datetime,hashlib,json,os,pathlib,subprocess,sys
root=pathlib.Path(__file__).resolve().parent
label=sys.argv[1]; argv=sys.argv[2:]
if not argv or '/' in label: raise SystemExit('label and argv required')
repo=pathlib.Path('/workspace/agenteam');tree=root/'tree'
inputs={}
for name in json.loads((root/'owned-paths.json').read_text()):
 p=repo/name
 if p.exists():
  data=p.read_bytes();q=tree/name;q.parent.mkdir(parents=True,exist_ok=True)
  if not q.exists() or q.read_bytes()!=data: q.write_bytes(data)
  inputs[name]=hashlib.sha256(data).hexdigest()
envadd={'GOENV':'off','GOWORK':'off','GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off','GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':'/workspace/agenteam-d09-final-build-k1jf1jcx/gocache','TMPDIR':str(root/'tmp'),'GOFLAGS':'-mod=readonly','CGO_ENABLED':'1','GOTELEMETRY':'off'}
env=dict(os.environ);env.update(envadd)
meta={'argv':argv,'cwd':str(tree),'environment':envadd,'baseline':'ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7','owned_inputs':inputs,'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'log':label+'.log'}
path=root/'evidence'/(label+'.json');log=root/'evidence'/(label+'.log')
if path.exists() or log.exists(): raise SystemExit('preserve prior run; choose new label')
path.write_text(json.dumps(meta,indent=2)+'\n')
with log.open('wb') as out:
 result=subprocess.run(argv,cwd=tree,env=env,stdout=out,stderr=subprocess.STDOUT)
meta.update(exit_code=result.returncode,finished_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),log_sha256=hashlib.sha256(log.read_bytes()).hexdigest())
path.write_text(json.dumps(meta,indent=2)+'\n')
print(json.dumps({'label':label,'exit':result.returncode,'log':str(log),'metadata':str(path)}))
print(log.read_text(errors='replace')[-4000:])
sys.exit(result.returncode)
