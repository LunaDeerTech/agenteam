#!/usr/bin/env python3
import datetime,hashlib,json,os,pathlib,subprocess,time
r=pathlib.Path(__file__).resolve().parent
old=pathlib.Path('/workspace/agenteam-artifact-foreign-v-4jgd5ed_')
e=r/'probe-compile';e.mkdir(exist_ok=True)
sha=lambda p:hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
c=json.loads((r/'evidence/candidate-manifest.json').read_text());d=json.loads((r/'evidence/delta.json').read_text())
assert sha(r/'evidence/candidate-manifest.json')=='79e5eb812c0fda2a42d390a7e2703b1ebca4ce581c055e1c2a99fda1ab76914e'
replace={str(old/'tree'/name):str(r/'overlay'/name) for name in d['files']}
for name,want in c['files'].items():
 p=r/'overlay'/name if name in d['files'] else old/'tree'/name
 assert sha(p)==want,name
probe=old/'tree/tests/objects/artifact_independent_foreign_result_test.go'
assert sha(probe)=='c40b8aeb6f574a2bbd7674f77b5ed5fb8c3311a1d367bc8302efaa2b199e5350'
(e/'overlay.json').write_text(json.dumps({'Replace':replace},indent=2)+'\n')
tool='/workspace/toolchains/go1.27.1/bin/go'
env={**os.environ,'AGENTEAM_GO':tool,'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOFLAGS':'-mod=readonly','GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':str(old/'gocache'),'TMPDIR':str(old/'tmp'),'GOTMPDIR':str(old/'tmp')}
argv=[tool,'test','-tags=integration','-race','-c','-overlay',str(e/'overlay.json'),'-o',str(e/'objects.test'),'./tests/objects']
command={'argv':argv,'cwd':str(old/'tree'),'environment':{k:env[k] for k in ['AGENTEAM_GO','GOTOOLCHAIN','GOENV','GOWORK','GOPROXY','GOSUMDB','GOFLAGS','GOMODCACHE','GOCACHE','TMPDIR','GOTMPDIR']},'candidate_manifest_sha256':sha(r/'evidence/candidate-manifest.json'),'probe_sha256':sha(probe),'selector':'^TestIndependentArtifactForeignJoinCommittedStoreUnknown$','overlay_sha256':sha(e/'overlay.json'),'operation':'compile only; no test/fixture execution'}
(e/'command.json').write_text(json.dumps(command,indent=2)+'\n')
start=time.time()
with (e/'compile.log').open('wb') as log:
 p=subprocess.Popen(argv,cwd=old/'tree',env=env,stdout=log,stderr=subprocess.STDOUT)
 (e/'running.json').write_text(json.dumps({'driver_pid':os.getpid(),'go_pid':p.pid,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2)+'\n')
 print(f'driver={os.getpid()} go={p.pid} log={e}/compile.log',flush=True)
 code=p.wait()
result={'exit_code':code,'elapsed_seconds':round(time.time()-start,6),'command_sha256':sha(e/'command.json'),'log_sha256':sha(e/'compile.log'),'candidate16_effective_matches':{name:sha(r/'overlay'/name if name in d['files'] else old/'tree'/name)==want for name,want in c['files'].items()},'probe_sha256':sha(probe),'go_pid':p.pid,'go_present':pathlib.Path('/proc',str(p.pid)).exists(),'tmp_entries':[x.name for x in (old/'tmp').iterdir()],'completed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
if code==0: result['binary_sha256']=sha(e/'objects.test')
(e/'result.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result,indent=2),flush=True)
raise SystemExit(code)
