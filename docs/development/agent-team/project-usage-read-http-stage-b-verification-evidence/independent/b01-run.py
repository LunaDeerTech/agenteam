import os,sys,json,subprocess,time,pathlib,hashlib,signal,ctypes
base=pathlib.Path('/workspace/scratch/usage-http-verification/b01');label=sys.argv[1];cmd=sys.argv[2:];out=base/label;out.mkdir(exist_ok=False)
assert ctypes.CDLL(None,use_errno=True).prctl(36,1,0,0,0)==0
repo=pathlib.Path('/workspace/agenteam');frozen=json.loads((base/'inputs.json').read_text())
def hashes():return {p:hashlib.sha256((repo/p).read_bytes()).hexdigest() for p in frozen}
before=hashes();assert all(before[p]==v['sha256'] for p,v in frozen.items())
env={**os.environ,'GOTOOLCHAIN':'local','GOENV':'off','GOPROXY':'off','GOSUMDB':'off','GOPATH':'/workspace/go','GOMODCACHE':'/workspace/go/pkg/mod','GOCACHE':'/home/agent/.cache/go-build','GOFLAGS':'-mod=readonly','GOMAXPROCS':'2','TMPDIR':str(out),'PYTHONDONTWRITEBYTECODE':'1'}
for k in list(env):
 if k.startswith('AGENTEAM_'):del env[k]
record={'argv':cmd,'cwd':str(repo),'env_overrides':{k:env[k] for k in ['GOTOOLCHAIN','GOENV','GOPROXY','GOSUMDB','GOPATH','GOMODCACHE','GOCACHE','GOFLAGS','GOMAXPROCS','TMPDIR','PYTHONDONTWRITEBYTECODE']},'before':before,'started':time.time(),'timeout_seconds':45,'adopted_waits':[]}
start=time.monotonic()
with (out/'raw.log').open('wb') as raw:
 p=subprocess.Popen(cmd,cwd=repo,env=env,stdout=raw,stderr=subprocess.STDOUT,start_new_session=True);record['pid']=p.pid
 record['starttime']=pathlib.Path('/proc/'+str(p.pid)+'/stat').read_text().split(') ',1)[1].split()[19]
 try:record['returncode']=p.wait(timeout=45)
 except subprocess.TimeoutExpired:
  record['timeout']=True;os.killpg(p.pid,signal.SIGTERM)
  try:record['returncode']=p.wait(timeout=3)
  except subprocess.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);record['returncode']=p.wait()
 while True:
  try:pid,status=os.waitpid(-1,os.WNOHANG)
  except ChildProcessError:break
  if pid==0:raise RuntimeError('owned descendant remains after command completion')
  record['adopted_waits'].append({'pid':pid,'status':status})
record.update(elapsed_seconds=time.monotonic()-start,after=hashes(),finished=time.time(),direct_pid_absent=not pathlib.Path('/proc/'+str(p.pid)).exists())
record['unchanged']=record['before']==record['after'];record['raw_sha256']=hashlib.sha256((out/'raw.log').read_bytes()).hexdigest()
(out/'command.json').write_text(json.dumps(record,indent=2)+'\n');print((out/'raw.log').read_text());print(json.dumps({k:record[k] for k in ['returncode','elapsed_seconds','direct_pid_absent','unchanged','adopted_waits']}));sys.exit(record['returncode'] if record['returncode']>=0 else 128-record['returncode'])
