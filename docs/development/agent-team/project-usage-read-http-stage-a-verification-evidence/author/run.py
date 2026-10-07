from pathlib import Path
import os,sys,json,subprocess,time,hashlib,signal,ctypes
base=Path('/workspace/scratch/usage-http-author'); root=Path('/workspace/agenteam')
label=sys.argv[1]; command=sys.argv[2:];out=base/label;out.mkdir(exist_ok=False)
env={'PATH':'/workspace/toolchains/go1.27.1/bin:/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin:/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin:/usr/bin:/bin','HOME':'/home/agent','GOTOOLCHAIN':'local','GOENV':'off','GOPROXY':'off','GOSUMDB':'off','GOPATH':'/workspace/go','GOMODCACHE':'/workspace/go/pkg/mod','GOCACHE':'/home/agent/.cache/go-build','GOFLAGS':'-mod=readonly','GOMAXPROCS':'2','AGENTEAM_USAGE_SCHEMA_PYTHON':sys.executable,'AGENTEAM_USAGE_SCHEMA_NODE':'/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node','AGENTEAM_USAGE_SCHEMA_EVIDENCE':str(out/'schema')}
paths=['internal/central/account/http_boundary.go','internal/central/account/http_boundary_test.go','internal/central/usage/http/wire.go','internal/central/usage/http/wire_test.go','api/openapi/project-usage.json','api/openapi/common.json','go.mod','go.sum']
def hashes():return {name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in paths}
libc=ctypes.CDLL(None,use_errno=True)
if libc.prctl(36,1,0,0,0)!=0:raise OSError(ctypes.get_errno(),'subreaper setup failed')
record={'driver_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'subreaper':True,'cwd':str(root),'command':command,'env':env,'before':hashes(),'started':time.time()};(out/'command.json').write_text(json.dumps(record,indent=2)+'\n')
with (out/'raw.log').open('wb') as log:
 p=subprocess.Popen(command,cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 record['pid']=p.pid
 try:code=p.wait(timeout=45)
 except subprocess.TimeoutExpired:
  record['timeout']=True;os.killpg(p.pid,signal.SIGTERM)
  try:code=p.wait(timeout=3)
  except subprocess.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);code=p.wait()
adopted=[];limit=time.monotonic()+3;kill_sent=False
while True:
 try: child,status=os.waitpid(-1,os.WNOHANG)
 except ChildProcessError:break
 if child:adopted.append({'pid':child,'wait_status':status});continue
 if time.monotonic()>limit and not kill_sent:
  try:os.killpg(p.pid,signal.SIGKILL)
  except ProcessLookupError:pass
  kill_sent=True;limit=time.monotonic()+3
 elif time.monotonic()>limit:raise RuntimeError('owned children did not reach terminal wait')
 time.sleep(.02)
record.update(adopted_waits=adopted,exit=code,finished=time.time(),after=hashes());(out/'command.json').write_text(json.dumps(record,indent=2)+'\n')
print((out/'raw.log').read_text());print(json.dumps({'label':label,'exit':code,'elapsed':record['finished']-record['started'],'unchanged':record['before']==record['after']}));sys.exit(code if code>=0 else 128-code)
