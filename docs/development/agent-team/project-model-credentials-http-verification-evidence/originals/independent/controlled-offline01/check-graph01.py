import pathlib,json,hashlib
D=pathlib.Path('/workspace/scratch/project-owner-credential-http-verification/controlled-offline01')
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
def ref(p):return {'path':str(p),'sha256':sha(p)}
cl=json.loads((D/'closure02.json').read_text());par=json.loads(pathlib.Path(cl['parent_input']['path']).read_text());ov=json.loads(pathlib.Path(cl['overlay']['path']).read_text())['Replace'];expected=dict(par['files']);expected.update(cl['files'])
# Test-expanded package GoFiles already include the actually compiled test files.
# TestGoFiles on unrelated dependency packages describe their unused own tests.
fields=['GoFiles','CgoFiles','CFiles','CXXFiles','MFiles','HFiles','FFiles','SFiles','SwigFiles','SwigCXXFiles','SysoFiles','EmbedFiles']
allgenerated={};out={}
for variant in ['normal','race']:
 p=D/'runs'/('graph-'+variant+'01')/'stdout.log';s=p.read_text();i=0;rows=[];dec=json.JSONDecoder()
 while i<len(s):
  while i<len(s) and s[i].isspace():i+=1
  if i==len(s):break
  r,i=dec.raw_decode(s,i);rows.append(r)
 actual={};generated={};direct={}
 for r in rows:
  assert not r.get('Error') and not r.get('DepsErrors'),r.get('ImportPath')
  assert '/tests/model' not in r.get('ImportPath','')
  for field in fields:
   for name in r.get(field,[]):
    v=str(pathlib.Path(r['Dir'])/name);v=ov.get(v,v)
    if not v:continue
    actual[v]=sha(v)
    if r.get('ImportPath','').endswith('.test'):
     body=pathlib.Path(v).read_text();assert '_test.TestMain(' not in body and '_xtest.TestMain(' not in body;assert 'm.Run()' in body
     assert 'TestIndependentProjectCredentialHTTPControls' in body
     generated[v]=actual[v]
  if r.get('ImportPath','')=='github.com/LunaDeerTech/agenteam/internal/central/model':
   direct={k:r.get(k,[]) for k in ['GoFiles','CgoFiles','TestGoFiles','XTestGoFiles','EmbedFiles','TestEmbedFiles','XTestEmbedFiles']}
 extra={p:h for p,h in actual.items() if expected.get(p)!=h and p not in generated}
 assert not extra,'extra actual inputs: '+str(len(extra))
 allgenerated.update(generated)
 out[variant]={'raw':ref(p),'result':ref(p.parent/'result.json'),'package_count':len(rows),'actual_file_count':len(actual),'extra':extra,'generated_testmain':generated,'user_TestMain':False,'direct_package_files':direct,'file_digest':hashlib.sha256(json.dumps(actual,sort_keys=True).encode()).hexdigest()}
report=D/'actual-probe-graph01.json';report.write_text(json.dumps({'checker':ref(__file__),'parent':cl['parent_input'],'overlay':cl['overlay'],'probe':ref(D/'independent_credential_controls_test.go'),'variants':out,'generated_files':allgenerated,'excluded':'integration tags/tests/model; no user TestMain; no test bodies executed'},indent=2)+'\n')
cl['actual_probe_graph']=ref(report);cl['files'].update(allgenerated);cl['files'][str(report)]=sha(report);cl['files'][str(pathlib.Path(__file__))]=sha(__file__)
cp=D/'closure03.json';cp.write_text(json.dumps(cl,indent=2)+'\n')
plan=json.loads((D/'prep-plan02.json').read_text());plan['closure']=ref(cp);plan['phase']='Preparation compile/vet only; exact listing in subsequent binary-bound plan; no test body/resource';go='/workspace/toolchains/go1.27.1/bin/go';(D/'bin').mkdir(exist_ok=True)
plan['commands']=[{'label':'compile-normal01','argv':[go,'test','-c','-vet=off','-o',str(D/'bin/controls-normal01.test'),'./internal/central/model'],'binary_output':str(D/'bin/controls-normal01.test')},{'label':'compile-race01','argv':[go,'test','-race','-c','-vet=off','-o',str(D/'bin/controls-race01.test'),'./internal/central/model'],'binary_output':str(D/'bin/controls-race01.test')},{'label':'vet-race01','argv':[go,'vet','-race','./internal/central/model']}]
pp=D/'compile-plan03.json';pp.write_text(json.dumps(plan,indent=2)+'\n')
for p in [report,cp,pp]:print(str(p),sha(p))
print('graphs',[(k,v['package_count'],v['actual_file_count'],len(v['extra'])) for k,v in out.items()])
