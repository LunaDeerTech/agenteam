from pathlib import Path
import hashlib,json,re,sys
R=Path(__file__).parent;B=Path('/workspace/scratch/project-initialization-convergence-author/pg-driver-v02/runs')
H=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
manifest=Path(sys.argv[1]);expected=json.loads(manifest.read_text());review=[];allids=set();alldeltas=set()
for x in expected:
 b=B/x['run'];j=lambda n:json.loads((b/n).read_text());assert H(b/'result.json')==x['result_sha256'];z=j('result.json');c=j('command.json');raw=(b/'raw.log').read_text();top=x['top']
 for k in ['actual_wait_completed','double_cleanup','resource_topology_matches','selected_tests_passed','source_files_unchanged','top_watchdog_complete','verification_inputs_unchanged','watchdog_thread_joined','host_tcp_delta_double_clear']:assert z[k],(x['run'],k)
 assert z['driver_exit']==0 and z['monitor_errors']==z['forced_tail_actions']==z['adopted_waits']==0
 assert c['actual_wait_completed'] and c['watchdog_thread_joined'] and c['exit']==0 and c['argv']==['sh','scripts/test-objects.sh','-run','^('+top+')$']
 before=j('input-before.json');after=j('input-after.json');assert before==after and before['accepted']
 assert re.findall(r'^=== RUN   ([^/\s]+)$',raw,re.M)==[top]
 ps=re.findall(r'^--- PASS: ([^/\s]+) \(([^)]+)s\)',raw,re.M);assert len(ps)==1 and ps[0][0]==top
 subs=re.findall(r'^    --- PASS: '+re.escape(top)+r'/([^\s]+) ',raw,re.M);assert len(subs)==x['sub_count']
 assert not re.search(r'--- FAIL:|^FAIL(?:\s|$)',raw,re.M)
 w=j('watchdog.json');assert w['complete'] and not w['active'] and not w['failures'] and w['budget_seconds']==120 and w['expected']==[top]
 resources=j('observed-resources.json');assert len(resources)==7
 assert not allids.intersection(resources);allids.update(resources)
 cleanup=j('cleanup.json');assert len(cleanup)==2
 for q in cleanup:
  assert q['baseline_unchanged'] and not q['remaining_new'] and not q['runtime_entries'] and not q['owned_processes']
  assert set(q['exact_absent'])==set(resources) and all(v['absent'] for v in q['exact_absent'].values())
 tail=j('owned-tail-retirement.json');assert tail['actual_owned_completion'] and not tail['remaining'] and not tail['actions']
 tcp=j('tcp-tail-observation.json');assert tcp['double_delta_clear'] and tcp['signals_sent_for_tcp'] is False and all(not q['new_host_rows'] for q in tcp['scans'][-2:])
 frozen=j('frozen-input.json');assert frozen['root_authorized_resources'] and frozen['driver_sha256']==H(b/'driver.py.txt')=='62895c8d30f5f35fbec65521ad0b145c74730d368f8e6bcd57102cb9a4ea0b5e'
 base=j('process-baseline.json');zb={(int(pid),v['starttime']) for pid,v in base.items() if v['ppid']==1 and v['state']=='Z'};ze={(v['pid'],v['starttime']) for v in cleanup[-1]['pid1_zombies']};delta=[v for v in cleanup[-1]['pid1_zombies'] if (v['pid'],v['starttime']) not in zb]
 assert zb<=ze and len(delta)==4 and all(v['name']=='containerd-shim' for v in delta)
 dz={(v['pid'],v['starttime']) for v in delta};assert not alldeltas.intersection(dz);alldeltas.update(dz)
 names=['result.json','raw.log','command.json','frozen-input.json','driver.py.txt','input-before.json','input-after.json','watchdog.json','observed-processes.json','observed-resources.json','cleanup.json','owned-tail-retirement.json','adopted-waits.json','process-baseline.json','tcp-baseline.json','tcp-observed-delta.json','tcp-tail-observation.json','disk-preflight.json','disk-before-spawn.json','local-image-digests.json']
 review.append({'run':x['run'],'top':top,'subcases':subs,'top_seconds':float(ps[0][1]),'fixture_seconds':c['seconds'],'direct_actual_wait':True,'adopted_wait_count':0,'owned_identities':z['observed_processes'],'resources':sorted(resources),'both_cleanups':True,'inputs_same':True,'supplementary_tcp_tail_seconds':tcp['seconds'],'daemon':{'baseline':len(zb),'final':len(ze),'delta':delta,'owned':False,'waited':False},'references':[{'path':str(b/n),'sha256':H(b/n)} for n in names]})
out={'status':'PASS_COMPLETED_AUTHOR_ROUNDS_ONLY','input':{'path':str(manifest),'sha256':H(manifest)},'check_script_sha256':H(Path(__file__)),'rounds':review,'distinct_resource_ids':len(allids),'distinct_new_daemon_identities':len(alldeltas),'complete_TCP_trace':False,'all_machine_zero':False,'resources_executed_by_reviewer':False,'limits':['Only listed completed author rounds. Independent A/B and remaining author groups are not implied.','New fixture uses lawful Project SQL and true registered service/Store, not Skills/Account create E2E.','Closed-store representative also has an expired token, not isolated error-priority proof.','Old compatibility fixture identity/Skills adapters remain old test sources, not new production capabilities.']}
p=R/(manifest.stem+'-result.json');p.write_text(json.dumps(out,indent=2)+'\n');print(json.dumps({'path':str(p),'sha256':H(p),'rounds':len(review),'resources':len(allids),'daemon_delta':len(alldeltas)},indent=2))
