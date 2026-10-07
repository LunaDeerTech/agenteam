from pathlib import Path
import os,sys,json,hashlib,subprocess,time,signal,ctypes,re,shutil
BASE=Path('/workspace/scratch/usage-http-verification/native01')
REPO=Path('/workspace/agenteam'); AUTHOR=Path('/workspace/scratch/usage-http-author')
MODE=sys.argv[1]
assert MODE in ('probe','native')
OUT=BASE/MODE;OUT.mkdir(exist_ok=False)
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def save(name,data): (OUT/name).write_text(json.dumps(data,indent=2)+'\n')
def stat(pid):
 try:
  raw=Path(f'/proc/{pid}/stat').read_text();f=raw.rsplit(') ',1)[1].split()
  return {'pid':int(pid),'starttime':f[19],'ppid':int(f[1]),'pgrp':int(f[2]),'state':f[0],'name':raw.split('(',1)[1].rsplit(')',1)[0]}
 except (FileNotFoundError,ProcessLookupError,PermissionError):return None
def procscan():return [s for p in Path('/proc').iterdir() if p.name.isdigit() and (s:=stat(p.name))]
def closure():
 b=json.loads((AUTHOR/'B/final-input.json').read_text());a=json.loads((AUTHOR/'a03-execution-input.json').read_text());f=json.loads((AUTHOR/'stage-a-final-input.json').read_text())
 assert sha(AUTHOR/'B/final-input.json')=='f99553429fffdb163086eadda94e28d2cbc1d78f80cff1b4955f5530faf5f3a7'
 known=a['files']|f['race_additional_inputs']|b['changed_known_inputs']|b['new_actual_inputs']
 graph=AUTHOR/'B/b01-race-list/raw.log';assert sha(graph)==b['actual_b_race_graph_sha256']
 raw=graph.read_text();dec=json.JSONDecoder();pos=0;paths=set();packages=0
 while raw[pos:].strip():
  while raw[pos].isspace():pos+=1
  p,pos=dec.raw_decode(raw,pos);packages+=1
  for k in ('GoFiles','CgoFiles','CFiles','CXXFiles','MFiles','HFiles','FFiles','SFiles','SwigFiles','SwigCXXFiles','SysoFiles','EmbedFiles'):
   paths.update(str(Path(p['Dir'])/n) for n in p.get(k,[]))
 assert packages==379 and len(paths)==2349
 actual={p:sha(p) for p in sorted(paths)}
 assert all(actual[p]==known[p]['sha256'] for p in paths)
 extra=[AUTHOR/'B/final-input.json',graph,AUTHOR/'B/b02-compile/command.json',AUTHOR/'B/usage-http.test',BASE/'run.py',Path('/usr/bin/strace'),Path(sys.executable).resolve(),REPO/'go.mod',REPO/'go.sum']
 actual.update({str(p):sha(p) for p in extra})
 assert actual[str(AUTHOR/'B/usage-http.test')]=='fc86e607bc6b9f5abebdca787634b06fb79ad8a012451ccb11235dbe9c7acf50'
 return {'actual_packages':packages,'actual_go_source_files':len(paths),'sha256':hashlib.sha256(json.dumps(actual,sort_keys=True).encode()).hexdigest(),'extra':{str(p):actual[str(p)] for p in extra}}
libc=ctypes.CDLL(None,use_errno=True)
if libc.prctl(36,1,0,0,0)!=0:raise OSError(ctypes.get_errno(),'subreaper failed')
assert shutil.disk_usage(BASE).free>=2*1024**3 and shutil.disk_usage('/tmp').free>=2*1024**3
before=closure();save('input-before.json',before)
for n in ('tcp','tcp6','unix'):(OUT/f'baseline-{n}.raw').write_text(Path('/proc/net/'+n).read_text())
initial=procscan();save('baseline-processes.json',initial)
tmp=OUT/'tmp';tmp.mkdir()
env={'PATH':'/workspace/toolchains/go1.27.1/bin:/usr/bin:/bin','HOME':'/home/agent','TMPDIR':str(tmp),'GOTOOLCHAIN':'local','GOENV':'off','GOPROXY':'off','GOSUMDB':'off','GOPATH':'/workspace/go','GOMODCACHE':'/workspace/go/pkg/mod','GOCACHE':'/home/agent/.cache/go-build','GOFLAGS':'-mod=readonly','GOMAXPROCS':'2'}
if MODE=='native':env['AGENTEAM_USAGE_HTTP_NATIVE']='1'
binary=[str(AUTHOR/'B/usage-http.test'),'-test.count=1','-test.parallel=1','-test.timeout=45s','-test.run=^TestProjectUsageHTTPNative(SlowBody|WriteAndClose|KeepAlive)$','-test.v'] if MODE=='native' else ['/bin/sleep','0.25']
command=['/usr/bin/strace','-D','-f','-ttt','-yy','-e','trace=%network,%process,close','-o',str(OUT/'trace.raw')]+binary
record={'mode':MODE,'driver_sha256':sha(__file__),'driver_pid':os.getpid(),'driver_stat':stat(os.getpid()),'subreaper':True,'command':command,'env':env,'cwd':str(REPO),'budget_seconds':45,'cleanup_grace_seconds':15,'started':time.time(),'before':before}
owned={};waits=[];signals=[]
def observe(pgid):
 allp=procscan();ids={os.getpid()}|{x['pid'] for x in owned.values()}
 while True:
  children={s['pid'] for s in allp if s['ppid'] in ids or s['pgrp']==pgid}
  if children<=ids:break
  ids|=children
 for s in allp:
  if s['pid']!=os.getpid() and s['pid'] in ids:
   key=(s['pid'],s['starttime'])
   if key not in owned:owned[key]=s|{'first_observed':time.time()}
 return [s for s in allp if (s['pid'],s['starttime']) in owned]
def send_owned(sig):
 members=observe(p.pid)
 if members:
  try:os.killpg(p.pid,sig);signals.append({'signal':sig,'time':time.time(),'members':members})
  except ProcessLookupError:pass
with (OUT/'raw.log').open('wb') as log:
 started=time.monotonic();p=subprocess.Popen(command,cwd=REPO,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 first=stat(p.pid);assert first;owned[(p.pid,first['starttime'])]=first|{'first_observed':time.time()};record['direct_pid']=first;save('command.json',record)
 while True:
  observe(p.pid)
  code=p.poll()
  if code is not None:break
  if time.monotonic()-started>=45:
   record['execution_budget_expired']=True;send_owned(signal.SIGTERM)
   try:code=p.wait(timeout=3)
   except subprocess.TimeoutExpired:send_owned(signal.SIGKILL);code=p.wait(timeout=3)
   break
  time.sleep(.01)
 # Popen.poll/wait has performed waitpid for the direct tracee process.
 record['execution_elapsed_seconds']=time.monotonic()-started
 waits.append({'role':'direct','pid':p.pid,'starttime':first['starttime'],'returncode':code,'actual_wait':True,'time':time.time()})
 cleanup_start=time.monotonic()
 while True:
  observe(p.pid)
  try:info=os.waitid(os.P_ALL,0,os.WEXITED|os.WNOHANG|os.WNOWAIT)
  except ChildProcessError:break
  if info is not None:
   s=stat(info.si_pid);assert s,info;owned.setdefault((s['pid'],s['starttime']),s|{'first_observed':time.time()})
   got,status=os.waitpid(info.si_pid,0);waits.append({'role':'adopted','pid':got,'starttime':s['starttime'],'wait_status':status,'returncode':os.waitstatus_to_exitcode(status),'actual_wait':True,'time':time.time()});continue
  elapsed=time.monotonic()-cleanup_start
  if elapsed>=15:raise RuntimeError('owned child did not join within cleanup grace')
  if elapsed>=6:send_owned(signal.SIGKILL)
  elif elapsed>=3:send_owned(signal.SIGTERM)
  time.sleep(.01)
record.update(exit=code,actual_waits=waits,signals=signals,owned_processes=list(owned.values()),execution_finished=time.time())
save('command.json',record)
trace=(OUT/'trace.raw').read_text()
ports=sorted({int(x) for x in re.findall(r'127\.0\.0\.1:(\d+)',trace)}-{0})
listen_lines=[line for line in trace.splitlines() if 'listen(' in line or ('<... listen resumed>' in line)]
listen_ports=sorted({int(x) for line in listen_lines for x in re.findall(r'127\.0\.0\.1:(\d+)',line)}-{0})
save('ports.json',{'ports':ports,'listener_ports':listen_ports,'listen_syscall_lines':listen_lines})
def cleanup_scan():
 processes=observe(p.pid);matches=[]
 for n in ('tcp','tcp6'):
  for line in Path('/proc/net/'+n).read_text().splitlines()[1:]:
   f=line.split();lp=int(f[1].rsplit(':',1)[1],16);rp=int(f[2].rsplit(':',1)[1],16)
   if lp in ports or rp in ports:matches.append({'table':n,'local':f[1],'remote':f[2],'state':f[3],'inode':f[9],'raw':line})
 return {'at':time.time(),'owned_processes':processes,'owned_port_rows':matches,'active_port_rows':[x for x in matches if x['state']!='06'],'time_wait_rows':[x for x in matches if x['state']=='06']}
scans=[];deadline=time.monotonic()+75;zero=[]
while True:
 check=cleanup_scan();scans.append(check)
 if not check['owned_processes'] and not check['owned_port_rows']:zero.append(check)
 else:zero=[]
 if len(zero)>=2 or time.monotonic()>=deadline:break
 time.sleep(.2 if zero else 1)
save('cleanup-observations.json',scans);save('cleanup-double-zero.json',zero)
after=closure();save('input-after.json',after)
record.update(after=after,inputs_unchanged=before==after,finished=time.time(),listener_ports=listen_ports,all_ports=ports,double_all_port_and_process_zero=len(zero)>=2,trace_sha256=sha(OUT/'trace.raw'),raw_sha256=sha(OUT/'raw.log'))
if MODE=='native':record['expected_listeners_observed']=len(listen_ports)==10
record['pass']=code==0 and all(w['returncode']==0 for w in waits) and not signals and before==after and len(zero)>=2 and (MODE=='probe' or len(listen_ports)==10)
save('command.json',record)
print(json.dumps({k:record[k] for k in ('mode','exit','execution_elapsed_seconds','actual_waits','listener_ports','inputs_unchanged','double_all_port_and_process_zero','pass')},indent=2));sys.exit(0 if record['pass'] else 1)
