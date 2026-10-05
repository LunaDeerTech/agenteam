#!/usr/bin/env python3
import datetime, hashlib, json, os, pathlib, subprocess, time
r=pathlib.Path(__file__).resolve().parent
review=r.parent
old=pathlib.Path('/workspace/agenteam-artifact-foreign-v-4jgd5ed_')
tree=old/'tree'
sha=lambda p:hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
write=lambda name,value:(r/name).write_text(json.dumps(value,indent=2,sort_keys=True)+'\n')
stamp=lambda:datetime.datetime.now(datetime.timezone.utc).isoformat()
(r/'docker-config').mkdir();(r/'runtime').mkdir()
tool='/workspace/toolchains/go1.27.1/bin/go'
minio='/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio'
overlay=review/'probe-compile/overlay.json'
candidate=json.loads((review/'evidence/candidate-manifest.json').read_text())
replacements=json.loads(overlay.read_text())['Replace']
assert sha(review/'evidence/candidate-manifest.json')=='79e5eb812c0fda2a42d390a7e2703b1ebca4ce581c055e1c2a99fda1ab76914e'
assert sha(overlay)=='4fdd115fc9104e1557a6c7d8a41e47446e32055418e2e878c3522b6efabf7c9d'
probe=tree/'tests/objects/artifact_independent_foreign_result_test.go'
assert sha(probe)=='c40b8aeb6f574a2bbd7674f77b5ed5fb8c3311a1d367bc8302efaa2b199e5350'
def identities():
 return {name:sha(pathlib.Path(replacements.get(str(tree/name),str(tree/name)))) for name in candidate['files']}
assert identities()==candidate['files']
assert sha(minio)=='dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'
env=dict(os.environ)
for key in ['DOCKER_CONTEXT','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH','AGENTEAM_OBJECT_FIXTURE','AGENTEAM_NET_FIXTURE','AGENTEAM_POSTGRES_FIXTURE','AGENTEAM_OBJECT_GUARD_CHILD']:
 env.pop(key,None)
env.update({'DOCKER_HOST':'unix:///var/run/docker.sock','DOCKER_CONFIG':str(r/'docker-config'),'AGENTEAM_GO':tool,'AGENTEAM_MINIO_BINARY':minio,'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOFLAGS':'-mod=readonly -v -overlay='+str(overlay),'GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':str(old/'gocache'),'TMPDIR':str(r/'runtime'),'GOTMPDIR':str(r/'runtime')})
argv=['sh','scripts/test-objects.sh','-run','^TestIndependentArtifactForeignJoinCommittedStoreUnknown$']
write('command.json',{'argv':argv,'cwd':str(tree),'environment':{k:env[k] for k in ['DOCKER_HOST','DOCKER_CONFIG','AGENTEAM_GO','AGENTEAM_MINIO_BINARY','GOTOOLCHAIN','GOENV','GOWORK','GOPROXY','GOSUMDB','GOFLAGS','GOMODCACHE','GOCACHE','TMPDIR','GOTMPDIR']},'candidate_manifest_sha256':sha(review/'evidence/candidate-manifest.json'),'overlay_sha256':sha(overlay),'probe_sha256':sha(probe),'source_before':identities(),'minio_sha256':sha(minio),'go_binary_sha256':sha(tool),'driver_script_sha256':sha(tree/'scripts/test-objects.sh'),'go_mod_sha256':sha(tree/'go.mod'),'go_sum_sha256':sha(tree/'go.sum')})
def docker(args):
 return subprocess.run(['docker',*args],env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
def resources():
 output={}
 for kind in ['containers','networks']:
  ls=docker(['ps','-aq','--no-trunc'] if kind=='containers' else ['network','ls','-q','--no-trunc'])
  if ls.returncode:raise RuntimeError('docker list failed '+ls.stderr)
  rows=[]
  for ident in ls.stdout.split():
   label='.Config.Labels' if kind=='containers' else '.Labels'
   fmt='{"id":{{json .Id}},"name":{{json .Name}},"labels":{{json '+label+'}}}'
   got=docker((['inspect'] if kind=='containers' else ['network','inspect'])+['--format',fmt,ident])
   if got.returncode==0:rows.append(json.loads(got.stdout))
  output[kind]=sorted(rows,key=lambda x:x['id'])
 return output
baseline=resources();write('baseline.json',baseline)
seen={k:{} for k in baseline}
go_processes={};watch_errors=[]
start=time.time()
with (r/'fixture.log').open('wb') as log:
 p=subprocess.Popen(argv,cwd=tree,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 write('running.json',{'driver_pid':os.getpid(),'shell_pid':p.pid,'process_group':p.pid,'started_utc':stamp()})
 print(json.dumps({'stage':'fixture driver launched','driver_pid':os.getpid(),'shell_pid':p.pid,'baseline_counts':{k:len(v) for k,v in baseline.items()},'log':str(r/'fixture.log')}),flush=True)
 while p.poll() is None:
  try:
   now=resources()
   for kind in seen:
    oldids={v['id'] for v in baseline[kind]}
    for row in now[kind]:
     if row['id'] not in oldids:seen[kind][row['id']]=row
   write('observed-owned-resources.json',{k:sorted(v.values(),key=lambda x:x['id']) for k,v in seen.items()})
   for d in pathlib.Path('/proc').iterdir():
    if not d.name.isdigit():continue
    try:
     if os.getpgid(int(d.name))!=p.pid:continue
     parts=(d/'cmdline').read_bytes().split(b'\0')
     if not parts or parts[0].decode(errors='replace')!=tool:continue
     ge={}
     for line in (d/'environ').read_bytes().split(b'\0'):
      key,sep,value=line.partition(b'=')
      if key in [b'GOFLAGS',b'GOTOOLCHAIN',b'GOWORK',b'GOENV']:
       ge[key.decode()]=value.decode(errors='replace')
     go_processes[d.name]={'argv':[v.decode(errors='replace') for v in parts if v],'environment':ge}
    except (FileNotFoundError,ProcessLookupError,PermissionError):pass
   write('actual-go-processes.json',go_processes)
  except Exception as ex:watch_errors.append(str(ex))
  time.sleep(.25)
 code=p.wait()
write('exit.json',{'exit_code':code,'seconds':round(time.time()-start,6),'ended_utc':stamp(),'log_sha256':sha(r/'fixture.log'),'watch_errors':watch_errors})
def cleanup_sample():
 now=resources();exact={}
 for kind in seen:
  exact[kind]=[]
  for ident,row in sorted(seen[kind].items()):
   got=docker((['inspect'] if kind=='containers' else ['network','inspect'])+['--format','{{.Id}}',ident])
   exact[kind].append({'id':ident,'name':row['name'],'labels':row['labels'],'absent':got.returncode!=0 and ('No such' in got.stderr or 'not found' in got.stderr),'returncode':got.returncode,'stderr':got.stderr.strip()})
 owned_pids=[]
 for d in pathlib.Path('/proc').iterdir():
  if not d.name.isdigit():continue
  try:
   if os.getpgid(int(d.name))==p.pid:owned_pids.append(int(d.name))
  except (ProcessLookupError,PermissionError):pass
 return {'observed_utc':stamp(),'baseline_equal':now==baseline,'resources':now,'exact_owned':exact,'owned_process_group':p.pid,'owned_pids':owned_pids,'runtime_entries':[str(x.relative_to(r/'runtime')) for x in (r/'runtime').rglob('*')],'source_after':identities(),'source_matches':identities()==candidate['files'],'probe_sha256':sha(probe),'overlay_sha256':sha(overlay)}
first=cleanup_sample();write('cleanup-01.json',first)
time.sleep(1)
second=cleanup_sample();write('cleanup-02.json',second)
summary={'exit_code':code,'seconds':round(time.time()-start,6),'owned_captured':{k:len(v) for k,v in seen.items()},'baseline_unchanged_twice':first['baseline_equal'] and second['baseline_equal'],'all_owned_exact_absent_twice':all(v['absent'] for x in [first,second] for values in x['exact_owned'].values() for v in values),'owned_processes_zero_twice':not first['owned_pids'] and not second['owned_pids'],'runtime_empty_twice':not first['runtime_entries'] and not second['runtime_entries'],'source_matches_twice':first['source_matches'] and second['source_matches'],'actual_go_calls_captured':len(go_processes),'log_sha256':sha(r/'fixture.log'),'watch_errors':watch_errors}
write('summary.json',summary);print(json.dumps(summary,indent=2),flush=True)
raise SystemExit(code)
