import pathlib,json,hashlib,os
r=pathlib.Path('/workspace/scratch/fixture-recovery'); e=r/'evidence'; sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
metas={p.name:json.loads(p.read_text()) for p in sorted(e.glob('*.meta.json'))}
assert all('exit_code' in m for m in metas.values()),'unfinished recorded command'
binary=r/'bin/minio'; actual=sha(binary)
assert all(not pathlib.Path('/proc',str(m['pid'])).exists() for m in metas.values()),'a recorded command PID remains'
assert not pathlib.Path('/workspace/go/pkg/mod/github.com/philhofer/fwd@v1.2.0').exists()
assert not pathlib.Path('/workspace/go/pkg/mod/github.com/tinylib/msgp@v1.4.0').exists()
expected='dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'
assert actual==expected,('binary SHA mismatch',actual)
pg={}
for name,digest in [('pg17','99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc'),('pg16','16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782')]:
 row=json.loads((e/(name+'-inspect.raw')).read_text()); ref='pgvector/pgvector@sha256:'+digest
 assert ref in row['RepoDigests'] and row['Architecture']=='amd64' and row['Os']=='linux'
 assert metas[name+'-pull.meta.json']['exit_code']==0 and metas[name+'-inspect.meta.json']['exit_code']==0
 pg[name]={'reference':ref,'id':row['Id'],'architecture':row['Architecture'],'os':row['Os'],'version_config':[v for v in row['Config']['Env'] if v.startswith('PG_VERSION=')]}
source=r/'src/github.com/minio/minio@v0.0.0-20251015172955-9e49d5e7a648'
source_verification=json.loads((e/'source-verification.json').read_text())
assert all(sha(source/f)==s for f,s in source_verification['files'].items())
inputs=json.loads((e/'input-manifest.json').read_text()); final_inputs={f:sha(pathlib.Path('/workspace/agenteam')/f) for f in inputs['files']}
summary={'baseline':inputs['main'],'binary':{'path':str(binary),'sha256':actual,'bytes':binary.stat().st_size,'injection':'AGENTEAM_MINIO_BINARY='+str(binary)},'postgres':pg,'final_input_hashes':final_inputs,'changed_input_paths':[f for f in inputs['files'] if final_inputs[f]!=inputs['files'][f]],'commands':{k:{x:v[x] for x in ['exit_code','elapsed_seconds','raw_sha256']} for k,v in metas.items()},'created_runtime_resources':[],'limitations':['Only dependency recovery: no containers, services, listeners, database connections or product tests started.','PG_VERSION is inspected image configuration, not a running PostgreSQL observation.','Existing module cache was mounted read-only; the two missing fixed dependencies were downloaded only to task-owned missing-modcache.','Historical build/download infrastructure failures are preserved separately; no SHA or fixture helper contract was changed.']}
assert 'RELEASE.2025-10-15T17-29-55Z (commit-id=9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a)' in (e/'minio-version.raw').read_text()
assert metas['minio-version.meta.json']['exit_code']==0
assert metas['minio-build-04.meta.json']['exit_code']==0
(e/'recovery-result.json').write_text(json.dumps(summary,indent=2,ensure_ascii=False)+'\n')
paths=[r/'run.py',r/'verify-source.py',r/'finalize.py',r/'download/minio.zip',binary,*sorted(e.iterdir())]
manifest={str(p.relative_to(r)):sha(p) for p in paths if p.is_file() and p.name not in ['output-manifest.json','output-manifest.sha256']}
(e/'output-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n'); digest=sha(e/'output-manifest.json'); (e/'output-manifest.sha256').write_text(digest+'  output-manifest.json\n')
print(json.dumps({'binary_sha256':actual,'binary_bytes':binary.stat().st_size,'output_manifest_sha256':digest,'input_changes':summary['changed_input_paths'],'commands':len(metas)},indent=2))
