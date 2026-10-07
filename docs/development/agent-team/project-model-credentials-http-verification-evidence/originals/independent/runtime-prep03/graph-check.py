from pathlib import Path
import hashlib,json,re

ROOT=Path(__file__).parent
OLD=ROOT.with_name('runtime-prep01')
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def bind(p):return {'path':str(p),'sha256':sha(p)}
def read(p):return json.loads(Path(p).read_bytes())
def put(n,v):p=ROOT/n;p.write_text(json.dumps(v,indent=2)+'\n');return bind(p)
def parent(row):
    assert sha(row['path'])==row['sha256']
    d=read(row['path']);f=parent(d['base_closure']) if 'base_closure' in d else {}
    f.update({p:(h['sha256'] if isinstance(h,dict) else h) for p,h in d['files'].items()});return f
closure=read(ROOT/'closure-pregraph01.json')
known=parent(closure['parent_input']);known.update(closure['files'])
overlay=read(ROOT/'overlay04.json')['Replace']
raw=ROOT/'runs/graph-race03/stdout.log'
s=raw.read_text();decoder=json.JSONDecoder();i=0;packages=[];files={};generated={};user_main=False
fields=['GoFiles','CgoFiles','CFiles','CXXFiles','MFiles','HFiles','FFiles','SFiles','SwigFiles','SwigCXXFiles','SysoFiles','EmbedFiles']
while i<len(s):
    while i<len(s) and s[i].isspace():i+=1
    if i==len(s):break
    p,i=decoder.raw_decode(s,i);packages.append(p)
    assert not p.get('Error') and not p.get('DepsErrors')
    for field in fields:
        for name in p.get(field,[]):
            virtual=str(Path(p['Dir'])/name);effective=overlay.get(virtual,virtual)
            if not effective:continue
            files[effective]=sha(effective)
            if p['ImportPath'].endswith('/tests/model.test'):
                generated[effective]=files[effective]
                assert not re.search(r'\b_test\w*\.TestMain\(',Path(effective).read_text())
            elif 'tests/model_test [' in p['ImportPath'] and field=='GoFiles':
                user_main |= bool(re.search(r'func\s+TestMain\s*\(',Path(effective).read_text()))
extra={p:h for p,h in files.items() if known.get(p)!=h and p not in generated}
assert not extra and not user_main
assert len(packages)==398 and len(files)==2526 and len(generated)==1
previous=read(OLD/'actual-graph01.json')
assert generated==previous['generated_testmain']
actual=put('actual-graph03.json',{'raw':bind(raw),'result':bind(ROOT/'runs/graph-race03/result.json'),'package_count':len(packages),'actual_file_count':len(files),'generated_testmain':generated,'user_TestMain':False,'extra':extra,'parent':closure['parent_input'],'overlay':bind(ROOT/'overlay04.json'),'file_digest':hashlib.sha256(json.dumps(files,sort_keys=True,separators=(',',':')).encode()).hexdigest(),'previous_actual_graph':bind(OLD/'actual-graph01.json'),'delta':'Only private A fresh bounded transaction context and assertion order; candidate03 and B unchanged; same build file count/generated TestMain.'})
closure['files'].update(generated);closure['files'][actual['path']]=actual['sha256'];closure['actual_probe_graph']=actual
clref=put('closure-compile01.json',closure)
plan=read(OLD/'compile-plan03.json');plan['closure']=clref;plan['output_root']=str(ROOT/'runs');plan['environment']['GOFLAGS']='-mod=readonly -p=1 -overlay='+str(ROOT/'overlay04.json');plan['environment']['TMPDIR']=str(ROOT/'tmp')
for cmd in plan['commands']:
    cmd['label']=cmd['label'].replace('01','03')
    if 'binary_output' in cmd:
        new=str(ROOT/'bin/independent-race03.test');cmd['argv']=[new if x==cmd['binary_output'] else x for x in cmd['argv']];cmd['binary_output']=new
ref=put('compile-plan02.json',plan)
print(json.dumps({'actual_graph':actual,'compile_plan':ref,'extra':extra,'generated':generated},indent=2))
