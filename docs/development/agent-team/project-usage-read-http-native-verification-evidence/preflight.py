import pathlib,json,hashlib,subprocess,os,shutil,time,re
base=pathlib.Path('/workspace/scratch/usage-http-verification/native01');repo=pathlib.Path('/workspace/agenteam');author=pathlib.Path('/workspace/scratch/usage-http-author/B');binary=author/'usage-http.test';commit='e7304512e73fdddb25bdbf1c0882400ad5b5f791'
final=json.loads((author/'final-input.json').read_text());sources=final['product_graph_paths'];excluded={'internal/central/app/account.go','internal/central/app/project_usage.go','internal/central/app/project_usage_test.go','tests/model/project_usage_http_fixture_test.go','tests/model/project_usage_http_test.go','tests/model/project_usage_http_terminal_test.go','tests/model/project_usage_http_root_test.go'}
readpaths=[]
for p in sources:
 path=pathlib.Path(p);rel=str(path.relative_to(repo));assert rel not in excluded
 raw=path.read_bytes();fixed=subprocess.check_output(['git','show',commit+':'+rel],cwd=repo);assert raw==fixed,rel;readpaths.append(rel)
assert not any(p.startswith('internal/central/app/') or p.startswith('tests/model/') for p in readpaths)
expected=final['artifacts'][str(binary)];assert hashlib.sha256(binary.read_bytes()).hexdigest()==expected['sha256'] and binary.stat().st_size==expected['bytes']
mainpath=pathlib.Path('/home/agent/.cache/go-build/20/20db1c7fcbf22be8985ae3926eda286736d8d250b17c9ff21d89d6e88903ebb5-d');raw=mainpath.read_bytes();assert hashlib.sha256(raw).hexdigest()=='20db1c7fcbf22be8985ae3926eda286736d8d250b17c9ff21d89d6e88903ebb5';main=raw.decode();assert 'os.Exit(m.Run())' in main and '_test.TestMain(' not in main
wanted=['TestProjectUsageHTTPNativeSlowBody','TestProjectUsageHTTPNativeWriteAndClose','TestProjectUsageHTTPNativeKeepAlive'];assert all('"'+n+'"' in main for n in wanted)
handler=subprocess.check_output(['git','show',commit+':internal/central/usage/http/handler_test.go'],cwd=repo).decode();assert 'net.Listen("tcp", "127.0.0.1:0")' in handler;assert handler.count('requireUsageNative(t)')==3
(build:=subprocess.run(['/workspace/toolchains/go1.27.1/bin/go','version','-m',str(binary)],capture_output=True,text=True,timeout=15)).check_returncode();assert '-race=true' in build.stdout;(base/'binary-build-info.raw').write_text(build.stdout)
for n in ['tcp','tcp6','unix']:(base/('baseline-'+n+'.raw')).write_text(pathlib.Path('/proc/net/'+n).read_text())
processes=[]
for p in pathlib.Path('/proc').iterdir():
 if not p.name.isdigit():continue
 try:
  s=(p/'stat').read_text();f=s.split(') ',1)[1].split();processes.append({'pid':int(p.name),'name':s.split('(',1)[1].rsplit(')',1)[0],'state':f[0],'ppid':int(f[1]),'pgrp':int(f[2]),'starttime':f[19]})
 except (FileNotFoundError,ProcessLookupError,PermissionError):pass
(base/'baseline-processes.json').write_text(json.dumps(processes,indent=2)+'\n')
free=shutil.disk_usage(base).free;tmpfree=shutil.disk_usage('/tmp').free;assert free>=2*1024**3 and tmpfree>=2*1024**3
result={'commit':commit,'summary_accepted':'e6cb70bdfc6ef7569d740f767f7dca3211a2ef7a','B_final_manifest_sha256':hashlib.sha256((author/'final-input.json').read_bytes()).hexdigest(),'binary':str(binary),'binary_sha256':expected['sha256'],'binary_bytes':expected['bytes'],'race_binary_confirmed':True,'product_graph_paths_fixed_git_match':len(readpaths),'active_C_paths_not_imported':True,'generated_testmain_sha256':hashlib.sha256(raw).hexdigest(),'testmain_no_custom_hook':True,'native_top_names':wanted,'listener':'127.0.0.1:0','no_resources_started':True,'space_bytes':{'workspace':free,'tmp':tmpfree},'strace':shutil.which('strace'),'ss':shutil.which('ss'),'preparation_at':time.time()}
(base/'preflight.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2))
