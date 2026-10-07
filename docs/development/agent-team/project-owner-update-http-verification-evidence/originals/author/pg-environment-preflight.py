from pathlib import Path
import os,json,hashlib,shutil,subprocess,time
b=Path('/workspace/scratch/project-owner-update-http-author/pg-driver-v01');out=b/'environment-preflight01';out.mkdir(mode=0o700);cfg=b/'docker-config';cfg.mkdir(mode=0o700,exist_ok=True);assert not list(cfg.iterdir())
images=['pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc','pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782'];env={'PATH':'/usr/local/bin:/usr/bin:/bin','HOME':'/home/agent','DOCKER_CONFIG':str(cfg),'DOCKER_HOST':'unix:///var/run/docker.sock'}
def dock(args):
 p=subprocess.run(['/usr/local/bin/docker',*args],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10,check=False);assert p.returncode==0,('local Docker preflight failed',args,p.returncode);return p.stdout.decode()
procs=[]
for p in Path('/proc').iterdir():
 if not p.name.isdecimal():continue
 try:
  raw=(p/'stat').read_text();f=raw.rsplit(') ',1)[1].split();procs.append({'pid':int(p.name),'starttime':f[19],'ppid':int(f[1]),'pgid':int(f[2]),'state':f[0]})
 except OSError:pass
(out/'process-baseline.json').write_text(json.dumps(procs,indent=2)+'\n')
free=shutil.disk_usage(b).free;assert free>=5*(1<<30),free
rows=[]
for im in images:
 j=json.loads(dock(['image','inspect','--format','{"id":{{json .Id}},"repo_digests":{{json .RepoDigests}}}',im]));assert im in j['repo_digests'];rows.append({'expected':im,**j})
base={'containers':dock(['ps','-aq']).split(),'networks':dock(['network','ls','-q']).split()};(out/'docker-baseline.json').write_text(json.dumps(base,indent=2)+'\n')
sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest();r={'pass':True,'time':time.time(),'free_bytes':free,'minimum':5*(1<<30),'images':rows,'driver_sha256':sha(b/'driver.py'),'frozen_sha256':sha(b/'frozen.json'),'no_image_pull':True,'empty_docker_config':not list(cfg.iterdir()),'process_baseline_sha256':sha(out/'process-baseline.json'),'docker_baseline_sha256':sha(out/'docker-baseline.json')};(out/'result.json').write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r))
