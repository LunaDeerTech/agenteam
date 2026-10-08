from pathlib import Path
import hashlib,json,re,unicodedata,difflib
from urllib.parse import unquote
R=Path('/workspace/agenteam');B=Path('/workspace/scratch/project-owner-audit-http-author');D=B/'docs16v02';h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();ref=lambda p:{'path':str(p),'sha256':h(p)}
def anchors(p):
 text=p.read_text();result=set(re.findall(r'<a\s+(?:id|name)=["\']([^"\']+)',text));counts={};fence=False
 for line in text.splitlines():
  if re.match(r'^\s*(```|~~~)',line):fence=not fence;continue
  if fence:continue
  m=re.match(r'^#{1,6}\s+(.+?)\s*#*$',line)
  if not m:continue
  slug=re.sub(r'<[^>]*>','',m.group(1)).lower().replace('`','')
  slug=''.join(c for c in slug if c in '-_ ' or not unicodedata.category(c).startswith(('P','S','C'))).replace(' ','-')
  n=counts.get(slug,0);counts[slug]=n+1;result.add(slug if not n else f'{slug}-{n}')
 return result
files=['docs/development/backend/audit.md','docs/development/backend/README.md'];checked=[];deltas=[];docrows=[]
for rel in files:
 p=R/rel;b=p.read_bytes();s=b.decode('utf-8');assert '\r' not in s and s.endswith('\n') and not any(line.rstrip()!=line for line in s.splitlines());assert sum(1 for x in s.splitlines() if re.match(r'^(```|~~~)',x))%2==0
 for target in re.findall(r'(?<!!)\[[^\]]*\]\(([^)]+)\)',s):
  if re.match(r'^([a-z]+://|mailto:)',target):continue
  target=target.strip('<>');path,_,fragment=target.partition('#');t=(p.parent/unquote(path)).resolve() if path else p;assert t.exists(),(rel,target)
  if fragment:assert unquote(fragment) in anchors(t),(rel,target)
  checked.append({'source':rel,'target':target,'resolved':str(t),'fragment':unquote(fragment)})
 before=D/'before'/rel;snap=D/'src'/rel;snap.parent.mkdir(parents=True,exist_ok=True);snap.write_bytes(b);deltas.extend(difflib.unified_diff(before.read_text().splitlines(True),s.splitlines(True),fromfile='before/'+rel,tofile='after/'+rel));docrows.append({'path':rel,'sha256':h(p),'snapshot':str(snap),'before':ref(before)})
(D/'delta.diff').write_text(''.join(deltas));(D/'checks.json').write_text(json.dumps({'status':'PASS','utf8_lf_finalnewline_no_trailing_whitespace':True,'fence_pairs':True,'local_links_and_fragments':checked,'count':len(checked),'note':'No Git command or product test; direct original-byte diff and local link/format checks only.'},indent=2)+'\n')
source=json.loads((B/'technical-final02/sources.json').read_text());rows=source['sources']
for row in rows:assert h(R/row['path'])==row['sha256'] and h(Path(row['snapshot']))==row['sha256']
assert len(rows)==14
for i,row in enumerate(docrows,15):row['card_path']=i
m={'status':'author full16 frozen for independent final document STATIC; technical14 unchanged','baseline':'cc850b2244cad771eb862a2c82d99887d5da7284','sources':rows+docrows,'technical_result':ref(B/'technical-final02/result.json'),'technical_report':ref(B/'technical-final02/report.md'),'candidate02':ref(B/'candidate02/manifest.json'),'delta':ref(D/'delta.diff'),'checks':ref(D/'checks.json'),'no_go_or_resources_or_git':True}
(D/'manifest16.json').write_text(json.dumps(m,indent=2)+'\n');print('links',len(checked))
for p in [D/'manifest16.json',D/'delta.diff',D/'checks.json']+[R/x for x in files]:print(p,h(p))
