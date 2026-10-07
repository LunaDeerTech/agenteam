import pathlib,json,hashlib,time
base=pathlib.Path('/workspace/scratch/usage-http-verification/a02');author=pathlib.Path('/workspace/scratch/usage-http-author')
expected={'stage-a-final-input.json':'ba247577940319f250b2a5d409848db6c569e38aa4e5d8bdaa5f82ab2ee4a3e0','stage-a-final-state.json':'199222a69c75431bf9c596942f2e6c985fb9ca9fe009baed9c7c8aa0feedf7f1'}
for name,want in expected.items():assert hashlib.sha256((author/name).read_bytes()).hexdigest()==want
inputs=json.loads((author/'stage-a-final-input.json').read_text());state=json.loads((author/'stage-a-final-state.json').read_text());verified=0
for section in ['race_additional_inputs','artifacts']:
 for name,rec in inputs[section].items():
  raw=pathlib.Path(name).read_bytes();assert len(raw)==rec['bytes'] and hashlib.sha256(raw).hexdigest()==rec['sha256'],name;verified+=1
own=[]
for p in base.glob('independent-*/command.json'):own.append(json.loads(p.read_text())['pid'])
author_groups=set(state['groups']);found=[];ownfound=[]
for p in pathlib.Path('/proc').iterdir():
 if not p.name.isdigit():continue
 try:
  fields=(p/'stat').read_text().split(') ',1)[1].split();rec={'pid':int(p.name),'state':fields[0],'ppid':int(fields[1]),'pgrp':fields[2],'starttime':fields[19]}
 except (FileNotFoundError,ProcessLookupError,PermissionError):continue
 if fields[2] in author_groups:found.append(rec)
 if int(fields[2]) in own:ownfound.append(rec)
expected_z={'pid':22765,'state':'Z','ppid':1,'pgrp':'20527','starttime':'81512'}
assert found==[expected_z],found
assert not ownfound,ownfound
report={'author_final_hashes':expected,'actual_verified_race_and_artifact_inputs':verified,'author_groups_remaining':found,'independent_groups_remaining':ownfound,'own_runs':len(own),'at':time.time(),'limitation':'Author a04 compiler is historical unjoined zombie; no false cleanup/adopted wait claimed.'}
(base/'tail-result.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,indent=2))
