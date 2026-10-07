from pathlib import Path
import hashlib,json,os,shutil,subprocess,sys,time

root=Path(__file__).parent
label=sys.argv[1]
out=root/'image-gates'/label
out.mkdir(parents=True,exist_ok=False)
config=root/'pg-driver-v03/docker-config'
config.mkdir(mode=0o700,exist_ok=True)
assert not list(config.iterdir())
docker='/usr/local/bin/docker'
assert hashlib.sha256(Path(docker).read_bytes()).hexdigest()=='27f239f97492c434e091a70b41b8796c698f0aa2b6052c6f9c28da4ee7ae888b'
free=shutil.disk_usage(root).free
assert free>=5*(1<<30)
env={'PATH':'/usr/local/bin:/usr/bin:/bin','DOCKER_CONFIG':str(config)}
refs=['pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc','pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782']
results=[]
for i,ref in enumerate(refs):
    argv=[docker,'image','inspect','--format','{"id":{{json .Id}},"repo_digests":{{json .RepoDigests}},"os":{{json .Os}},"architecture":{{json .Architecture}}}',ref]
    start=time.monotonic()
    child=subprocess.Popen(argv,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    stdout,stderr=child.communicate()
    (out/f'image-{i}.raw').write_bytes(stdout)
    (out/f'image-{i}.stderr').write_bytes(stderr)
    row={'argv':argv,'pid':child.pid,'actual_exit':child.returncode,'actual_wait':True,'seconds':time.monotonic()-start,'stdout_sha256':hashlib.sha256(stdout).hexdigest(),'stderr_sha256':hashlib.sha256(stderr).hexdigest()}
    (out/f'image-{i}.command.json').write_text(json.dumps(row,indent=2)+'\n')
    assert child.returncode==0
    image=json.loads(stdout)
    assert ref in image['repo_digests'] and image['os']=='linux' and image['architecture']=='amd64'
    results.append({'reference':ref,**image})
result={'passed':True,'time_unix':time.time(),'free_bytes':free,'minimum_bytes':5*(1<<30),'empty_config':str(config),'images':results,'resources_started':False,'scope':'local inspect only; original fixture driver independently takes fresh PID/starttime/resource baseline at actual launch'}
(out/'result.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result))
