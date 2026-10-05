import os,pathlib,json,subprocess,time
p=pathlib.Path('/tmp/agenteam-object-audit-verifier-frxgtjbx')
env=os.environ.copy()
chosen={'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOMODCACHE':'/workspace/agenteam-dependency-cache/modcache','GOCACHE':str(p/'gocache')}
env.update(chosen)
cmd=['/workspace/toolchains/go1.27.1/bin/go','test','-c','-race','-tags=integration','-o',str(p/'objects-independent.test'),'./tests/objects']
start=time.time()
with (p/'logs/compile.log').open('w') as log:
 log.write(json.dumps({'command':cmd,'cwd':str(p/'tree'),'env':chosen})+'\n');log.flush()
 r=subprocess.run(cmd,cwd=p/'tree',env=env,stdout=log,stderr=subprocess.STDOUT)
result={'name':'independent-integration-compile','command':cmd,'cwd':str(p/'tree'),'env':chosen,'exit':r.returncode,'seconds':round(time.time()-start,3)}
(p/'evidence/compile-result.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result))
raise SystemExit(r.returncode)
