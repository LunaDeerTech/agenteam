import pathlib,hashlib,zipfile,json,base64
r=pathlib.Path('/workspace/scratch/fixture-recovery-new'); z=r/'download/minio.zip'
expected_zip='b137c35bf9708b4032a6a8301495a2563cab25111c28b80fd609812a3252a2f8'
assert hashlib.sha256(z.read_bytes()).hexdigest()==expected_zip,'source ZIP SHA mismatch'
prefix='github.com/minio/minio@v0.0.0-20251015172955-9e49d5e7a648/'
with zipfile.ZipFile(z) as archive:
 names=sorted(archive.namelist()); h=hashlib.sha256()
 for name in names:
  p=pathlib.PurePosixPath(name)
  assert name.startswith(prefix) and '..' not in p.parts and not p.is_absolute()
  h.update((hashlib.sha256(archive.read(name)).hexdigest()+'  '+name+'\n').encode())
 module_sum='h1:'+base64.b64encode(h.digest()).decode()
 assert module_sum=='h1:6TdolSCLSs2nwm8i0PpWDqf9iX2Ty9WQK8wmr7dCnUM=',module_sum
 archive.extractall(r/'src')
source=r/'src'/prefix
expected={'go.mod':'673f06144e90bc045f0a20050d2874c52e551be5bfbd70dff6e76b66da0db702','go.sum':'86e062349c7abdce0465561bb409d410a95b00b0969ba05d5fb5f2e3550a5cd4'}
for name,digest in expected.items():assert hashlib.sha256((source/name).read_bytes()).hexdigest()==digest,name
mod_sum='h1:'+base64.b64encode(hashlib.sha256((expected['go.mod']+'  go.mod\n').encode()).digest()).decode()
assert mod_sum=='h1:yCWDkwWO9IWpGsT4mreDDN/B/QVmK2zC666uInRAcqE=',mod_sum
result={'zip_sha256':expected_zip,'zip_bytes':z.stat().st_size,'module_sum':module_sum,'gomod_sum':mod_sum,'files':expected,'source':str(source)}
(r/'evidence/source-verification.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2))
