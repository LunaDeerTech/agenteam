import datetime,json,os,pathlib,subprocess,sys,time,threading
p=pathlib.Path(__file__).parent
name=sys.argv[1];cmd=sys.argv[2:]
env=dict(os.environ,GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOMODCACHE='/workspace/agenteam-dependency-cache/modcache',GOCACHE=str(p/'gocache'),GOTMPDIR=str(p/'tmp'),TMPDIR=str(p/'runtime'),GOPROXY='off',GOFLAGS='-mod=readonly -v',AGENTEAM_GO='/workspace/toolchains/go1.27.1/bin/go',AGENTEAM_MINIO_BINARY='/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio',DOCKER_HOST='unix:///var/run/docker.sock',DOCKER_CONFIG=str(p/'docker-config'))
log=p/'logs'/(name+'.log');start=datetime.datetime.now(datetime.timezone.utc).isoformat();begin=time.monotonic()
done=threading.Event();owned={'container':set(),'network':set()};baseline=json.loads((p/'docker-baseline.json').read_text())
def observe():
 while not done.wait(0.4):
  for kind in owned:
   args=['docker',kind,'ls','-q','--no-trunc']
   if kind=='container':args.insert(3,'-a')
   try:owned[kind].update(set(subprocess.check_output(args,env=env,text=True).split())-{v['id'] for v in baseline[kind]})
   except subprocess.CalledProcessError:pass
observer=None
if cmd[:2]==['sh','scripts/test-objects.sh']:
 observer=threading.Thread(target=observe);observer.start()
with log.open('w') as f:
 f.write(json.dumps({'command':cmd,'cwd':str(p/'snapshot'),'started':start})+'\n');f.flush()
 r=subprocess.run(cmd,cwd=p/'snapshot',env=env,stdout=f,stderr=subprocess.STDOUT)
done.set()
if observer is not None:
 observer.join()
 (p/(name+'-owned-ids.json')).write_text(json.dumps({k:sorted(v) for k,v in owned.items()},indent=2)+'\n')
result={'name':name,'command':cmd,'started':start,'seconds':round(time.monotonic()-begin,3),'exit':r.returncode,'log':str(log)}
with (p/'results.jsonl').open('a') as f:f.write(json.dumps(result)+'\n')
print(json.dumps(result));print(log.read_text()[-6500:]);sys.exit(r.returncode)
