from pathlib import Path
import json,hashlib,difflib,re,subprocess
V=Path('/workspace/scratch/project-initialization-convergence-verification');A=Path('/workspace/scratch/project-initialization-convergence-author');R=Path(__file__).parent;REPO=Path('/workspace/agenteam');H=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();J=lambda p:json.loads(p.read_bytes());ref=lambda p:{'path':str(p),'sha256':H(p)}
assert H(V/'final05/result.json')=='b05a074bbe57ca0b72c6c35783b32dd4fe988ae2ec2ff5e8607b2e6381d30567'
ip=A/'installation01/result.json';assert H(ip)=='410683759be1395fdac3afc203e2df5eb26a659189145ffbcae713f43a48c22f';ins=J(ip)
for k in ['candidate','preflight','first_preflight_error','diff']:assert H(Path(ins[k]['path']))==ins[k]['sha256']
pre=J(Path(ins['preflight']['path']));inputs=[]
for k in ['frozen_integration_input','frozen_pure_input','frozen_overlay']:assert H(Path(pre[k]['path']))==pre[k]['sha256']
for k in ['frozen_integration_input','frozen_pure_input']:inputs.append(J(Path(pre[k]['path']))['files'])
assert len(pre['targets'])==5 and all(t['target_absent'] for t in pre['targets'])
m=J(Path(ins['candidate']['path']));assert len(m['files'])==5
for x,y in zip(m['files'],ins['files']):
 assert x['path']==y['path'] and x['sha256']==y['sha256'];p=REPO/x['path'];assert p.read_bytes()==Path(x['snapshot']).read_bytes() and H(p)==x['sha256']
sets=[]
for d in ins['directory_equivalence']:
 p=Path(d['directory']);names=sorted(x.name for x in p.iterdir() if x.is_file() and x.suffix=='.go');assert names==d['expected_effective_go_files']==d['after_go_files']
 for old in d['unchanged_existing_hashes']:
  q=p/old['name'];assert H(q)==old['sha256'];assert any(v.get(str(q))==old['sha256'] for v in inputs),str(q)
 sets.append({'directory':str(p),'effective_go_file_count':len(names),'existing_hashes_same':True})
assert sorted(x['effective_go_file_count'] for x in sets)==[13,16,39]
readme=REPO/'docs/development/backend/README.md';expected='394a856acf52cf0be6eaa89f3fbce9f680f834f1de0bc0ac073f3462a1094696';assert H(readme)==expected
after=readme.read_text();head=subprocess.check_output(['git','-C',str(REPO),'rev-parse','HEAD'],text=True).strip();before=subprocess.check_output(['git','-C',str(REPO),'show',head+':docs/development/backend/README.md']).decode()
start=after.index('## Project 初始化收敛授权库\n');end=after.index('## Project Owner 列表与详情只读 HTTP\n',start);block=after[start:end];assert after[:start]+after[end:]==before
assert len(block.strip().split('\n\n'))==4
links=[]
for target in re.findall(r'\[[^\]]*\]\(([^)]+)\)',block):
 assert not '://' in target;dest=(readme.parent/target.split('#',1)[0]).resolve();assert dest.exists();links.append({'target':target,'resolved':str(dest),'exists':True})
assert len(links)==1
alltargets=[t for t in re.findall(r'\[[^\]]*\]\(([^)]+)\)',after) if '://' not in t and not t.startswith('#')]
missing=[t for t in alltargets if not (readme.parent/t.split('#',1)[0]).resolve().exists()];assert not missing
assert '\r' not in after and after.endswith('\n') and all(l==l.rstrip() for l in after.splitlines())
(R/'README-before.md').write_text(before);(R/'README.md').write_text(after);(R/'README.diff').write_text(''.join(difflib.unified_diff(before.splitlines(True),after.splitlines(True),fromfile=head+':docs/development/backend/README.md',tofile='docs/development/backend/README.md')))
assert H(readme)==expected
sources=[{'path':x['path'],'sha256':x['sha256'],'snapshot':x['snapshot'],'installed_byte_identical':True} for x in m['files']]+[{'path':'docs/development/backend/README.md','sha256':expected,'snapshot':str(R/'README.md'),'kind':'three-paragraph capability/boundary documentation only'}]
(R/'sources.json').write_text(json.dumps({'files':sources,'count':6,'candidate':ins['candidate'],'technical_parent':ref(V/'final05/result.json')},indent=2)+'\n')
checks={'status':'PASS_STATIC_INSTALLATION_AND_README','technical_tests_reused':ref(V/'final05/result.json'),'installation':ref(ip),'all_five_bytes_identical':True,'directory_sets':sets,'README_sha256':expected,'README_before_head':head,'README_change':'only one new heading and three paragraphs inserted; all prior bytes unchanged','new_links':links,'all_relative_path_targets_exist_count':len(alltargets),'unchanged_fragments_reuse_prior_acceptance':True,'UTF8_LF_final_newline_trailing_space_pass':True,'no_test_or_resource_commands':True,'no_repository_or_git_mutation_by_reviewer':True,'references':[ref(Path(ins[k]['path'])) for k in ['preflight','first_preflight_error','diff']]}
(R/'checks.json').write_text(json.dumps(checks,indent=2)+'\n');print(json.dumps({'status':checks['status'],'source_count':6,'new_links':1,'all_relative_path_targets':len(alltargets),'README_before_head':head,'checks_sha256':H(R/'checks.json'),'sources_sha256':H(R/'sources.json')},indent=2))
