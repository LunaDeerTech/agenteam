import os,pathlib,subprocess,sys,time,json,hashlib
r=pathlib.Path('/tmp/agenteam-artifact-stop-author-y597q6vl'); label,pattern=sys.argv[1:]; log=r/'logs'/f'{label}.log'
e=os.environ.copy(); e.pop('DOCKER_CONTEXT',None); e.update(DOCKER_HOST='unix:///var/run/docker.sock', DOCKER_CONFIG=str(r/'docker-config'), TMPDIR=str(pathlib.Path('/workspace/agenteam-artifact-stop-build-y597q6vl/runtime')), AGENTEAM_GO='/workspace/toolchains/go1.27.1/bin/go', AGENTEAM_MINIO_BINARY='/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio', GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOPROXY='off', GOFLAGS='-mod=readonly', GOMODCACHE='/workspace/agenteam-dependency-cache/modcache', GOCACHE=str(pathlib.Path('/workspace/agenteam-artifact-stop-build-y597q6vl/go-build')))
files=(r/'authorized-paths.txt').read_text().splitlines()
cmd=['sh','scripts/test-objects.sh','-run',pattern]
(r/f'input-{label}.json').write_text(json.dumps({'baseline':'6658a6cb1f29299521773bc8dc86b2f607b8c809','files':{f:hashlib.sha256((r/'snapshot'/f).read_bytes()).hexdigest() for f in files},'command':cmd,'cwd':str(r/'snapshot'),'environment':{k:e[k] for k in ['DOCKER_HOST','DOCKER_CONFIG','TMPDIR','AGENTEAM_GO','AGENTEAM_MINIO_BINARY','GOTOOLCHAIN','GOENV','GOWORK','GOPROXY','GOFLAGS','GOMODCACHE','GOCACHE']}},indent=2)+'\n')
commands={'containers':['ps','-a','--no-trunc','--format','{{.ID}} {{.Names}} {{.Labels}}'],'networks':['network','ls','--no-trunc','--format','{{.ID}} {{.Name}} {{.Labels}}']}
def resources():
 return {k:subprocess.check_output(['docker',*c],env=e,text=True) for k,c in commands.items()}
before=resources(); seen={k:{} for k in commands}
for k,v in before.items(): (r/f'{label}-before-{k}.txt').write_text(v)
started=time.monotonic()
with log.open('wb') as f:
 p=subprocess.Popen(cmd,cwd=r/'snapshot',env=e,stdout=f,stderr=subprocess.STDOUT)
 while p.poll() is None:
  for k,v in resources().items():
   for line in v.splitlines(): seen[k][line.split()[0]]=line
  time.sleep(1)
 exitcode=p.wait()
(r/'logs'/f'{label}.exit').write_text(str(exitcode)+'\n')
after=resources(); result={'exit':exitcode,'seconds':round(time.monotonic()-started,3)}
def norm(v):
 out={}
 for line in v.splitlines():
  a=line.split(' ',2);out[a[0]]=[a[1],sorted(a[2].split(',')) if len(a)>2 and a[2] else []]
 return out
for k,v in after.items():
 (r/f'{label}-after-{k}.txt').write_text(v); (r/f'{label}-live-{k}.txt').write_text('\n'.join(seen[k].values())+'\n'); orig=norm(before[k]); owned=set(seen[k])-set(orig); checks={}
 for id in sorted(owned):
  c=subprocess.run(['docker','container' if k=='containers' else 'network','inspect','--format','{{.Id}}',id],env=e,text=True,capture_output=True)
  checks[id]={'exit':c.returncode,'output':(c.stdout+c.stderr).strip()}
 result[k]={'baseline_ids_names_labels_equal':norm(v)==orig,'owned':checks}
result['runtime_entries']=[str(p.relative_to(pathlib.Path('/workspace/agenteam-artifact-stop-build-y597q6vl/runtime'))) for p in (pathlib.Path('/workspace/agenteam-artifact-stop-build-y597q6vl/runtime')).rglob('*')]; procs=[]
for x in pathlib.Path('/proc').iterdir():
 if not x.name.isdigit():continue
 try:exe=str((x/'exe').readlink())
 except (FileNotFoundError,PermissionError,ProcessLookupError):continue
 if 'objects.test' in exe or str(pathlib.Path('/workspace/agenteam-artifact-stop-build-y597q6vl/runtime')) in exe or '/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio' in exe:procs.append({'pid':x.name,'exe':exe})
result['owned_processes']=procs
(r/f'cleanup-{label}.json').write_text(json.dumps(result,indent=2)+'\n'); print(json.dumps(result,indent=2),flush=True); sys.exit(exitcode)
