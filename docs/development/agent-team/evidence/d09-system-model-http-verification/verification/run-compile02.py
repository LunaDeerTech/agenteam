import datetime,hashlib,json,os,subprocess
from pathlib import Path
p=Path(__file__).resolve().parent
selected={'AGENTEAM_GO':'/workspace/toolchains/go1.27.1/bin/go','GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOFLAGS':'-mod=readonly','GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':str(p/'gocache'),'TMPDIR':str(p/'runtime')}
env=os.environ.copy();env.update(selected)
version=subprocess.check_output([selected['AGENTEAM_GO'],'version'],env=env,text=True).strip()
assert version.startswith('go version go1.27.1 '),version
args=[selected['AGENTEAM_GO'],'test','-race','-tags=integration','-c','-o',str(p/'probe/model.test'),'./tests/model']
manifest=json.loads((p/'evidence/fixture02/manifest.json').read_text())
for f in manifest['files']:
    assert hashlib.sha256((p/'tree'/f['path']).read_bytes()).hexdigest()==f['sha256'],f['path']
record={'argv':args,'cwd':str(p/'tree'),'environment':selected,'go_version':version,'candidate_manifest_sha256':hashlib.sha256((p/'evidence/fixture02/manifest.json').read_bytes()).hexdigest(),'probe_sha256':hashlib.sha256((p/'probe/independent_system_http_test.go').read_bytes()).hexdigest(),'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'purpose':'compile-only; no test execution or Docker'}
(p/'evidence/compile02-command.json').write_text(json.dumps(record,indent=2)+'\n')
with (p/'evidence/compile02.log').open('wb') as log:
    proc=subprocess.Popen(args,cwd=p/'tree',env=env,stdout=log,stderr=subprocess.STDOUT)
    record['pid']=proc.pid
    (p/'evidence/compile02-running.json').write_text(json.dumps(record,indent=2)+'\n')
    status=proc.wait()
record.update(exit_code=status,finished_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),log_sha256=hashlib.sha256((p/'evidence/compile02.log').read_bytes()).hexdigest())
(p/'evidence/compile02-result.json').write_text(json.dumps(record,indent=2)+'\n')
print(json.dumps(record,indent=2))
raise SystemExit(status)
