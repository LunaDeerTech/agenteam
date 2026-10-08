import json
import pathlib

root = pathlib.Path(__file__).resolve().parent
handover = {
    'source': 'Current task coordination messages, recorded after the actions; these are not command execution logs.',
    'scope': 'No product/script/lock edits; dependency commands only. Documentation added later under two explicitly assigned paths.',
    'events': [
        {'from': 'root', 'to': 'fixture_recovery', 'action': 'Authorized restoring only the original fixed PostgreSQL digests and exact MinIO source/binary in the owned scratch root. Prohibited Git, delegation, containers, databases, listeners and touching existing infrastructure.'},
        {'from': 'root', 'to': 'fixture_recovery', 'action': 'Expanded scope to be the sole installer for tests/account-captcha-web npm ci and matching browser dependencies, preserving lock bytes.'},
        {'from': 'root', 'to': 'fixture_recovery', 'action': 'Authorized necessary web locked dependency repair only after frontend_worker acknowledges no active Node readers.'},
        {'from': 'frontend_worker', 'to': 'fixture_recovery', 'action': 'ACK: no current Node readers/checks/background tasks; continued source/test editing only and promised no Node commands until the install window was released.'},
        {'from': 'fixture_recovery', 'to': 'frontend_worker', 'action': 'Released the dependency window after successful harness npm ci, identical locked integrity verification, copying only missing go-captcha-vue@2.0.7, two successful npm ls commands and unchanged package/lock fingerprints.'},
        {'from': 'root', 'to': 'fixture_recovery', 'action': 'Directed reuse of existing /usr/bin/chromium when version and libraries are complete; no additional browser download required. No optional browser download had started and none was performed.'},
        {'from': 'fixture_recovery', 'to': 'root and verification_worker', 'action': 'Reported exact MinIO binary SHA/version/size, both fixed PostgreSQL digest inspections, locked Node dependency completion and browser version/library observations; no dynamic resource or product verification claimed.'},
        {'from': 'root', 'to': 'fixture_recovery', 'action': 'Authorized only docs/development/agent-team/environment-test-dependencies-2026-10-08.md and corresponding -evidence/ for this archive. Entry points, task ledger and Git remain root-owned.'}
    ]
}
(root / 'evidence/window-handover.json').write_text(json.dumps(handover, indent=2, ensure_ascii=False) + '\n')

# These source texts are transcribed from the exact tool-call stdin submitted in
# this turn. The original command wrapper recorded argv/env/output, not stdin.
sources = {
    'cache-inventory': '''import json, os, pathlib, shutil
roots=['/workspace/go/pkg/mod/cache/download/github.com/minio/minio/@v','/workspace/go/pkg/mod/github.com/minio','/workspace/go/pkg/mod/cache/download/github.com/philhofer/fwd/@v','/workspace/go/pkg/mod/cache/download/github.com/tinylib/msgp/@v','/workspace/scratch/fixture-recovery/bin','/tmp/agenteam-d05-r01-hxwb6ogm/bin','/root/.cache/ms-playwright','/home/agent/.cache/ms-playwright','/ms-playwright','/opt/pw-browsers','/opt/codex/runtimes/codex-primary-runtime/dependencies/node','/workspace/toolchains','/workspace/agenteam/web/node_modules']
for item in roots:
 p=pathlib.Path(item)
 print(json.dumps({'path':item,'exists':p.exists(),'entries': sorted(x.name for x in p.iterdir()) if p.is_dir() else None}))
print(json.dumps({'env':{k:v for k,v in os.environ.items() if k in ['NODE_PATH','PLAYWRIGHT_BROWSERS_PATH','PLAYWRIGHT_NODEJS_PATH','HOME','PATH','GOMODCACHE','GOCACHE','GOPATH']},'commands':{x:shutil.which(x) for x in ['node','npm','bwrap','go','docker']}}))
''',
    'cache-inventory02': '''import json,os,pathlib,shutil
roots=['/root/.cache/ms-playwright','/home/agent/.cache/ms-playwright','/ms-playwright','/opt/pw-browsers','/opt/codex/runtimes/codex-primary-runtime/dependencies/node','/opt/codex/runtimes/cua/lib/node_modules','/workspace/toolchains','/workspace/agenteam/web/node_modules','/workspace/go/pkg/mod/cache/download/github.com/minio/minio/@v']
for item in roots:
 p=pathlib.Path(item)
 try: result={'path':item,'exists':p.exists(),'entries': sorted(x.name for x in p.iterdir()) if p.is_dir() else None}
 except OSError as e: result={'path':item,'error':str(e)}
 print(json.dumps(result))
print(json.dumps({'env':{k:v for k,v in os.environ.items() if k in ['NODE_PATH','PLAYWRIGHT_BROWSERS_PATH','PLAYWRIGHT_NODEJS_PATH','HOME','PATH','GOMODCACHE','GOCACHE','GOPATH']},'commands':{x:shutil.which(x) for x in ['node','npm','bwrap','go','docker']}}))
''',
    'playwright-inventory': '''const fs=require('fs');
console.log(JSON.stringify({node:process.version,nodePath:process.execPath}));
for (const id of ['playwright','playwright-core','@playwright/test']) {
 try {
 const p=require(id), path=require.resolve(id), pkg=require(id+'/package.json');
 console.log(JSON.stringify({id,path,version:pkg.version,browsers:Object.fromEntries(['chromium','firefox','webkit'].filter(n=>p[n]).map(n=>[n,{path:p[n].executablePath(),exists:fs.existsSync(p[n].executablePath())}]))}));
 } catch(e) { console.log(JSON.stringify({id,error:e.message})); }
}
''',
    'missing-module-verify': '''import json,pathlib,hashlib
r=pathlib.Path('/workspace/scratch/fixture-recovery-new'); old=pathlib.Path('/workspace/agenteam/docs/development/agent-team/environment-test-dependencies-2026-10-07-evidence/evidence/missing-module-verification.json')
raw=(r/'evidence/missing-module-download.raw').read_text(); dec=json.JSONDecoder(); out=[]
while raw.strip():
 o,i=dec.raw_decode(raw.lstrip());out.append(o);raw=raw.lstrip()[i:]
for current, expected in zip(out,json.loads(old.read_text()),strict=True):
 for k in ['Path','Version','Sum','GoModSum','Origin']: assert current[k]==expected[k],k
 current['files']={}
 for k in ['Info','GoMod','Zip']:
  p=pathlib.Path(current[k]); got=hashlib.sha256(p.read_bytes()).hexdigest(); assert got==expected['files'][k]['sha256'],k
  current['files'][k]={'owned_path':str(p),'sha256':got}
(r/'evidence/missing-module-verification.json').write_text(json.dumps(out,indent=2)+'\\n'); print(json.dumps(out,indent=2))
''',
    'web-missing-package-restore': '''import json,pathlib,shutil,hashlib
r=pathlib.Path('/workspace/agenteam'); src=r/'tests/account-captcha-web/node_modules/go-captcha-vue'; dst=r/'web/node_modules/go-captcha-vue'
a=json.loads((r/'web/package-lock.json').read_text())['packages']['node_modules/go-captcha-vue']
b=json.loads((r/'tests/account-captcha-web/package-lock.json').read_text())['packages']['node_modules/go-captcha-vue']
assert a==b
assert not dst.exists()
assert json.loads((src/'package.json').read_text())['version']==a['version']
shutil.copytree(src,dst)
h=hashlib.sha256();count=0;size=0
for p in sorted(dst.rglob('*')):
 if p.is_file():
  rel=p.relative_to(dst); data=p.read_bytes(); assert data==(src/rel).read_bytes(); h.update(str(rel).encode()+b'\\0'+hashlib.sha256(data).digest());count+=1;size+=len(data)
print(json.dumps({'source':str(src),'destination':str(dst),'locked_version':a['version'],'locked_integrity':a['integrity'],'files':count,'bytes':size,'copied_tree_sha256':h.hexdigest()}))
''',
    'lock-installed-check': '''import json,pathlib,hashlib
r=pathlib.Path('/workspace/agenteam')
for name in ['web','tests/account-captcha-web']:
 root=r/name; lock=json.loads((root/'package-lock.json').read_text());missing=[];mismatch=[];found=0;optional_missing=0
 for path,info in lock['packages'].items():
  if not path:continue
  p=root/path/'package.json'
  if not p.exists():
   if info.get('optional'): optional_missing+=1
   else: missing.append(path)
   continue
  found+=1; actual=json.loads(p.read_text()).get('version')
  if actual!=info.get('version'):mismatch.append({'path':path,'actual':actual,'locked':info.get('version')})
 print(json.dumps({'root':name,'lock_sha256':hashlib.sha256((root/'package-lock.json').read_bytes()).hexdigest(),'installed_versions_checked':found,'optional_missing':optional_missing,'required_missing':missing,'mismatch':mismatch}))
 assert not missing and not mismatch
'''
}
result = {
    'provenance': 'Recorded after execution from the exact submitted tool-call stdin texts; these were not independently captured by run.py at command start.',
    'scripts': sources
}
(root / 'evidence/submitted-stdin.json').write_text(json.dumps(result, indent=2, ensure_ascii=False) + '\n')
