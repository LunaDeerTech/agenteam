#!/usr/bin/env python3
"""Read-only source/configuration and explicit seven-resource observation doubles."""
import io,json,os,subprocess,tempfile,types
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
OWN=Path(__file__).resolve().parents[2]
OUTPUT=OWN/'output/ai/runner-control/tmp/default-failures-mapping';OUTPUT.mkdir(parents=True,exist_ok=True)
SELECTOR='^TestRunnerControlDefaultFailures$'
NAMES=['TestRunnerControlDefaultFailures']
paths=['.agent-state/work-owner-http/root_chain_driver.py','.agent-state/task-planning-recovery/pg_only_supervisor.py']
modules=[]
baselines=[]
for path in paths:
 text=(ROOT/path).read_text();old=subprocess.check_output(['git','show','04cfe262:'+path],cwd=ROOT,text=True)
 lines=text.splitlines(keepends=True);added=[i for i,l in enumerate(lines) if SELECTOR in l];assert len(added)==1
 assert ''.join(l for i,l in enumerate(lines) if i!=added[0])==old, 'old source changed'
 scope={'__file__':str(ROOT/path),'__name__':'independent_review'};exec(compile(text,str(ROOT/path),'exec'),scope);modules.append(scope)
 before={'__file__':str(ROOT/path),'__name__':'independent_review'};exec(compile(old,str(ROOT/path),'exec'),before);baselines.append(before)
driver,sup=modules
assert sup['budgets'](False)==(123,3) and sup['budgets'](True)==(540,60)
assert driver['TARGETS'][SELECTOR]=='tests/process'
assert 'time.monotonic() + 75' in (ROOT/paths[1]).read_text()
binary=ROOT/'output/ai/runner-control/runnercontrol-default-failures-race-3.test'
with tempfile.TemporaryDirectory(prefix='mapping-',dir=OUTPUT) as td:
 base=Path(td);fresh=base/'new-runtime'
 for old_selector in baselines[0]['TARGETS']:
  assert driver['configuration'](str(binary),old_selector,str(fresh))==baselines[0]['configuration'](str(binary),old_selector,str(fresh))
 assert driver['input_paths'](binary)==baselines[0]['input_paths'](binary)
 config=driver['configuration'](str(binary),SELECTOR,str(fresh))
 assert config['resources']==7 and config['test_timeout']=='6m' and config['cwd']==str(ROOT/'tests/process') and not fresh.exists()
 negatives=[SELECTOR[1:],SELECTOR[:-1],'^TestRunnerControl.*$', '^TestRunnerControlDefaultFailures$/^central_killed$', SELECTOR+'|^TestExtra$', '^TestRunnerControl(DefaultProcesses|DefaultFailures)$']
 for selector in negatives:
  try:driver['configuration'](str(binary),selector,str(fresh))
  except ValueError:pass
  else:raise AssertionError('nonexact selector accepted')
 # Only absence/time are doubles. The actual ownership parser, expected-set/
 # actual-wait check and both private/runtime observations run unchanged.
 sup['time']=types.SimpleNamespace(monotonic=lambda:0,sleep=lambda _:None)
 cases=[('normal',NAMES,True,False,False,True),('wrong-top',['TestRunnerControlDefaultProcesses'],True,False,False,False),('missing',[],True,False,False,False),('extra',NAMES+['TestExtra'],True,False,False,False),('missing-wait',NAMES,False,False,False,False),('live-resource',NAMES,True,True,False,False),('private-remains',NAMES,True,False,True,False)]
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
print('two exact additions reversed byte-identically; budgets 123/3 and 540/60, TCP75 unchanged; config 1 positive/6 negative; 7 actual observer controls/14 resource observations each PASS; no resources started')
