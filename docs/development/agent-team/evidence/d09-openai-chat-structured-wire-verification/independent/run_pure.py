import os,json,subprocess,time,datetime,hashlib
from pathlib import Path
r=Path(__file__).resolve().parent
env=os.environ.copy()
fixed={"GOENV":"off","GOWORK":"off","GOTOOLCHAIN":"local","GOPROXY":"off","GOSUMDB":"off","GOMODCACHE":"/workspace/agenteam-dependency-cache/modcache","GOCACHE":str(r/"private-cache"),"TMPDIR":str(r/"runtime"),"GOFLAGS":"-mod=readonly","CGO_ENABLED":"1","GOTELEMETRY":"off"}
env.update(fixed)
go="/workspace/toolchains/go1.27.1/bin/go"
commands=[("compile",[go,"test","-c","-tags=integration","-race","-o",str(r/"build/model.test"),"./tests/model"]),("vet",[go,"vet","-tags=integration","./tests/model"])]
for name,argv in commands:
 entry={"argv":argv,"cwd":str(r/"tree"),"environment":fixed,"kind":"offline compilation or vet only; no test binary execution","started_at":datetime.datetime.now(datetime.timezone.utc).isoformat()}
 (r/"evidence"/(name+"-01.json")).write_text(json.dumps(entry,indent=2)+"\n")
 print(name+" started",flush=True)
 with (r/"evidence"/(name+"-01.log")).open("wb") as f:
  run=subprocess.run(argv,cwd=r/"tree",env=env,stdout=f,stderr=subprocess.STDOUT)
 entry.update(exit_code=run.returncode,finished_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),log_sha256=hashlib.sha256((r/"evidence"/(name+"-01.log")).read_bytes()).hexdigest())
 (r/"evidence"/(name+"-01.json")).write_text(json.dumps(entry,indent=2)+"\n")
 print(name+" exit="+str(run.returncode),flush=True)
 if run.returncode:raise SystemExit(run.returncode)
