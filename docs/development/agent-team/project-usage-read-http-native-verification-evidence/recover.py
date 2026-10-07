from pathlib import Path
import re,json,hashlib,time,os
B=Path('/workspace/scratch/usage-http-verification/native01');O=B/'native';R=B/'recovery';R.mkdir(exist_ok=False)
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def save(n,d):(R/n).write_text(json.dumps(d,indent=2)+'\n')
original={str(p.relative_to(B)):sha(p) for p in [B/'run.py',O/'trace.raw',O/'raw.log',O/'command.json',O/'ports.json',O/'cleanup-double-zero.json',O/'input-before.json',O/'input-after.json']}
assert original['run.py']=='f8edfd51dec7a9244bf4ba5e0283b5163350a117c4cdd3ff83ecf36276b91f3e'
cmd=json.loads((O/'command.json').read_text());before=json.loads((O/'input-before.json').read_text());assert cmd['exit']==0 and not cmd['pass'] and cmd['inputs_unchanged']
raw=(O/'trace.raw').read_text();pending={};calls=[]
for number,line in enumerate(raw.splitlines(),1):
 m=re.match(r'^(\d+) (\d+\.\d+) (.*)$',line)
 if not m:continue
 tid,when,body=m.groups()
 if body.endswith('<unfinished ...>'):
  assert tid not in pending,(tid,pending.get(tid),line)
  pending[tid]=(body.removesuffix('<unfinished ...>'),number,float(when));continue
 resumed=re.match(r'<\.\.\. (\w+) resumed>(.*)',body)
 if resumed:
  prefix,begin,timestamp=pending.pop(tid);assert prefix.startswith(resumed[1]+'(')
  body=prefix+resumed[2]
 else:begin=number;timestamp=float(when)
 calls.append({'tid':int(tid),'line':begin,'completed_line':number,'time':timestamp,'body':body})
assert not pending,pending
save('reassembled-calls.json',calls)
listeners={};local={};connections=[];accepted=[];created=[];closed=set();binds=[]
for c in calls:
 s=c['body'];im=re.search(r'<TCP(?:v6)?:\[(\d+)\]>',s)
 if s.startswith('socket(') and re.search(r'= \d+<TCP',s):created.append(int(im[1]))
 if s.startswith('close(') and im and s.endswith('= 0'):closed.add(int(im[1]))
 if s.startswith('listen(') and s.endswith('= 0'):
  assert im;inode=int(im[1]);assert inode not in listeners;listeners[inode]=c
 if s.startswith('getsockname(') and s.endswith('= 0'):
  pm=re.search(r'sin_port=htons\((\d+)\).*sin_addr=inet_addr\("127\.0\.0\.1"\)',s)
  assert im and pm,s;local[int(im[1])]=int(pm[1])
 if s.startswith('connect('):
  pm=re.search(r'sin_port=htons\((\d+)\).*sin_addr=inet_addr\("127\.0\.0\.1"\)',s)
  assert im and pm,s;connections.append({'inode':int(im[1]),'server_port':int(pm[1]),'source':c})
 if s.startswith('accept4(') and re.search(r'= \d+<TCP',s):accepted.append(c)
 if s.startswith('bind('):binds.append(c)
assert len(listeners)==10 and len(connections)==10 and len(accepted)==10
assert all(i in local for i in listeners)
listener_records=[{'inode':i,'port':local[i],'listen':c,'closed':i in closed} for i,c in listeners.items()]
for c in connections:c['client_port']=local[c['inode']];assert c['server_port'] in [r['port'] for r in listener_records]
ports=sorted(set(local.values()));inodes=set(created)|set(local)|set(listeners)
assert set(created)<=closed and all(r['closed'] for r in listener_records)
assert len(ports)==20 and len(created)==23 and len(closed)==33,(len(ports),len(created),len(closed))
# Three Go network capability sockets are immediately closed; only ten binds become listeners.
save('recovered-ports.json',{'listeners':listener_records,'connections':connections,'all_ports':ports,'created_inodes':created,'all_closed_inodes':sorted(closed),'binds':binds,'accepted_connections':len(accepted),'original_trace_sha256':sha(O/'trace.raw')})
def proc_stat(pid):
 try:
  raw=Path(f'/proc/{pid}/stat').read_text();f=raw.rsplit(') ',1)[1].split();return {'pid':pid,'starttime':f[19],'pgrp':int(f[2]),'state':f[0]}
 except (FileNotFoundError,ProcessLookupError):return None
owned=cmd['owned_processes'];scan_no=0
def scan():
 global scan_no
 scan_no+=1;now=time.time();processes=[]
 for s in owned:
  actual=proc_stat(s['pid'])
  if actual and actual['starttime']==s['starttime']:processes.append(actual)
 rows=[]
 for n in ('tcp','tcp6'):
  text=Path('/proc/net/'+n).read_text();(R/f'cleanup-{scan_no:02d}-{n}.raw').write_text(text)
  for line in text.splitlines()[1:]:
   f=line.split();lp=int(f[1].rsplit(':',1)[1],16);rp=int(f[2].rsplit(':',1)[1],16)
   if lp in ports or rp in ports or int(f[9]) in inodes:rows.append({'table':n,'local':f[1],'remote':f[2],'state':f[3],'inode':f[9],'raw':line})
 return {'at':now,'owned_processes':processes,'rows':rows,'active_rows':[r for r in rows if r['state']!='06'],'time_wait_rows':[r for r in rows if r['state']=='06']}
scans=[];zero=[];deadline=time.monotonic()+75
while True:
 s=scan();scans.append(s)
 if not s['owned_processes'] and not s['rows']:zero.append(s)
 else:zero=[]
 save('cleanup-observations.json',scans)
 if len(zero)>=2 or time.monotonic()>=deadline:break
 time.sleep(.2 if zero else 1)
assert len(zero)>=2
# Recompute the same accepted source closure, without importing execution driver.
A=Path('/workspace/scratch/usage-http-author');b=json.loads((A/'B/final-input.json').read_text());a=json.loads((A/'a03-execution-input.json').read_text());f=json.loads((A/'stage-a-final-input.json').read_text());known=a['files']|f['race_additional_inputs']|b['changed_known_inputs']|b['new_actual_inputs']
graph=(A/'B/b01-race-list/raw.log').read_text();dec=json.JSONDecoder();pos=0;paths=set();packages=0
while graph[pos:].strip():
 while graph[pos].isspace():pos+=1
 p,pos=dec.raw_decode(graph,pos);packages+=1
 for k in ('GoFiles','CgoFiles','CFiles','CXXFiles','MFiles','HFiles','FFiles','SFiles','SwigFiles','SwigCXXFiles','SysoFiles','EmbedFiles'):paths.update(str(Path(p['Dir'])/n) for n in p.get(k,[]))
actual={p:sha(p) for p in sorted(paths)};assert all(actual[p]==known[p]['sha256'] for p in paths)
actual.update({p:sha(p) for p in before['extra']});digest=hashlib.sha256(json.dumps(actual,sort_keys=True).encode()).hexdigest();assert digest==before['sha256']
assert all(sha(B/p)==s for p,s in original.items())
result={'recovery_script_sha256':sha(__file__),'original_artifacts_unchanged':original,'test_rerun':False,'original_wrapper_exit':1,'original_empty_port_cleanup_invalid':True,'test_binary_exit':cmd['exit'],'test_binary_execution_seconds':cmd['execution_elapsed_seconds'],'top_passes':3,'sub_passes':9,'actual_waits':cmd['actual_waits'],'listeners':len(listeners),'connection_pairs':len(connections),'ports':ports,'all_33_socket_inodes_close_success':len(closed)==33,'two_current_cleanup_scans':zero,'source_closure_packages':packages,'source_closure_files':len(paths),'source_closure_digest':digest,'source_closure_unchanged':True,'no_historical_time_wait_claim':True,'recovery_pass':True}
save('result.json',result);print(json.dumps(result,indent=2))
