import pathlib,re,json,hashlib,subprocess
r=pathlib.Path('/workspace/scratch/go-dependency-recovery');src=pathlib.Path('/workspace/agenteam');cache=pathlib.Path('/workspace/go/pkg/mod')
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
mods=[]
for path,version in re.findall(r'^\s*([^\s]+)\s+(v[^\s]+)(?:\s+// indirect)?\s*$',(src/'go.mod').read_text(),re.M):
 esc=lambda s:re.sub('[A-Z]',lambda m:'!'+m[0].lower(),s)
 base=cache/'cache/download'/esc(path)/'@v';files=[base/(esc(version)+suffix) for suffix in ('.mod','.info','.zip','.ziphash')]
 missing=[str(p.relative_to(cache)) for p in files if not p.exists()]
 source=cache/(esc(path)+'@'+esc(version));exists=source.is_dir()
 mods.append({'Path':path,'Version':version,'source_present':exists,'missing':missing,'existing_files':{str(p.relative_to(cache)):sha(p) for p in files if p.is_file()}})
manifest={'main':subprocess.check_output(['git','rev-parse','HEAD'],cwd=src,text=True).strip(),'go.mod':sha(src/'go.mod'),'go.sum':sha(src/'go.sum'),'modules':mods,'origin_failure':{'command.json':sha(pathlib.Path('/workspace/scratch/usage-http-author/a01-list/command.json')),'raw.log':sha(pathlib.Path('/workspace/scratch/usage-http-author/a01-list/raw.log'))}}
(r/'evidence/input-and-missing.json').write_text(json.dumps(manifest,indent=2)+'\n')
missing=[m['Path']+'@'+m['Version'] for m in mods if m['missing'] or not m['source_present']]
(r/'missing-arguments.json').write_text(json.dumps(missing,indent=2)+'\n');print(json.dumps({'required_modules':len(mods),'missing_modules':len(missing),'missing':missing},indent=2))
