from pathlib import Path
import hashlib,json,re
B=Path('/workspace/scratch/project-initialization-convergence-author/pg-driver-v02/runs/new1');R=Path(__file__).parent
H=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();J=lambda n:json.loads((B/n).read_text())
assert H(B/'result.json')=='304b1605d9c2c7a3b4e789fd7ccb7cefdf7d048cb0422610f86911132733dd81'
r=J('result.json');cmd=J('command.json');before,after=J('input-before.json'),J('input-after.json');w=J('watchdog.json');cl=J('cleanup.json');res=J('observed-resources.json');tail=J('owned-tail-retirement.json');tcp=J('tcp-tail-observation.json');brief=J('brief.json')
for k in ['actual_wait_completed','double_cleanup','host_tcp_delta_double_clear','resource_topology_matches','selected_tests_passed','source_files_unchanged','top_watchdog_complete','verification_inputs_unchanged','watchdog_thread_joined']:assert r[k],k
assert r['driver_exit']==0 and r['forced_tail_actions']==r['monitor_errors']==r['adopted_waits']==0
assert cmd['exit']==0 and cmd['actual_wait_completed'] and cmd['watchdog_thread_joined']
assert before==after and before['accepted'] and not before['missing'] and not before['mismatches'] and not before['package_set_changes']
assert len(cl)==2 and len(res)==7
for c in cl:
 assert c['baseline_unchanged'] and not c['owned_processes'] and not c['remaining_new'] and not c['runtime_entries']
 assert set(c['exact_absent'])==set(res) and all(v['absent'] for v in c['exact_absent'].values())
assert tail['actual_owned_completion'] and not tail['remaining'] and not tail['actions']
assert tcp['double_delta_clear'] and tcp['signals_sent_for_tcp'] is False and len(tcp['scans'])>=2
assert all(not x['new_host_rows'] for x in tcp['scans'][-2:])
raw=(B/'raw.log').read_text();top='TestProjectInitializationConvergenceFacts'
assert re.findall(r'^=== RUN   ([^/\s]+)$',raw,re.M)==[top]
assert re.findall(r'^--- PASS: ([^/\s]+) ',raw,re.M)==[top]
subs=re.findall(r'^    --- PASS: '+top+r'/([^\s]+) ',raw,re.M);assert len(subs)==18
assert not re.search(r'--- FAIL:|^FAIL(?:\s|$)',raw,re.M)
assert w['complete'] and not w['active'] and not w['failures'] and w['budget_seconds']==120 and w['expected']==[top]
assert cmd['argv']==['sh','scripts/test-objects.sh','-run','^('+top+')$']
f=J('frozen-input.json');assert f['root_authorized_resources'] and f['driver_sha256']==H(B/'driver.py.txt')=='62895c8d30f5f35fbec65521ad0b145c74730d368f8e6bcd57102cb9a4ea0b5e'
base=J('process-baseline.json');z0={(int(pid),x['starttime']) for pid,x in base.items() if x['ppid']==1 and x['state']=='Z'};z1={(x['pid'],x['starttime']) for x in cl[-1]['pid1_zombies']};delta=[x for x in cl[-1]['pid1_zombies'] if (x['pid'],x['starttime']) not in z0]
assert len(z0)==449 and len(z1)==453 and len(delta)==4 and z0<=z1
assert {(x['pid'],x['starttime']) for x in delta}=={(x['pid'],x['starttime']) for x in brief['pid1_zombies']['delta']}
assert all(x['name']=='containerd-shim' and x['ppid']==1 and x['state']=='Z' for x in delta)
names=['brief.json','result.json','raw.log','command.json','frozen-input.json','driver.py.txt','input-before.json','input-after.json','watchdog.json','observed-processes.json','observed-resources.json','cleanup.json','owned-tail-retirement.json','adopted-waits.json','process-baseline.json','tcp-baseline.json','tcp-observed-delta.json','tcp-tail-observation.json','disk-preflight.json','disk-before-spawn.json','local-image-digests.json']
out={'status':'PASS_BOUNDED_AUTHOR_NEW1_EVIDENCE_REVIEW','top':top,'subcases':subs,'top_seconds':1.16,'fixture_seconds':cmd['seconds'],'direct_actual_wait':True,'adopted_wait_count':0,'observed_owned':r['observed_processes'],'resources_count':7,'resource_ids':sorted(res),'resource_double_clear':True,'owned_runtime_double_clear':True,'inputs_same':True,'supplementary_tcp_seconds':tcp['seconds'],'supplementary_tcp_double_clear':True,'TCP_complete_short_connection_trace':False,'daemon':{'baseline':len(z0),'final':len(z1),'difference':delta,'owned':False,'waited':False},'references':[{'path':str(B/n),'sha256':H(B/n)} for n in names],'limits':['New Facts only, no transaction/old compatibility or independent PG acceptance yet.','New fixture lawful SQL/registered Service/real Store scope; not Skills/Account create E2E.','Read-only review of completed original evidence; no rerun/resources by reviewer.']}
(R/'new1.json').write_text(json.dumps(out,indent=2)+'\n');print(json.dumps({'status':out['status'],'sha256':H(R/'new1.json'),'fixture_seconds':cmd['seconds'],'resources':7,'daemon_delta':len(delta)},indent=2))
