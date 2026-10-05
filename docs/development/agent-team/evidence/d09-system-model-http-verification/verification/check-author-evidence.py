import hashlib,json,re
from pathlib import Path
A=Path('/workspace/agenteam-system-http-author-zyt918wl')
P=Path(__file__).resolve().parent
expected={'author-report.md':'a5284098af5707420f0a61a8010f82e5a3f6f10ab69b28fbeb4d46480f6d9562','coverage-matrix.json':'d5693830cc61c88f4d36ea8b99d252d751589371c3c4b4451b98c1965ecefe21','source-final-check.json':'2bbaea9f047664d88ac2f0d85757118b971936ee39f48a8ba5e7f6e44065b019'}
for rel,want in expected.items():assert hashlib.sha256((A/rel).read_bytes()).hexdigest()==want
m=json.loads((A/'coverage-matrix.json').read_text());refs=[]
def visit(x):
    if isinstance(x,dict):
        if isinstance(x.get('path'),str) and 'sha256' in x:refs.append(x)
        for v in x.values():visit(v)
    elif isinstance(x,list):
        for v in x:visit(v)
visit(m)
for x in refs:assert hashlib.sha256(Path(x['path']).read_bytes()).hexdigest()==x['sha256'],x['path']
final={f['path']:f['sha256'] for f in json.loads((A/'fixture-input-02/manifest.json').read_text())['files']}
prod={f['path']:f['sha256'] for f in json.loads((A/'production-review-02/manifest.json').read_text())['files']}
groups=[];tops=set();subs=set()
for g in m['fixture_groups']:
    raw=Path(g['raw']['path']).read_text()
    rt=re.findall(r'^--- PASS: ([^ ]+)',raw,re.M);rs=re.findall(r'^\s+--- PASS: ([^ ]+/[^ ]+)',raw,re.M)
    assert set(rt)==set(g['top_pass']) and set(rs)==set(g['sub_pass']) and not re.search(r'--- (FAIL|SKIP):',raw),g['label']
    assert int(Path(g['exit_file']['path']).read_text())==0
    i=json.loads(Path(g['input']['path']).read_text())
    diffs=[k for k,v in final.items() if i['files'].get(k)!=v]
    assert not diffs or (g['label']=='new-configuration-01' and diffs==['tests/model/system_http_fixture_test.go','tests/model/system_http_credentials_test.go']),diffs
    first=json.loads(Path(g['cleanup_first']['path']).read_text());second=json.loads(Path(g['cleanup_double']['path']).read_text())
    assert not first['runtime_entries'] and not first['owned_processes'] and not second['runtime_entries'] and not second['owned_processes'] and second['source17_end_match']
    for key,expected_count in [('containers',4),('networks',3)]:
        check=second['second'][key]
        assert check['baseline_equal'] and len(check['owned'])==expected_count
        for observed in check['owned'].values():assert observed['exit']!=0 and ('no such' in observed['output'].lower() or 'not found' in observed['output'].lower())
    tops.update(rt);subs.update(rs);groups.append({'label':g['label'],'top_count':len(rt),'sub_count':len(rs),'exit':0,'different_from_final_files':diffs,'double_cleanup_exact_ids_absent':True})
assert len(tops)==20 and len(subs)==48
pure=[]
for g in m['pure_checks']:
    i=json.loads(Path(g['input']['path']).read_text());r=json.loads(Path(g['result']['path']).read_text())
    files=i.get('files',{})
    if isinstance(files,list):files={f['path']:f['sha256'] for f in files}
    assert r['exit']==g['exit']
    pure.append({'label':g['label'],'exit':g['exit'],'production_diff_from_final':[k for k,v in prod.items() if files.get(k)!=v],'raw':g['raw']})
result={'author_summary_hashes':expected,'all_referenced_files_hash_matched':len(refs),'groups':groups,'top_count':len(tops),'sub_count':len(subs),'skip_count':0,'pure':pure,'fixed_production_final':prod,'claim':'author evidence audited; not independent test execution'}
(P/'evidence/author-final-checked.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps({'hash_refs':len(refs),'groups':groups,'pure':[{k:v for k,v in x.items() if k!='raw'} for x in pure]},indent=2))
