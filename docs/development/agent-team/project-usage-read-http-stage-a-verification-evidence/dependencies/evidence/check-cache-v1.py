import pathlib,json,hashlib,base64,zipfile,re
r=pathlib.Path('/workspace/scratch/go-dependency-recovery');cache=pathlib.Path('/workspace/go/pkg/mod');project=pathlib.Path('/workspace/agenteam');sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
inputs=json.loads((r/'evidence/input-and-missing.json').read_text());sums={tuple(line.split()[:2]):line.split()[2] for line in (project/'go.sum').read_text().splitlines()};checks=[]
for row in inputs['modules']:
 path=row['Path'];v=row['Version'];esc=lambda s:re.sub('[A-Z]',lambda m:'!'+m[0].lower(),s);base=cache/'cache/download'/esc(path)/'@v';z=base/(esc(v)+'.zip');mod=base/(esc(v)+'.mod');source=cache/(esc(path)+'@'+esc(v));prefix=path+'@'+v+'/'
 with zipfile.ZipFile(z) as archive:
  h=hashlib.sha256()
  for name in sorted(archive.namelist()):h.update((hashlib.sha256(archive.read(name)).hexdigest()+'  '+name+'\n').encode())
 zip_sum='h1:'+base64.b64encode(h.digest()).decode();assert zip_sum==sums[(path,v)]
 mod_sum='h1:'+base64.b64encode(hashlib.sha256((sha(mod)+'  go.mod\n').encode()).digest()).decode();assert mod_sum==sums[(path,v+'/go.mod')]
 h=hashlib.sha256();files=sorted(p for p in source.rglob('*') if p.is_file())
 for p in files:
  assert not p.is_symlink();h.update((sha(p)+'  '+prefix+str(p.relative_to(source))+'\n').encode())
 source_sum='h1:'+base64.b64encode(h.digest()).decode();assert source_sum==zip_sum,('source content mismatch',path,v,source_sum,zip_sum)
 checks.append({'Path':path,'Version':v,'module_sum':zip_sum,'go_mod_sum':mod_sum,'extracted_source_sum':source_sum,'source_files':len(files)})
assert sha(project/'go.mod')==inputs['go.mod'] and sha(project/'go.sum')==inputs['go.sum']
installation=json.loads((r/'evidence/cache-installation.json').read_text());assert all(sha(cache/p)==s for p,s in installation['preexisting_files_unchanged'].items())
for m in inputs['modules']:assert all(sha(cache/p)==s for p,s in m['existing_files'].items())
before=json.loads((r/'evidence/availability-input-before.json').read_text());after={p:sha(project/p) for p in before['sha256']};changed=[p for p in after if before['sha256'][p]!=after[p]]
(r/'evidence/availability-input-after.json').write_text(json.dumps({'sha256':after,'changed_paths':changed},indent=2)+'\n');(r/'evidence/all-required-module-verification.json').write_text(json.dumps(checks,indent=2)+'\n');print(json.dumps({'required_modules_verified':len(checks),'all_zip_and_source_sums_match_go_sum':True,'prior_cache_bytes_preserved':True,'project_mod_sum_unchanged':True,'availability_input_changes':changed},indent=2))
