from pathlib import Path
import hashlib,json,re,unicodedata,sys
from urllib.parse import unquote
root=Path('/workspace/agenteam');manifest=Path(sys.argv[1]);output=Path(sys.argv[2]);m=json.loads(manifest.read_text())
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
checks=[]
def anchors(p):
 s=p.read_text();out=set(re.findall(r'<a\s+(?:id|name)=["\']([^"\']+)',s));seen={};fence=False
 for line in s.splitlines():
  if re.match(r'^\s*(```|~~~)',line):fence=not fence;continue
  if fence:continue
  hit=re.match(r'^#{1,6}\s+(.+?)\s*#*$',line)
  if not hit:continue
  slug=re.sub(r'<[^>]*>','',hit.group(1)).lower().replace('`','')
  slug=''.join(c for c in slug if c in '-_ ' or not unicodedata.category(c).startswith(('P','S','C'))).replace(' ','-')
  n=seen.get(slug,0);seen[slug]=n+1;out.add(slug if n==0 else f'{slug}-{n}')
 return out
assert len(m['sources'])==16
for row in m['sources']:
 original=root/row['path'];snap=Path(row['snapshot']);assert sha(original)==sha(snap)==row['sha256']
 if row['card_path']<15:continue
 s=snap.read_text();assert s.endswith('\n') and '\r' not in s and all(line==line.rstrip() for line in s.splitlines())
 assert sum(bool(re.match(r'^\s*(```|~~~)',line)) for line in s.splitlines())%2==0
 for target in re.findall(r'(?<!!)\[[^\]]*\]\(([^)]+)\)',s):
  if re.match(r'^([a-z]+://|mailto:)',target):continue
  name,_,fragment=target.strip('<>').partition('#');q=(original.parent/unquote(name)).resolve() if name else original
  assert q.is_file(),(row['path'],target)
  if fragment:assert unquote(fragment) in anchors(q),(row['path'],target)
  checks.append({'source':row['path'],'target':target,'path':str(q),'sha256':sha(q)})
output.write_text(json.dumps({'status':'PASS_SOURCE16_FORMAT_LOCAL_LINKS','manifest_sha256':sha(manifest),'local_links':checks,'count':len(checks),'technical_source_count':14,'doc_count':2},ensure_ascii=False,indent=2)+'\n')
print('PASS',len(checks),sha(output))
