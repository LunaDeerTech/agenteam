import datetime,json,os,pathlib,subprocess,time
ROOT=pathlib.Path(__file__).resolve().parent

def docker(args,env):
 return subprocess.run(['/usr/local/bin/docker',*args],env=env,capture_output=True,timeout=20)

def live_baseline(env):
 result={'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'containers':[],'networks':[]}
 for kind,listargs in [('containers',['ps','-aq','--no-trunc']),('networks',['network','ls','-q','--no-trunc'])]:
  p=docker(listargs,env);assert p.returncode==0,p.stderr.decode(errors='replace')
  for value in p.stdout.decode().split():
   args=['inspect','--format','{"id":{{json .Id}},"name":{{json .Name}},"labels":{{json .Config.Labels}}}',value]
   if kind=='networks':args=['network','inspect','--format','{"id":{{json .Id}},"name":{{json .Name}},"labels":{{json .Labels}}}',value]
   p=docker(args,env);assert p.returncode==0,p.stderr.decode(errors='replace')
   item=json.loads(p.stdout);item['labels']=item['labels'] or {};result[kind].append(item)
  result[kind].sort(key=lambda x:x['id'])
 return result

def expected_baseline():
 raw=json.loads((ROOT/'evidence/trusted-resource-baseline.json').read_text())
 return {out:sorted([{'id':x['ID'],'name':x['Name'],'labels':x['Labels'] or {}} for x in raw[kind]],key=lambda x:x['id']) for kind,out in [('container','containers'),('network','networks')]}

def same_baseline(actual):
 expected=expected_baseline()
 return all(actual[kind]==expected[kind] for kind in expected)

def process_info(pid):
 base=pathlib.Path('/proc')/str(pid)
 try:
  stat=(base/'stat').read_text();fields=stat[stat.rfind(')')+2:].split()
  return {'pid':int(pid),'ppid':int(fields[1]),'start_ticks':fields[19],'state':fields[0],'executable':os.readlink(base/'exe'),'cwd':os.readlink(base/'cwd')}
 except (FileNotFoundError,ProcessLookupError,PermissionError):return None

def owned_processes(marker):
 needle=('AGENTEAM_STRUCTURED_AUTHOR_MARKER='+marker).encode();result=[]
 for base in pathlib.Path('/proc').iterdir():
  if not base.name.isdigit():continue
  try:
   if needle not in (base/'environ').read_bytes().split(b'\0'):continue
  except (FileNotFoundError,ProcessLookupError,PermissionError):continue
  info=process_info(base.name)
  if info:result.append(info)
 return sorted(result,key=lambda x:x['pid'])

def cleanup_observation(label,env):
 operations=[json.loads(x) for x in (ROOT/'evidence'/(label+'-docker.jsonl')).read_text().splitlines()]
 created={x['created_id']:x for x in operations if 'created_id' in x}
 checks=[]
 for value,entry in created.items():
  args=['inspect','--format','{{.Id}}',value] if entry['kind']=='container' else ['network','inspect','--format','{{.Id}}',value]
  p=docker(args,env);stderr=p.stderr.decode(errors='replace').strip()
  absent=p.returncode!=0 and value in stderr and ('No such' in stderr or 'not found' in stderr)
  observed=entry.get('actual_resource')
  labels=dict(x.split('=',1) for x in entry['labels'])
  valid=(entry.get('actual_inspect_exit')==0 and observed is not None and observed['id']==value and all(observed['labels'].get(k)==v for k,v in labels.items()) and bool(labels) and (not entry.get('name') or observed['name'].lstrip('/')==entry['name']))
  checks.append({'id':value,'kind':entry['kind'],'actual_live_identity_verified':valid,'actual_resource':observed,'inspect_argv':args,'inspect_exit':p.returncode,'stderr':stderr,'absent':absent})
 record=json.loads((ROOT/'evidence'/(label+'.json')).read_text())
 processes=owned_processes(record['owned_process_marker'])
 runner=process_info(record['runner_process']['pid'])
 runner_absent=runner is None or runner['start_ticks']!=record['runner_process']['start_ticks']
 runtime=sorted(str(p.relative_to(ROOT/'runtime')) for p in (ROOT/'runtime').rglob('*'))
 baseline=live_baseline(env)
 return {'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'created_resources':checks,'owned_processes':processes,'runner_absent':runner_absent,'runtime_entries':runtime,'baseline':baseline,'baseline_exact_unchanged':same_baseline(baseline),'complete':bool(checks) and all(x['actual_live_identity_verified'] and x['absent'] for x in checks) and not processes and runner_absent and not runtime and same_baseline(baseline)}
