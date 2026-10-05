from pathlib import Path
import subprocess,json,re,hashlib
root=Path(__file__).parent;repo=Path('/workspace/agenteam');rev=json.loads((root/'baseline.json').read_text())['baseline'];dst=root/'snapshot';dst.mkdir()
paths=subprocess.check_output(['git','ls-tree','-r','--name-only',rev],cwd=repo).decode().splitlines();go=[p for p in paths if p.endswith('.go')];by={}
for p in go:by.setdefault(str(Path(p).parent),[]).append(p)
test_roots=['internal/central/postgres','tests/database','internal/central/app','tests/process','tests/security','tests/outbox','internal/central/outbox','tests/account','tests/accountmail','internal/central/accountmail','internal/central/recoverylog','tests/project','internal/central/model','tests/model','tests/objects','internal/central/object']
tests={p for p in by if any(p==r or p.startswith(r+'/') for r in test_roots)}
queue=list(tests)+['cmd/agenteam','cmd/agenteam-runner','tests/testsupport/objectstore/cmd/fixture','tests/testsupport/outbound/cmd/fixture','tests/testsupport/postgres/cmd/fixture','tests/testsupport/outbound/cmd/server'];seen=set();selected={};prefix='github.com/LunaDeerTech/agenteam/'
while queue:
 pkg=queue.pop()
 if pkg in seen:continue
 seen.add(pkg);assert pkg in by,pkg
 for p in by[pkg]:
  if p.endswith('_test.go') and pkg not in tests:continue
  raw=subprocess.check_output(['git','show',rev+':'+p],cwd=repo);selected[p]=raw;s=raw.decode()
  imports=re.findall(r'^import\s+(?:[A-Za-z_.][\w.]*\s+)?"([^"]+)"',s,re.M)
  for b in re.findall(r'\bimport\s*\((.*?)\)',s,re.S):imports+=re.findall(r'"([^"]+)"',b)
  queue += [i[len(prefix):] for i in imports if i.startswith(prefix)]
assets=['go.mod','go.sum','scripts/test-objects.sh','scripts/test-security.sh','scripts/test-postgres.sh']
assets += [p for p in paths if p.startswith(('db/migrations/','api/openapi/')) and not p.endswith('.go') and Path(p).suffix in ['.sql','.json']]
assets += [p for p in paths if '/testdata/' in p and any(p.startswith(pkg+'/') for pkg in seen)]
for p in assets:selected[p]=subprocess.check_output(['git','show',rev+':'+p],cwd=repo)
for p,raw in selected.items():q=dst/p;q.parent.mkdir(parents=True,exist_ok=True);q.write_bytes(raw)
(root/'inputs/fixed-closure.json').write_text(json.dumps({'baseline':rev,'note':'fixed Go import closure plus original driver targets and required source assets; no docs/evidence/web/.git/cache copy','packages':sorted(seen),'files':[{'path':p,'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)} for p,raw in sorted(selected.items())]},indent=2)+'\n')
print(json.dumps({'packages':len(seen),'files':len(selected),'bytes':sum(map(len,selected.values()))}))
