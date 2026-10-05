import sys,subprocess,pathlib,json,os,time,hashlib,shutil
root=pathlib.Path(__file__).parent
repo=pathlib.Path('/workspace/agenteam')
paths=json.loads((root/'paths.json').read_text())
label=sys.argv[1]; argv=sys.argv[2:]
assert label and not (root/'logs'/(label+'.json')).exists(),label
inputs=[]
for path in paths:
 p=repo/path; q=root/'snapshot'/path
 if p.exists():
  q.parent.mkdir(parents=True,exist_ok=True)
  if os.environ.get('RUN_FIXED_INPUT')=='1':
   assert q.read_bytes()==p.read_bytes(),path
  else: shutil.copyfile(p,q)
  inputs.append({'path':path,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()})
 else: inputs.append({'path':path,'sha256':None})
env=os.environ.copy()
settings={'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOFLAGS':'-mod=readonly -buildvcs=false -p=2','GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':str(root/'gocache'),'GOTMPDIR':str(root/'gotmp'),'TMPDIR':str(root/'runtime'),'GOMAXPROCS':'4','AGENTEAM_GO':'/workspace/toolchains/go1.27.1/bin/go','AGENTEAM_MINIO_BINARY':'/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio','CGO_ENABLED':'1'}
env.update(settings)
record={'argv':argv,'cwd':str(root/'snapshot'),'env':settings,'inputs':inputs,'started_unix':time.time()}
with (root/'logs'/(label+'.log')).open('wb') as log:
 result=subprocess.run(argv,cwd=root/'snapshot',env=env,stdout=log,stderr=subprocess.STDOUT)
if result.returncode:
 saved=root/'inputs'/label
 saved.mkdir(exist_ok=True)
 for row in inputs:
  if row['sha256']:
   src=root/'snapshot'/row['path']; dst=saved/row['path'];dst.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(src,dst)
record.update(exit_code=result.returncode,elapsed_seconds=round(time.time()-record['started_unix'],3))
(root/'logs'/(label+'.json')).write_text(json.dumps(record,indent=2)+'\n')
print(json.dumps({'label':label,'argv':argv,'exit':result.returncode,'seconds':record['elapsed_seconds']}))
print((root/'logs'/(label+'.log')).read_text(errors='replace')[-14000:])
sys.exit(result.returncode)
