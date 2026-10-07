import json,pathlib,hashlib,subprocess
base=pathlib.Path('/workspace/scratch/usage-http-verification/a02');base.mkdir(exist_ok=True)
source=pathlib.Path('/workspace/agenteam'); manifest=pathlib.Path('/workspace/scratch/usage-http-author/a02-current-five.json')
assert hashlib.sha256(manifest.read_bytes()).hexdigest()=='8d78d3b4de0fb8fcb8f0fb8af6ea4ff92fe03c40554d24014b0be422b0e8b7a9'
entries=json.loads(manifest.read_text())
for name,expected in entries.items():
 raw=(source/name).read_bytes(); assert len(raw)==expected['bytes'] and hashlib.sha256(raw).hexdigest()==expected['sha256'],name
 target=base/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(raw)
name='api/openapi/common.json';raw=subprocess.check_output(['git','show','6fa2ee72:'+name],cwd=source)
target=base/name;target.write_bytes(raw);entries[name]={'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw),'fixed_git':'6fa2ee72'}
(base/'inputs.json').write_text(json.dumps(entries,indent=2)+'\n')
print('Frozen exactly five candidate paths plus fixed common.json; six hashes verified. No full tree copy.')
