import os,sys,json,subprocess,time,pathlib,hashlib
ROOT=pathlib.Path('/workspace/scratch/fixture-recovery')
name=sys.argv[1]; args=sys.argv[2:]
env={'PATH':'/workspace/toolchains/go1.27.1/bin:/usr/local/bin:/usr/bin:/bin','LANG':'C.UTF-8','TMPDIR':str(ROOT/'tmp'),'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'https://proxy.golang.org','GOSUMDB':'sum.golang.org','GOMODCACHE':'/workspace/go/pkg/mod','GOCACHE':str(ROOT/'gocache'),'GOOS':'linux','GOARCH':'amd64','GOAMD64':'v1','CGO_ENABLED':'0','GOTELEMETRY':'off','GOFLAGS':''}
for k in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY','http_proxy','https_proxy','all_proxy','no_proxy'):
 if k in os.environ: env[k]=os.environ[k]
meta={'argv':args,'cwd':os.getcwd(),'env':{k: ('<inherited-present>' if 'PROXY' in k.upper() and k not in ('GOPROXY',) else v) for k,v in env.items()},'started_ns':time.time_ns()}
path=ROOT/'evidence'/name
path.with_suffix('.meta.json').write_text(json.dumps(meta,indent=2)+'\n')
with path.with_suffix('.raw').open('wb') as f:
 p=subprocess.Popen(args,stdout=f,stderr=subprocess.STDOUT,env=env)
 meta['pid']=p.pid; meta['exit_code']=p.wait()
meta['ended_ns']=time.time_ns(); meta['elapsed_seconds']=(meta['ended_ns']-meta['started_ns'])/1e9
meta['raw_sha256']=hashlib.sha256(path.with_suffix('.raw').read_bytes()).hexdigest()
path.with_suffix('.meta.json').write_text(json.dumps(meta,indent=2)+'\n')
print(json.dumps(meta,ensure_ascii=False))
print(path.with_suffix('.raw').read_text(errors='replace')[-6000:])
sys.exit(meta['exit_code'])
