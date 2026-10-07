from pathlib import Path
import hashlib,json,re,subprocess
ROOT=Path('/workspace/agenteam')
OUT=Path(__file__).parent
AUTHOR=Path('/workspace/scratch/project-owner-update-http-spec')
CARD=ROOT/'docs/development/work-items/d08-project-owner-update-http.md'
BASE='901eb54605d293d4308caadd278c2c3a7ae1b824'
sha=lambda b:hashlib.sha256(b).hexdigest()
commands=[]
def git(*args):
 p=subprocess.run(['git',*args],cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 commands.append({'argv':['git',*args],'exit':p.returncode})
 return p
raw=(AUTHOR/'rev1.md').read_bytes(); manifest_raw=(AUTHOR/'frozen01.json').read_bytes(); m=json.loads(manifest_raw)
assert sha(raw)=='f504baf6c91b291c38bdcc0b6590aaa68cc86554466c6ae15694b95de6b77669'
assert sha(manifest_raw)=='3151ebc3dfdf682437cea978741a1803fb08f7eb07dd151bd542881243884bf1'
assert CARD.read_bytes()==raw
source_checks=[]
for p,h in m['source_files'].items():
 fixed=git('show',BASE+':'+p)
 fixed_hash=sha(fixed.stdout) if fixed.returncode==0 else None
 current_hash=sha((ROOT/p).read_bytes())
 assert current_hash==h,(p,'frozen drift')
 source_checks.append({'path':p,'frozen_sha256':h,'fixed_product_sha256':fixed_hash,'current_matches_frozen':True,'fixed_product_matches':h==fixed_hash})
assert [x['path'] for x in source_checks if not x['fixed_product_matches']]==['docs/development/work-items/d08-project-owner-read-http.md','docs/development/agent-team/project-owner-read-http-verification.md']
scope=[]
assert len(m['scope_paths'])==19
assert len(set(x['path'] for x in m['scope_paths']))==19
for f in m['scope_paths']:
 p=f['path']; check=git('cat-file','-e',BASE+':'+p)
 if f['new']:
  assert check.returncode!=0 and not (ROOT/p).exists(),p
 else:
  assert check.returncode==0,p
  data=git('show',BASE+':'+p).stdout
  assert sha(data)==f['baseline_sha256'] and sha((ROOT/p).read_bytes())==f['baseline_sha256'],p
 scope.append({**f,'verified':True})
text=raw.decode('utf-8'); assert '\r' not in text and text.endswith('\n') and not any(x.rstrip()!=x for x in text.splitlines())
def slug(s):
 s=re.sub(r'[`*_~]','',s).lower()
 s=''.join(ch for ch in s if ch.isalnum() or ch in '_- ')
 return s.replace(' ','-')
links=[]
for value in re.findall(r'\[[^\]]*\]\(([^)]+)\)',text):
 path,sep,fragment=value.partition('#'); target=(CARD.parent/path).resolve()
 assert target.is_file(),value
 target_text=target.read_text(); heading_slugs=[slug(x) for x in re.findall(r'^#{1,6}\s+(.+?)\s*$',target_text,re.M)]
 if sep: assert fragment in heading_slugs,(value,heading_slugs)
 links.append({'reference':value,'target':str(target),'sha256':sha(target.read_bytes()),'fragment_checked':bool(sep)})
assert len(links)==9
ws=git('diff','--no-index','--check','/dev/null',str(AUTHOR/'rev1.md'))
assert ws.returncode==0,(ws.returncode,ws.stdout,ws.stderr)
(OUT/'diff-check.raw').write_bytes(ws.stdout+ws.stderr)
oldtops=['TestProjectB02AuditEventReceiptTouchAtomicityAndNoOp','TestProjectB02TwoPlannedTargetsCompeteForOneCanonicalName','TestProjectB02ReceiptRequiresCurrentSessionAndReplaysAcrossRenewal','TestProjectB02UnknownOriginalCallResolvesAfterWriterSerialization','TestModelProjectOwnerReadHTTPProjectionAndPaging','TestModelProjectOwnerReadHTTPRootBinding','TestModelProjectUsageHTTPRootBinding','TestModelMeetingSummarySettingsRoot','TestModelSystemHTTPConfigurationCRUD']
selectors=[]
for top in oldtops:
 found=git('grep','-n','func '+top+'(',BASE,'--','tests/project','tests/model')
 assert found.returncode==0
 selectors.append({'name':top,'fixed_source':found.stdout.decode().strip()})
assert CARD.read_bytes()==raw and (AUTHOR/'rev1.md').read_bytes()==raw and (AUTHOR/'frozen01.json').read_bytes()==manifest_raw
result={'status':'STATIC_INPUT_AND_DOCUMENT_CHECKS_PASS','product_baseline':BASE,'card_sha256':sha(raw),'author_freeze_sha256':sha(manifest_raw),'sources':source_checks,'post_product_document_provenance':'42/44 match product Git. Remaining 2 are the frozen accepted Owner-read archival header/result; product dependencies all match. Archive adds acceptance history, not new product requirements.','scope':scope,'links':links,'old_selectors':selectors,'format':'UTF8/LF/final_newline/no_trailing_whitespace/git_diff_check PASS','card_before_after_same':True,'commands':commands,'script_sha256':sha(Path(__file__).read_bytes()),'limitations':['Static only; no Go/Node/test/resource execution','No implementation acceptance','No product/card/Git mutations']}
(OUT/'checks.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'status':result['status'],'source_files':len(source_checks),'fixed_product_matches':sum(x['fixed_product_matches'] for x in source_checks),'scope_paths':len(scope),'new_paths':sum(x['new'] for x in scope),'links':len(links),'old_selectors':len(selectors),'checks_sha256':sha((OUT/'checks.json').read_bytes()),'script_sha256':result['script_sha256']},ensure_ascii=False,indent=2))
