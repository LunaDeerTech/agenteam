import pathlib,json,hashlib,base64,zipfile,os,stat
r=pathlib.Path('/workspace/scratch/go-dependency-recovery');cache=pathlib.Path('/workspace/go/pkg/mod');stage=r/'staging-modcache';project=pathlib.Path('/workspace/agenteam')
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
sums={tuple(line.split()[:2]):line.split()[2] for line in (project/'go.sum').read_text().splitlines()}
raw=(r/'evidence/download-required.raw').read_text();dec=json.JSONDecoder();rows=[]
while raw.strip():
 row,n=dec.raw_decode(raw.lstrip());rows.append(row);raw=raw.lstrip()[n:]
assert {m['Path']+'@'+m['Version'] for m in rows}==set(json.loads((r/'missing-arguments.json').read_text()))
checks=[]
for row in rows:
 assert 'Error' not in row
 for field,suffix in [('Sum',''),('GoModSum','/go.mod')]: assert row[field]==sums[(row['Path'],row['Version']+suffix)]
 z=stage/pathlib.Path(row['Zip']).relative_to(cache);mod=stage/pathlib.Path(row['GoMod']).relative_to(cache)
 with zipfile.ZipFile(z) as archive:
  h=hashlib.sha256()
  for name in sorted(archive.namelist()):h.update((hashlib.sha256(archive.read(name)).hexdigest()+'  '+name+'\n').encode())
 assert 'h1:'+base64.b64encode(h.digest()).decode()==row['Sum']
 modsum='h1:'+base64.b64encode(hashlib.sha256((sha(mod)+'  go.mod\n').encode()).digest()).decode()
 assert modsum==row['GoModSum']
 checks.append({'Path':row['Path'],'Version':row['Version'],'Sum':row['Sum'],'GoModSum':row['GoModSum'],'zip_sha256':sha(z),'gomod_sha256':sha(mod)})
(r/'evidence/new-module-verification.json').write_text(json.dumps(checks,indent=2)+'\n')
created=[];existing={};skipped=[];created_bytes=0;treehash=hashlib.sha256()
for src in sorted(stage.rglob('*')):
 if not src.is_file():continue
 assert not src.is_symlink()
 rel=src.relative_to(stage)
 if str(rel).startswith('cache/download/sumdb/') or src.name.endswith('.lock'):continue
 dst=cache/rel
 if dst.exists():
  assert dst.is_file() and not dst.is_symlink()
  old=sha(dst);existing[str(rel)]=old
  if src.name=='list': skipped.append(str(rel));continue
  assert old==sha(src),('preexisting content differs',str(rel))
  continue
 dst.parent.mkdir(parents=True,exist_ok=True)
 data=src.read_bytes()
 try:fd=os.open(dst,os.O_WRONLY|os.O_CREAT|os.O_EXCL,stat.S_IMODE(src.stat().st_mode))
 except FileExistsError:raise RuntimeError('concurrent cache writer at '+str(rel))
 with os.fdopen(fd,'wb') as out:out.write(data)
 assert sha(dst)==hashlib.sha256(data).hexdigest()
 created.append(str(rel));created_bytes+=len(data);treehash.update((hashlib.sha256(data).hexdigest()+'  '+str(rel)+'\n').encode())
assert all(sha(cache/f)==h for f,h in existing.items())
inputs=json.loads((r/'evidence/input-and-missing.json').read_text())
assert sha(project/'go.mod')==inputs['go.mod'] and sha(project/'go.sum')==inputs['go.sum']
for m in inputs['modules']:
 assert all(sha(cache/f)==h for f,h in m['existing_files'].items())
(r/'evidence/created-cache-paths.txt').write_text('\n'.join(created)+'\n')
result={'downloaded_fixed_modules':len(rows),'created_files':len(created),'created_bytes':created_bytes,'created_path_content_sha256':treehash.hexdigest(),'preexisting_files_unchanged':existing,'preexisting_version_lists_preserved':skipped,'project_mod_sum_unchanged':True}
(r/'evidence/cache-installation.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({k:v for k,v in result.items() if k not in ['preexisting_files_unchanged','preexisting_version_lists_preserved']},indent=2))
