#!/usr/bin/env python3
"""Reuse the accepted finite mapping method for the three cost cases.

Only time/resource observations are doubles; no subprocess is started except
read-only git show. The existing supervisor/parser performs the tail checks.
"""
import io,json,os,re,subprocess,tempfile,types
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
OUTPUT=ROOT/'output/ai/object-metadata-cleanup/entry-controls';OUTPUT.mkdir(parents=True,exist_ok=True)
SELECTOR='^TestObjectMetadataCleanup(ProjectHistoryPlans|SkillsIndexPlans|TransferAndForeignKeyPlans)$'
NAMES=['TestObjectMetadataCleanupProjectHistoryPlans','TestObjectMetadataCleanupSkillsIndexPlans','TestObjectMetadataCleanupTransferAndForeignKeyPlans']
paths=['.agent-state/work-owner-http/root_chain_driver.py','.agent-state/task-planning-recovery/pg_only_supervisor.py']
modules=[]
for path in paths:
 text=(ROOT/path).read_text();old=subprocess.check_output(['git','show','365b2729:'+path],cwd=ROOT,text=True)
 inverse=text
 if path==paths[0]:
  start,end=inverse.index('def metadata_cost_inputs():'),inverse.index('def configuration(')
  inverse=inverse[:start]+inverse[end:]
 else:
  block="        if args.run == '"+SELECTOR+"':\n            inputs.update({str(p): adapter.sha(p) for p in adapter.metadata_cost_inputs()})\n"
  assert inverse.count(block)==1;inverse=inverse.replace(block,'')
 lines=inverse.splitlines(keepends=True);added=[i for i,l in enumerate(lines) if SELECTOR in l];assert len(added)==1
 assert ''.join(l for i,l in enumerate(lines) if i!=added[0])==old, 'old source changed'
 scope={'__file__':str(ROOT/path),'__name__':'independent_review'};exec(compile(text,str(ROOT/path),'exec'),scope);modules.append(scope)
driver,sup=modules
assert sup['budgets'](False)==(123,3) and sup['budgets'](True)==(540,60)
assert driver['TARGETS'][SELECTOR]=='tests/objects'
assert 'time.monotonic() + 75' in (ROOT/paths[1]).read_text()
binary=ROOT/'output/ai/object-metadata-cleanup/metadata-cleanup-cost-race.test'
with tempfile.TemporaryDirectory(prefix='mapping-',dir=OUTPUT) as td:
 base=Path(td);fresh=base/'new-runtime'
 config=driver['configuration'](str(binary),SELECTOR,str(fresh))
 assert config['resources']==7 and config['test_timeout']=='6m' and config['cwd']==str(ROOT/'tests/objects') and not fresh.exists()
 negatives=[SELECTOR[1:],SELECTOR[:-1],'^TestObjectMetadataCleanup.*$', '^TestObjectMetadataCleanupProjectHistoryPlans$', SELECTOR+'|^TestExtra$', '^TestObjectMetadataCleanup(SkillsIndexPlans|ProjectHistoryPlans|TransferAndForeignKeyPlans)$']
 for selector in negatives:
  try:driver['configuration'](str(binary),selector,str(fresh))
  except ValueError:pass
  else:raise AssertionError('nonexact selector accepted')
 # Only absence/time are doubles. The actual ownership parser, expected-set/
 # actual-wait check and both private/runtime observations run unchanged.
 sup['time']=types.SimpleNamespace(monotonic=lambda:0,sleep=lambda _:None)
 cases=[('normal',NAMES,True,False,False,True),('reordered',NAMES[::-1],True,False,False,True),('missing',NAMES[:1],True,False,False,False),('extra',NAMES+['TestExtra'],True,False,False,False),('missing-wait',NAMES,False,False,False,False),('live-resource',NAMES,True,True,False,False),('private-remains',NAMES,True,False,True,False)]
 for index,(label,names,wait,live,private,expected) in enumerate(cases):
  directory=base/label;directory.mkdir();runtime=directory/'runtime';runtime.mkdir();calls=[]
  resources=[]
  for group,(tag,kinds) in enumerate([('agenteam.d05.objectfixture',['container','network']),('agenteam.d04.networkfixture',['container','network']),('agenteam.d03.fixture',['container','container','network'])],1):
   for kind in kinds:resources.append({'kind':kind,'id':format(len(resources)+1,'064x'),'label':tag,'nonce':format(group,'032x')})
  directories=[str(runtime/n) for n in ['object','outbound','pg']]
  record=directory/'owned.json';record.write_text(json.dumps({'kind':'work-owner-root-chain','resources':resources,'directories':directories}));record.chmod(0o600)
  if private:Path(directories[0]).mkdir()
  def absent(item,timeout):calls.append(item['id']);return not live
  sup['exact_absent']=absent
  logpath=directory/'test.log'
  with logpath.open('w+') as log:
   for name in names:log.write('=== RUN   '+name+'\n')
   if wait:log.write('D03 explicit test actual_wait pid=123 code=0 selector='+SELECTOR+'\n')
   result=sup['observe_root_chain'](directory,log,logpath,SELECTOR)
  assert result==expected,(label,result)
  assert len(calls)==14 and all(calls.count(item['id'])==2 for item in resources)
  output=logpath.read_text();assert output.count('ROOT private_observation=')==2 and output.count('ROOT runtime_observation=')==2
print('new exact mapping/input delta reversed byte-identically; budgets 123/3 and 540/60, TCP75 unchanged; config 1 positive/6 negative; 7 actual observer controls/14 resource observations each PASS; no resources started')

# New inputs must include all actual same-package helpers and exactly the SQL
# files embedded by these three cases, without altering any old input list.
old={'__file__':str(ROOT/paths[0]),'__name__':'old_cost_driver'}
exec(compile(subprocess.check_output(['git','show','365b2729:'+paths[0]],cwd=ROOT,text=True),'old_cost_driver','exec'),old)
assert driver['input_paths'](binary)==old['input_paths'](binary)
old_fresh=OUTPUT/'unused-old-configuration'
assert not old_fresh.exists()
for selector in old['TARGETS']:
 assert driver['configuration'](binary,selector,old_fresh)==old['configuration'](binary,selector,old_fresh)
expected_go=set((ROOT/'tests/objects').glob('*.go'))
sql=set()
for name in ('metadata_cleanup_cost_test.go','metadata_cleanup_skill_cost_test.go','metadata_cleanup_transfer_cost_test.go'):
 sql.update((ROOT/'tests/objects'/p).resolve() for p in re.findall(r'^//go:embed (testdata/\S+)$',(ROOT/'tests/objects'/name).read_text(),re.M))
assert len(sql)==3
expected=expected_go|sql
def complete(actual):
 return set(actual)==expected and all(p.is_file() for p in actual)
actual=driver['metadata_cost_inputs']()
assert complete(actual)
for missing in sql:
 assert not complete([p for p in actual if p!=missing])
assert not complete(actual[:-1])
assert not complete(actual+[ROOT/'AGENTS.md'])
print('actual Go helpers + 3 actual go:embed seeds exact; 5 bad closures rejected; all 10 old configurations/input lists unchanged')
