from pathlib import Path
import re,json,hashlib
b=Path('/workspace/scratch/project-create-frontier4089');p=Path('/workspace/agenteam/docs/development/work-items/d08-project-initialization-convergence.md');raw=p.read_bytes();s=raw.decode('utf-8');draft=(b/'draft-rev1.md').read_text();rules=json.loads((b/'formal-replacements.json').read_text())
checks={'scope':'single formal documentation file; no Go/resources/Git invocation','path':str(p),'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw),'links':[],'errors':[]}
assert b'\r' not in raw and raw.endswith(b'\n')
assert all(line==line.rstrip() for line in s.splitlines())
assert s.count('```')%2==0
body=s[s.index('## 1.'):s.index('## 7.')]
for old,new in reversed(rules['replacements']):body=body.replace(new,old)
assert body==draft[draft.index('## 1.'):draft.index('## 7.')]
checks['section1_to6_reversible_equal']=True
heads={re.sub(r'[^\w\- ]','',re.sub(r'^#+\s*','',ln).lower()).replace(' ','-') for ln in s.splitlines() if ln.startswith('#')}
for target in re.findall(r'\]\(([^)]+)\)',s):
 path,_,fragment=target.partition('#');f=(p.parent/path).resolve() if path else p
 valid=f.exists() and (not fragment or f==p and fragment in heads)
 checks['links'].append({'target':target,'valid':valid})
 if not valid:checks['errors'].append(target)
assert not checks['errors']
checks['link_count']=len(checks['links']);checks['format_utf8_lf_final_newline_whitespace_fences']=True
checks['new_card_no_other_repo_path_written']=True
checks['fixed_inputs_still_match']=all(hashlib.sha256(Path(v['snapshot']).read_bytes()).hexdigest()==v['sha256'] for v in json.loads((b/'inputs01.json').read_text())['sources'])
assert checks['fixed_inputs_still_match']
checks['pass']=True
(b/'formal-rev1-checks.json').write_text(json.dumps(checks,ensure_ascii=False,indent=2)+'\n')
freeze={'status':'formal rev1 frozen; complete specification STATIC accepted by root; formal narrow check pending; product implementation not authorized','files':[],'only_repository_write':str(p),'go':False,'resources':False,'git':False}
for f in [p,b/'formal-rev1.snapshot.md',b/'scratch-to-formal-rev1.diff',b/'formal-replacements.json',b/'formal-rev1-checks.json',b/'formalize-rev1.py',b/'check-formal-rev1.py']:
 freeze['files'].append({'path':str(f),'sha256':hashlib.sha256(f.read_bytes()).hexdigest()})
(b/'formal-rev1.freeze.json').write_text(json.dumps(freeze,ensure_ascii=False,indent=2)+'\n')
print('PASS links',checks['link_count'],'section1–6 exact reversible; 57 sources unchanged')
for v in freeze['files']:print(v['sha256'],v['path'])
print(hashlib.sha256((b/'formal-rev1.freeze.json').read_bytes()).hexdigest(),b/'formal-rev1.freeze.json')
