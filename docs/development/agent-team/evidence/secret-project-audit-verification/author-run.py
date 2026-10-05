import datetime,json,os,pathlib,subprocess,sys,time
p=pathlib.Path(__file__).parent
name=sys.argv[1]; cmd=sys.argv[2:]
env=dict(os.environ,GOTOOLCHAIN="local",GOENV="off",GOWORK="off",GOMODCACHE="/workspace/agenteam-dependency-cache/modcache",GOCACHE=str(p/"gocache"),GOTMPDIR=str(p/"tmp"),GOPROXY="off",GOFLAGS="-mod=readonly")
log=p/"logs"/(name+".log"); started=datetime.datetime.now(datetime.timezone.utc).isoformat(); t=time.monotonic()
with log.open("w") as f:
 f.write(json.dumps({"command":cmd,"cwd":str(p/"snapshot"),"started":started})+"\n"); f.flush()
 r=subprocess.run(cmd,cwd=p/"snapshot",env=env,stdout=f,stderr=subprocess.STDOUT)
out={"name":name,"command":cmd,"started":started,"seconds":round(time.monotonic()-t,3),"exit":r.returncode,"log":str(log)}
with (p/"results.jsonl").open("a") as f:f.write(json.dumps(out)+"\n")
print(json.dumps(out));print(log.read_text()[-4500:]);sys.exit(r.returncode)
