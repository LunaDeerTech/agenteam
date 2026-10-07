from pathlib import Path
import json,hashlib,collections
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
B=Path('/workspace/scratch/summary-verification/project-model-owner-read-http-author-evidence01');R=Path('/workspace/scratch/project-model-owner-read-http-author/pg-driver-v03/runs/new-authority01');S=Path('/workspace/agenteam/api/openapi');sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest();read=lambda p:json.loads(Path(p).read_bytes())
assert sha(R/'result.json')=='28101be721bb516d6e90596be3627ad6f71c1407e06dd912a0b0a0cf7378315f'
r=read(R/'result.json');v=read(R/'verification-input.json');assert sha(R/'frozen-input.json')==v['frozen_input_sha256'];assert sha(R/'driver.py.txt')==v['driver_sha256']
assert all(r[k] for k in ['actual_wait_completed','double_cleanup','resource_topology_matches','selected_tests_passed','source_files_unchanged','top_watchdog_complete','verification_inputs_unchanged','watchdog_thread_joined']) and r['driver_exit']==0 and r['forced_tail_actions']==r['monitor_errors']==0 and r['observed_resources']==7
assert read(R/'input-before.json')==read(R/'input-after.json')
for c in read(R/'cleanup.json')[-2:]:assert c['baseline_unchanged'] and not c['owned_processes'] and not c['remaining_new'] and not c['runtime_entries'] and len(c['exact_absent'])==7 and all(x['absent'] for x in c['exact_absent'].values())
raw=(R/'raw.log').read_text();assert '--- PASS: TestModelProjectConfigurationHTTPAuthorityAndTerminal ' in raw and '--- FAIL:' not in raw
assert '--- PASS: TestModelProjectConfigurationHTTPAuthorityAndTerminal/real-COMMIT-ACK-drop/GET' in raw and '--- PASS: TestModelProjectConfigurationHTTPAuthorityAndTerminal/real-COMMIT-ACK-drop/HEAD' in raw
body=R/'safe-http-evidence/read-unknown.json';source=R/'safe-http-evidence/read-unknown-source.json';meta=read(source);wire=body.read_bytes();value=json.loads(wire)
assert sha(body)==meta['body_sha256'] and meta['schema_sha256']==sha(S/'project-models.json')
assert meta['method']=='GET' and meta['status']==503 and meta['content_type']=='application/problem+json' and int(meta['content_length'])==len(wire) and meta['run']==r['run']=='new-authority01' and meta['input_sha256']==v['frozen_input_sha256']
assert meta['response_headers']['X-Request-ID']==value['request_id'] and value['code']=='COMMIT_UNKNOWN' and value['commit_state']=='unknown' and value['status']==503
assert not set(value)&{'input','items','capabilities','attempt','attempt_id','cause_id'}
base='https://independent-project-model-evidence.invalid/';registry=Registry().with_resource(base+'common.json',Resource.from_contents(read(S/'common.json'),default_specification=DRAFT202012));Draft202012Validator({'$ref':base+'common.json#/components/schemas/Problem'},registry=registry,format_checker=FormatChecker()).validate(value)
result={'status':'AUTHOR_AUTHORITY_UNKNOWN_ORIGINAL_BYTES_SCHEMA_PASS','source_run':str(R),'author_result_sha256':sha(R/'result.json'),'body_sha256':sha(body),'source_sha256':sha(source),'actual_header_matches_body_RequestID':True,'common_schema_sha256':sha(S/'common.json'),'original_get_and_HEAD_ACKdrop_passes_in_raw':True,'new_nonowned_nonwaited_PID1':dict(collections.Counter(x['name'] for x in r['new_pid1_zombies_not_owned_or_joined'])),'limits':['real DB COMMIT ACK drop with controlled HTTP writer, not native client transport','HEAD zero body and underlying cause/attempt are author assertions bound by raw and candidate03, not independently replayed here','shared Problem lookup retry hint is unchanged; no new read lookup or automatic second read is claimed','no resource execution or active bounded round consumed']};(B/'authority-result.json').write_text(json.dumps(result,indent=2)+'\n');print('PASS original Unknown GET body/common Problem and actual RequestID; author GET+HEAD ACKdrop/actualwait/7resource doublecleanup bound')
