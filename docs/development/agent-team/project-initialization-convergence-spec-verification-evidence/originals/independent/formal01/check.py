from pathlib import Path
import hashlib, json, re, sys

B = Path('/workspace/scratch/project-create-frontier4089')
O = Path('/workspace/scratch/project-initialization-convergence-verification/formal01')
P = Path('/workspace/agenteam/docs/development/work-items/d08-project-initialization-convergence.md')
def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()
expected = {
 P: '5530d79eef389085bd27e61c6bad00f61e779d0d5ec296e00abfd00e63e245ae',
 B/'formal-rev1.freeze.json': '7246a4785adbdfa1095adccb31a3b63dcb243526d2b303db6cac08408f830c90',
 B/'scratch-to-formal-rev1.diff': '5e1db26411b56f7abe8335df5607d89291382366ec47d9826e228ffe889bb17d',
 B/'draft-rev1.md': '34346798c4e526bf56c08cea24c4fe0ede8f59a73cb1fc8c9bbe222479db4f7b',
 B/'inputs01.json': '057d9f57e0e7f2786444fcc685c11cda23557587c7bb9dab7bd45f478176367e',
 O.parent/'rev1/result.json': 'a583df8a99b3a9d492766c0a8cbbbadd6b8f75c52301d5e5ebfc1a0b503d6d61',
}
errors = [str(p) for p,h in expected.items() if sha(p) != h]
frozen = json.loads((B/'formal-rev1.freeze.json').read_text())
for f in frozen['files']:
 if sha(Path(f['path'])) != f['sha256']: errors.append('formal freeze: '+f['path'])

text = P.read_text()
old = (B/'draft-rev1.md').read_text()
body = lambda s: s[s.index('## 1.'):s.index('## 7.')]
converted = body(old)
replacements = json.loads((B/'formal-replacements.json').read_text())['replacements']
counts = []
for a,b in replacements:
 counts.append(converted.count(a)); converted = converted.replace(a,b)
if converted != body(text): errors.append('technical body changed beyond declared replacements')
if any(n != 1 for n in counts): errors.append('unexpected replacement multiplicity')
reverse = re.compile('|'.join(re.escape(b) for _,b in sorted(replacements,key=lambda r:len(r[1]),reverse=True)))
mapping = {b:a for a,b in replacements}
restored = reverse.sub(lambda m:mapping[m.group()],body(text))
if restored != body(old): errors.append('reverse technical bytes differ')

sources = json.loads((B/'inputs01.json').read_text())['sources']
rows = re.findall(r'^\| \[([^\]]+)\]\(([^)]+)\) \| `([0-9a-f]{64})` \| `([0-9a-f]{40})` \|$',text,re.M)
actual = {p:(h,g) for p,_,h,g in rows}
wanted = {x['path']:(x['sha256'],x['git_blob']) for x in sources}
if actual != wanted or len(rows)!=57: errors.append('57 source index differs')
stopped=json.loads((B/'stopped-source-check.json').read_text())
for x in stopped['unchanged_critical_sources']:
 pattern='| ['+x['path']+'](../../../'+x['path']+') | `'+x['old_and_current_blob']+'` |'
 if pattern not in text: errors.append('stopped blob summary: '+x['path'])

links=[]
for target in re.findall(r'\[[^\]]+\]\(([^)]+)\)',text):
 path,_,fragment=target.partition('#')
 resolved=(P.parent/path).resolve() if path else P
 valid=resolved.is_file()
 if fragment:
  # The only fragment in this card targets its explicit Chinese heading.
  valid=valid and resolved==P and fragment=='停止源同字节核对' and '### 停止源同字节核对\n' in text
 links.append({'target':target,'valid':valid})
 if not valid: errors.append('link: '+target)
if re.search(r'\]\((?:fixed/|[^)]*scratch)',text): errors.append('nonportable link')
raw=P.read_bytes()
if b'\r' in raw or not raw.endswith(b'\n') or any(s.rstrip()!=s for s in text.splitlines()): errors.append('format')
for p,h in expected.items():
 if sha(p)!=h: errors.append('input changed: '+str(p))
out={'kind':'formal-document-narrow-static-check','inputs':[{'path':str(p),'sha256':h} for p,h in expected.items()], 'formal_freeze_files':len(frozen['files']),'reversible_replacements':len(replacements),'section_1_to_6_reverse_bytes_equal':restored==body(old),'source_rows':len(rows),'stopped_source_rows':4,'links':links,'errors':errors,'pass':not errors,'executed_go_resources_git':False}
(O/'checks.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({k:v for k,v in out.items() if k not in ['inputs','links']},ensure_ascii=False))
print('links_checked='+str(len(links)))
sys.exit(bool(errors))
