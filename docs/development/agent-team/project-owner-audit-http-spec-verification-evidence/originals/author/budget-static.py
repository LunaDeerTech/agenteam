"""Static scalar accounting only. Does not execute Go constructors or tests."""
import json,pathlib
P=pathlib.Path(__file__).parent
ID='01900000-0000-7000-8000-000000000001'; N='9223372036854775807'; D='sha256:'+'f'*64
enc=lambda x:json.dumps(x,ensure_ascii=False,separators=(',',':')).replace('&',r'\u0026').replace('<',r'\u003c').replace('>',r'\u003e').replace('\u2028',r'\u2028').replace('\u2029',r'\u2029').encode()
associations={k:ID for k in ('tool_id','execution_id','tool_call_id','operation_id','request_id','approval_id','runner_id','correlation_id','http_trace_id')}
actor={'kind':'service','service':'project-initialization','cause_ref':D,'project_id':ID}
# A conservative Cartesian envelope, deliberately not claimed to be one valid entry.
shell={'audit_id':ID,'created_at':'9999-12-31T23:59:59.999999Z','scope':'project','project_id':ID,'actor':actor,'action':'project.archive.completed','outcome':'success','resource':{'kind':'artifact_collection','id':ID},'metadata':None,'associations':associations,'summary':'Outbound access denied'}
shell_without_metadata=len(enc(shell))-4
cartesian_record=shell_without_metadata+4096
page_overhead=len(enc({'items':[],'next_cursor':'a'*8192}))
page_bound=page_overhead+200*cartesian_record+199
# Concrete legal structural candidate under ObjectMetadata/NewEntry: no runtime object operation.
metadata={'object_id':ID,'transfer_id':ID,'initiator_kind':'agent_run','initiator_id':ID,'initiator_execution_id':ID,'media_type':'&'*256,'byte_size':N,'sent_bytes':N,'phase':'revoked','reason':'storage_unavailable'}
representative=dict(shell,actor={'kind':'service','service':'object-maintenance','cause_ref':D,'project_id':ID},action='object.transfer.revoke',resource={'kind':'object_transfer','id':ID},metadata=metadata,summary='Audit event')
rows=[]
for i in range(201):
 r=dict(representative,audit_id=f'01900000-0000-7000-8000-{1000-i:012x}')
 rows.append(r)
report={'kind':'static scalar accounting, not Go execution nor maximality proof over every producer','project_actions_count':31,'max_page_items':200,'metadata_canonical_bound':4096,'max_cursor_safe_ascii_bound':8192,'fixed_shell_without_metadata_bytes':shell_without_metadata,'cartesian_record_upper_bytes':cartesian_record,'page_overhead_with_max_shape_cursor_bytes':page_overhead,'cartesian_page_upper_bytes':page_bound,'chosen_success_budget_bytes':1024*1024,'cartesian_bound_below_budget':page_bound<=1024*1024,'largest_proposed_escaped_structural_representative':{'action':representative['action'],'mime_decoded_ascii_bytes':256,'mime_encoded_content_bytes':1536,'metadata_bytes':len(enc(metadata)),'record_bytes':len(enc(representative)),'page200_with_shape_cursor_bytes':len(enc({'items':rows[:200],'next_cursor':'a'*8192}))},'required_future_Go_evidence':['All31 action maxima built with original formal constructors and new DTO','Concrete 201st sentinel also fully validated; first200 encode/decode unchanged','Actual maximum across31 constructed action extremals compared, not guessed','Real signed cursor example separate from8192B wire shape cap','Exact production JSON bytes measured and standard schema parsed','No claim of actual runtime/Object production or global RSS bound']}
(P/'budget-static.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,ensure_ascii=False))
