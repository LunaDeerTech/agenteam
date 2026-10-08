from pathlib import Path
import ctypes, datetime, hashlib, json, os, signal, subprocess, sys, time, traceback

ROOT = Path(__file__).resolve().parent
REPO = Path('/workspace/agenteam')
AUTHOR = Path('/workspace/scratch/platform-embedding-resolution-author-v1/pure-check01')
GO = '/workspace/toolchains/go1.27.1/bin/go'
SELECTOR = '^TestIndependentPlatformEmbeddingPolicyComplement$'

def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()

def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()

def save(name, value):
    with (ROOT / name).open('x') as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write('\n')

def identity(pid):
    try:
        fields = Path(f'/proc/{pid}/stat').read_text().rsplit(') ', 1)[1].split()
    except FileNotFoundError:
        return None
    return {'pid':pid,'state':fields[0],'ppid':int(fields[1]),'pgrp':int(fields[2]),'session':int(fields[3]),'starttime':int(fields[19])}

def owned_group(pgrp):
    found, unknown = [], []
    for path in Path('/proc').iterdir():
        if not path.name.isdigit():
            continue
        try:
            value = identity(int(path.name))
        except PermissionError:
            unknown.append(int(path.name))
            continue
        if value and value['pgrp'] == pgrp and value['session'] == pgrp:
            found.append(value)
    return found, unknown

def input_check(records):
    changed=[]
    for row in records:
        path=REPO / row['path']
        try:
            raw=path.read_bytes()
        except FileNotFoundError:
            changed.append({'path':row['path'],'reason':'missing'})
            continue
        h=hashlib.sha256(raw).hexdigest()
        if len(raw)!=row['bytes'] or h!=row['sha256']:
            changed.append({'path':row['path'],'actual_bytes':len(raw),'actual_sha256':h,'expected_sha256':row['sha256']})
    return changed

start=time.monotonic()
child=None
owner=None
code=None
waited=False
forced=[]
adopted=[]
observed={}
scans=[]
exception=None
inputs_same=False
probe_same=False
subreaper=False
records=[]
safe_env={
 'GOTOOLCHAIN':'local','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOTELEMETRY':'off','GOENV':'off',
 'GOFLAGS':'-mod=readonly -p=1','GOCACHE':'/workspace/.cache/go-build','GOMODCACHE':'/workspace/go/pkg/mod',
 'GOMAXPROCS':'2','TMPDIR':str(ROOT/'tmp'),
}
argv=[GO,'test','-mod=readonly','-p=1','-count=1','-timeout=40s','-overlay='+str(ROOT/'overlay.json'),'-run',SELECTOR,'-v','./internal/central/model']
probe_paths=[ROOT/'independent_test.go',ROOT/'overlay.json',ROOT/'run.py']
probe_before={str(p):digest(p) for p in probe_paths}
save('command-before.json',{'argv':argv,'cwd':str(REPO),'safe_env':safe_env,'started_utc':utc(),'wrapper_identity':identity(os.getpid()),'outer_limit_including_retirement_seconds':45,'child_termination_deadline_seconds':40,'go_test_timeout_seconds':40,'probe_input_sha256':probe_before,'go_binary_sha256':digest(GO),'author_input_manifest_sha256':digest(AUTHOR/'inputs-before.json'),'cache_ownership':'Root handed over exclusive Go/cache window; reuse accepted cache without copying; own TMPDIR and output.'})
try:
    assert digest(AUTHOR/'inputs-before.json')=='dadf09108c5c0d0d9dee56d215b6c2975abd01987cd04ef132a5db5be17040de'
    records=json.loads((AUTHOR/'inputs-before.json').read_text())['files']
    assert len(records)==352
    changed=input_check(records)
    save('inputs-before-check.json',{'count':len(records),'changed':changed,'same':not changed,'time':utc()})
    assert not changed, 'frozen input drift before execution'
    assert not (REPO/'internal/central/model/platform_embedding_independent_test.go').exists()
    libc=ctypes.CDLL(None,use_errno=True)
    if libc.prctl(36,1,0,0,0)!=0:
        raise OSError(ctypes.get_errno(),'PR_SET_CHILD_SUBREAPER failed')
    subreaper=True
    env=os.environ.copy();env.update(safe_env)
    env['PATH']='/workspace/toolchains/go1.27.1/bin:'+env.get('PATH','')
    with (ROOT/'stdout.raw').open('xb') as stdout,(ROOT/'stderr.raw').open('xb') as stderr:
        child=subprocess.Popen(argv,cwd=REPO,env=env,stdin=subprocess.DEVNULL,stdout=stdout,stderr=stderr,start_new_session=True)
        owner=identity(child.pid)
        save('spawn.json',{'time':utc(),'direct_pid':child.pid,'identity':owner,'argv':argv,'subreaper':subreaper})
        assert owner and owner['pgrp']==child.pid and owner['session']==child.pid
        while child.poll() is None and time.monotonic()-start < 40:
            members,_=owned_group(child.pid)
            for item in members:
                observed[(item['pid'],item['starttime'])]=item
            time.sleep(0.03)
        if child.poll() is not None:
            code=child.wait();waited=True
        else:
            forced.append({'signal':'SIGTERM','time':utc(),'reason':'40-second command deadline; exact owned process group'})
            members,_=owned_group(child.pid)
            if members: os.killpg(child.pid,signal.SIGTERM)
except BaseException as error:
    exception={'type':type(error).__name__,'message':str(error),'traceback':traceback.format_exc()}
finally:
    if child is not None and not waited:
        try:
            code=child.wait(timeout=max(0.01,42-(time.monotonic()-start)));waited=True
        except subprocess.TimeoutExpired:
            members,_=owned_group(child.pid)
            if members:
                os.killpg(child.pid,signal.SIGKILL)
                forced.append({'signal':'SIGKILL','time':utc(),'reason':'exact owned command group did not retire after TERM'})
            try:
                code=child.wait(timeout=max(0.01,44-(time.monotonic()-start)));waited=True
            except subprocess.TimeoutExpired:
                forced.append({'error':'direct actual wait incomplete','time':utc()})
    save('actual-wait.json',{'time':utc(),'direct_identity':owner,'actual_direct_wait_completed':waited,'exit_code':code,'exception':exception,'forced':forced})
    if child is not None:
        retirement_term_at = None
        retirement_kill_sent = False
        while time.monotonic()-start < 44:
            while True:
                try:
                    pid,status=os.waitpid(-1,os.WNOHANG)
                except ChildProcessError:
                    break
                if not pid: break
                adopted.append({'pid':pid,'status':status,'exit_code':os.waitstatus_to_exitcode(status),'actual_wait':True,'time':utc()})
            members,unknown=owned_group(child.pid)
            if not members: break
            running = [item for item in members if item['state'] != 'Z']
            if running and retirement_term_at is None:
                os.killpg(child.pid,signal.SIGTERM)
                retirement_term_at = time.monotonic()
                forced.append({'signal':'SIGTERM','time':utc(),'reason':'owned descendants outlived direct actual wait'})
            elif running and not retirement_kill_sent and (time.monotonic()-retirement_term_at > 1 or time.monotonic()-start > 43):
                os.killpg(child.pid,signal.SIGKILL)
                retirement_kill_sent = True
                forced.append({'signal':'SIGKILL','time':utc(),'reason':'remaining exact owned descendants'})
            time.sleep(0.03)
        for index in range(2):
            if index: time.sleep(0.1)
            members,unknown=owned_group(child.pid)
            current=identity(child.pid)
            scans.append({'time':utc(),'owned_group':members,'proc_permission_unknown_count':len(unknown),'original_direct_absent':bool(owner) and (current is None or current['starttime']!=owner['starttime'])})
    save('retirement.json',{'actual_direct_wait':waited,'adopted_waits':adopted,'observed_owned_identities':list(observed.values()),'double_observations':scans,'watchdog':'No asynchronous thread/process created; synchronous supervisor reached terminal path.','scope':'Only this new process group/session; no whole-host cleanup claim.'})
    changed_after=input_check(records) if records else [{'reason':'no verified baseline'}]
    inputs_same=bool(records) and not changed_after
    probe_after={str(p):digest(p) for p in probe_paths}
    probe_same=probe_after==probe_before
    save('inputs-after-check.json',{'count':len(records),'changed':changed_after,'same':inputs_same,'probe_same':probe_same,'probe_input_sha256':probe_after,'time':utc()})
    elapsed=time.monotonic()-start
    clean=len(scans)==2 and all(not x['owned_group'] and x['original_direct_absent'] and x['proc_permission_unknown_count']==0 for x in scans)
    raws=[]
    for name in ['stdout.raw','stderr.raw']:
        p=ROOT/name
        if p.exists(): raws.append({'path':str(p),'bytes':p.stat().st_size,'sha256':digest(p)})
    accepted=waited and code==0 and exception is None and not forced and clean and inputs_same and probe_same and elapsed<45
    result={'accepted':accepted,'state':'STOP_PASS' if accepted else 'STOP_FAIL','completed_utc':utc(),'actual_exit':code,'actual_direct_wait_completed':waited,'adopted_waits':adopted,'outer_seconds_including_retirement':elapsed,'owned_double_empty':clean,'inputs352_same':inputs_same,'probe_inputs_same':probe_same,'forced':forced,'exception':exception,'raw':raws,'no_external_resources_started':True,'scope':'One independent pure top only; no PG or runtime authority/transaction/replay/Unknown proof.'}
    save('result.json',result)
    print(json.dumps(result,ensure_ascii=False),flush=True)
    sys.exit(0 if accepted else 1)
