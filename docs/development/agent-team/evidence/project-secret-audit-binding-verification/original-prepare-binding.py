import json,os,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parent
env=os.environ.copy()
env.update({'AGENTEAM_GO':'/workspace/toolchains/go1.27.1/bin/go','GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':'/tmp/agenteam-d08-b02-baseline-2h_8imsq/cache','GOTMPDIR':str(root/'runtime'),'TMPDIR':str(root/'runtime'),'GOFLAGS':'-mod=readonly -overlay='+str(root/'binding-overlay.json')+' -v'})
go=env['AGENTEAM_GO']
commands=[('binding-map-race',[go,'test','-race','-count=1','-run','^TestProjectBindingIndependentCallerMapCannotChangeDispatch$','./internal/central/project']),('binding-integration-compile',[go,'test','-tags=integration','-race','-run','^$','./tests/project']),('binding-probe-vet',[go,'vet','-tags=integration','./internal/central/project','./tests/project'])]
records=[]
for name,cmd in commands:
    start=time.monotonic()
    with (root/'evidence'/(name+'.log')).open('w') as out:r=subprocess.run(cmd,cwd=root/'snapshot',env=env,stdout=out,stderr=subprocess.STDOUT)
    records.append({'name':name,'command':cmd,'cwd':str(root/'snapshot'),'exit':r.returncode,'seconds':time.monotonic()-start,'GOFLAGS':env['GOFLAGS']})
    (root/'evidence/binding-pure-checks.json').write_text(json.dumps(records,indent=2)+'\n')
    print(name+': exit='+str(r.returncode),flush=True)
    if r.returncode:raise SystemExit(r.returncode)
