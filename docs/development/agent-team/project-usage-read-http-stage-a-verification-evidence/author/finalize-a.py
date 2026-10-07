from pathlib import Path
import json,hashlib,time,subprocess
base=Path('/workspace/scratch/usage-http-author');root=Path('/workspace/agenteam')
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
original=json.loads((base/'a03-execution-input.json').read_text())['files']
changed=[]
for name,entry in original.items():
 p=Path(name)
 if not p.is_file() or sha(p)!=entry['sha256']:changed.append(name)
raw=(base/'a10-race-list/raw.log').read_text();decoder=json.JSONDecoder();pos=0;paths=set();packages=[]
while pos<len(raw):
 while pos<len(raw) and raw[pos].isspace():pos+=1
 if pos==len(raw):break
 package,pos=decoder.raw_decode(raw,pos);packages.append(package)
 if not package.get('Dir'):continue
 for key in ['GoFiles','CgoFiles','EmbedFiles','CFiles','CXXFiles','HFiles','SFiles','SysoFiles']:
  for name in package.get(key,[]):
   path=Path(package['Dir'])/name
   if not path.is_file():raise RuntimeError('missing actual race input '+str(path))
   paths.add(path)
extra={str(p):{'sha256':sha(p),'bytes':p.stat().st_size} for p in sorted(paths) if str(p) not in original}
fixedFive=json.loads((base/'a02-current-five.json').read_text())
assert all(sha(root/name)==entry['sha256'] for name,entry in fixedFive.items())
assert changed==[str(base/'run.py')],changed
artifacts=[base/'run.py',base/'account.test',base/'wire.test',base/'a08-wire-pure/schema/schema-input.json',base/'a08-wire-pure/schema/python-check.txt',base/'a08-wire-pure/schema/node-check.txt',Path('/usr/bin/gcc').resolve()]
result={'five_manifest_sha256':sha(base/'a02-current-five.json'),'original_execution_manifest_sha256':sha(base/'a03-execution-input.json'),'race_list_sha256':sha(base/'a10-race-list/raw.log'),'actual_race_packages':len(packages),'race_additional_inputs':extra,'changed_original_inputs':changed,'driver_original_preserved':str(base/'a04-pure/runner-original.py'),'artifacts':{str(p):{'sha256':sha(p),'bytes':p.stat().st_size} for p in artifacts}}
(base/'stage-a-final-input.json').write_text(json.dumps(result,indent=2)+'\n')
commands=[];groups={}
for path in sorted(base.glob('a*/command.json')):
 r=json.loads(path.read_text());commands.append({'label':path.parent.name,'exit':r['exit'],'elapsed':r['finished']-r['started'],'before_after_equal':r['before']==r['after'],'adopted_waits':r.get('adopted_waits'),'command_record_sha256':sha(path),'raw_sha256':sha(path.parent/'raw.log')});groups[r['pid']]={'label':path.parent.name,'members':[]}
for path in Path('/proc').iterdir():
 if not path.name.isdigit():continue
 try:
  raw=(path/'stat').read_text();fields=raw[raw.rfind(')')+2:].split();pgid=int(fields[2])
  if pgid in groups:groups[pgid]['members'].append({'pid':int(path.name),'ppid':int(fields[1]),'state':fields[0],'starttime':fields[19]})
 except (FileNotFoundError,PermissionError,ProcessLookupError):pass
out={'at':time.time(),'groups':groups,'commands':commands,'product_five_unchanged':True,'final_input_sha256':sha(base/'stage-a-final-input.json')}
(base/'stage-a-final-state.json').write_text(json.dumps(out,indent=2)+'\n')
print(json.dumps({'commands':commands,'race_extra_files':len(extra),'original_changes':changed,'process_members':{pid:g for pid,g in groups.items() if g['members']},'final_input_sha256':out['final_input_sha256'],'final_state_sha256':sha(base/'stage-a-final-state.json')},indent=2))
