from pathlib import Path
import json,hashlib,sys
base=Path('/workspace/scratch/usage-http-author');root=Path('/workspace/agenteam')
raw=(base/'a03-list/raw.log').read_text();decoder=json.JSONDecoder();packages=[];pos=0
while pos<len(raw):
 while pos<len(raw) and raw[pos].isspace():pos+=1
 if pos==len(raw):break
 p,pos=decoder.raw_decode(raw,pos);packages.append(p)
paths=set();missing=[]
for p in packages:
 if not p.get('Dir'):continue
 for key in ['GoFiles','CgoFiles','EmbedFiles','CFiles','CXXFiles','HFiles','SFiles','SysoFiles']:
  for name in p.get(key,[]):
   path=Path(p['Dir'])/name
   if path.is_file():paths.add(path)
   else:missing.append(str(path))
paths.update(root/p for p in ['go.mod','go.sum','api/openapi/common.json','api/openapi/project-usage.json','docs/development/work-items/d09-project-usage-read-http.md'])
paths.update([base/'run.py',base/'freeze-input.py',Path('/workspace/toolchains/go1.27.1/bin/go'),Path('/workspace/toolchains/go1.27.1/pkg/tool/linux_amd64/compile'),Path('/workspace/toolchains/go1.27.1/pkg/tool/linux_amd64/link'),Path('/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node'),Path(sys.executable)])
result={str(p):{'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'bytes':p.stat().st_size} for p in sorted(paths)}
output=base/'a03-execution-input.json';output.write_text(json.dumps({'packages':len(packages),'files':result,'generated_missing':missing},indent=2)+'\n')
print(len(packages),'packages',len(result),'files','missing generated',len(missing),'manifest sha256',hashlib.sha256(output.read_bytes()).hexdigest())
