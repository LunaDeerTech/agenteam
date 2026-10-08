import ctypes, datetime, json, os, pathlib, signal, subprocess, time
ROOT=pathlib.Path(__file__).resolve().parent
NODE='/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node'
assert ctypes.CDLL(None, use_errno=True).prctl(36,1,0,0,0) == 0
START=time.monotonic(); OWNER=os.getpid(); observed={}; waits=[]; actions=[]; errors=[]; scans=[]; child=None; direct=None

def now(): return datetime.datetime.now(datetime.timezone.utc).isoformat()
def snapshot():
 rows={}
 for p in pathlib.Path('/proc').iterdir():
  if not p.name.isdigit(): continue
  try:
   v=(p/'stat').read_text(); name=v[v.index('(')+1:v.rindex(')')]; cols=v[v.rindex(')')+2:].split(); pid=int(p.name)
   rows[pid]={'pid':pid,'starttime':cols[19],'ppid':int(cols[1]),'state':cols[0],'name':name}
  except (OSError,ValueError,IndexError): pass
 return rows

def owned():
 rows=snapshot(); ids={p for p,v in rows.items() if v['ppid']==OWNER or f"{p}:{v['starttime']}" in observed}
 changed=True
 while changed:
  old=len(ids); ids.update(p for p,v in rows.items() if v['ppid'] in ids); changed=len(ids)!=old
 for p in ids: observed[f"{p}:{rows[p]['starttime']}"]=rows[p]
 return [rows[p] for p in sorted(ids)]

def reap_adopted():
 for v in owned():
  if v['ppid']!=OWNER or child is not None and v['pid']==child.pid: continue
  try: pid,status=os.waitpid(v['pid'],os.WNOHANG)
  except ChildProcessError: continue
  if pid: waits.append({**v,'actual_wait':True,'exit':os.waitstatus_to_exitcode(status),'time':now()})

def signal_owned(sig):
 for v in reversed(owned()):
  if v['state']=='Z': continue
  try: os.kill(v['pid'],sig); actions.append({'pid':v['pid'],'starttime':v['starttime'],'signal':sig,'time':now()})
  except ProcessLookupError: pass

raw=(ROOT/'raw.log').open('w')
try:
 child=subprocess.Popen([NODE,str(ROOT/'experiment.cjs')],cwd=ROOT,stdin=subprocess.DEVNULL,stdout=raw,stderr=subprocess.STDOUT,start_new_session=True,env={**os.environ,'TMPDIR':str(ROOT/'runtime')})
 while child.poll() is None and time.monotonic()-START<10:
  owned(); reap_adopted(); time.sleep(.05)
 if child.poll() is None: errors.append('work deadline 10s'); signal_owned(signal.SIGTERM)
finally:
 if child is not None:
  while child.poll() is None and time.monotonic()-START<12:
   owned(); reap_adopted(); time.sleep(.05)
  if child.poll() is None: signal_owned(signal.SIGKILL)
  try: direct=child.wait(timeout=max(.1,14-time.monotonic()+START))
  except subprocess.TimeoutExpired: errors.append('direct wait exceeded')
  while time.monotonic()-START<14:
   reap_adopted(); remaining=owned()
   if not remaining: break
   if time.monotonic()-START>=12: signal_owned(signal.SIGKILL)
   time.sleep(.05)
 for n in range(2):
  reap_adopted(); scans.append({'time':now(),'owned':owned()}); time.sleep(.1)
 raw.close()
 elapsed=time.monotonic()-START
 report={'status':'PASS' if direct==0 and not errors and not actions and all(not x['owned'] for x in scans) and elapsed<15 else 'FAIL','seconds_including_cleanup':elapsed,'limit_seconds':15,'supervisor_pid':OWNER,'direct_pid':child.pid if child else None,'direct_actual_wait':direct is not None,'direct_exit':direct,'adopted_waits':waits,'observed':observed,'actions':actions,'errors':errors,'owned_double_scan':scans,'runtime_entries':sorted(str(x.relative_to(ROOT/'runtime')) for x in (ROOT/'runtime').rglob('*'))}
 (ROOT/'retirement.json').write_text(json.dumps(report,indent=2)+'\n')
 print(json.dumps({k:report[k] for k in ['status','seconds_including_cleanup','direct_actual_wait','direct_exit','actions','errors','owned_double_scan','runtime_entries']}))
 if report['status']!='PASS': raise SystemExit(1)
