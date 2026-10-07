import json,pathlib,hashlib,subprocess
base=pathlib.Path('/workspace/scratch/usage-http-verification/a02');repo=pathlib.Path('/workspace/agenteam');author=pathlib.Path('/workspace/scratch/usage-http-author/a03-execution-input.json')
d=json.loads(author.read_text());candidate=json.loads((base/'inputs.json').read_text());counts={'repository':0,'candidate':0,'fixed_dependency':0};fail=[]
for raw,expected in d['files'].items():
 p=pathlib.Path(raw)
 try:rel=str(p.relative_to(repo))
 except ValueError:continue
 counts['repository']+=1;actual=p.read_bytes();digest=hashlib.sha256(actual).hexdigest()
 if digest!=expected['sha256']:fail.append([rel,'author-list-input mismatch'])
 if rel in candidate:
  counts['candidate']+=1
  if digest!=candidate[rel]['sha256']:fail.append([rel,'a02 mismatch'])
 else:
  fixed=subprocess.check_output(['git','show','6fa2ee72:'+rel],cwd=repo);counts['fixed_dependency']+=1
  if actual!=fixed:fail.append([rel,'fixed main bytes mismatch'])
record={'source_author_manifest':str(author),'author_manifest_sha256':hashlib.sha256(author.read_bytes()).hexdigest(),'manifest_kind':'original nonrace Go package source closure; race-specific closure is separate author evidence','fixed_git':'6fa2ee721a75ea34a1ccd6523b8b25d68328c5b9','counts':counts,'failures':fail}
(base/'dependency-result.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps(record,indent=2));raise SystemExit(bool(fail))
