import datetime,hashlib,json,os,subprocess,time
from pathlib import Path
P=Path(__file__).resolve().parent
E=P/'evidence/dynamic01';E.mkdir()
ready_bytes=(P/'evidence/ready-input.json').read_bytes()
assert hashlib.sha256(ready_bytes).hexdigest()=='edc9a5ab945cb9ae241ddf4269e0964a81abc34f09d38d98234de168357eaaa4'
ready=json.loads(ready_bytes)
for f in ready['files']:
    assert hashlib.sha256((P/'tree'/f['path']).read_bytes()).hexdigest()==f['sha256'],f['path']
probe=(P/'tree/tests/model/independent_structured_wire_test.go').read_bytes()
assert hashlib.sha256(probe).hexdigest()==ready['probe']['sha256']
assert not list((P/'runtime').iterdir())
(P/'docker-config').mkdir(mode=0o700)
assert not list((P/'docker-config').iterdir())
selected=ready['planned_environment']
env=os.environ.copy()
for key in ['DOCKER_CONTEXT','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH','AGENTEAM_PG_FIXTURE','AGENTEAM_OUTBOUND_FIXTURE','AGENTEAM_OBJECT_FIXTURE']:
    env.pop(key,None)
env.update(selected)
def now():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def save(name,data):
    (E/name).write_text(json.dumps(data,indent=2,sort_keys=True)+'\n')
def docker(args):
    return subprocess.run(['/usr/local/bin/docker','--host','unix:///var/run/docker.sock','--config',str(P/'docker-config'),*args],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=20)
formats={
 'containers':'{"id":{{json .Id}},"name":{{json .Name}},"labels":{{json .Config.Labels}},"running":{{json .State.Running}}}',
 'networks':'{"id":{{json .Id}},"name":{{json .Name}},"labels":{{json .Labels}}}'
}
def inspect(kind,ident):return docker((['inspect'] if kind=='containers' else ['network','inspect'])+['--format',formats[kind],ident])
def snapshot():
    result={}
    for kind,args in [('containers',['ps','-aq','--no-trunc']),('networks',['network','ls','-q','--no-trunc'])]:
        listed=docker(args)
        if listed.returncode:raise RuntimeError('Docker inventory failed: '+listed.stderr)
        rows=[]
        for ident in listed.stdout.split():
            seen=inspect(kind,ident)
            if seen.returncode:
                if 'no such' in seen.stderr.lower() or 'not found' in seen.stderr.lower():continue
                raise RuntimeError('Docker inspect failed: '+seen.stderr)
            row=json.loads(seen.stdout);row['labels']=row['labels'] or {};rows.append(row)
        result[kind]=sorted(rows,key=lambda x:x['id'])
    return result
baseline=snapshot()
assert len(baseline['containers'])==2 and len(baseline['networks'])==4,baseline
save('baseline.json',{'time':now(),'resources':baseline})
baseids={k:{x['id'] for x in v} for k,v in baseline.items()}
owned={'containers':{},'networks':{}}
command={'started_utc':now(),'argv':ready['planned_driver_argv'],'cwd':ready['cwd'],'environment':selected,'ready_input_sha256':hashlib.sha256(ready_bytes).hexdigest(),'production_manifest_sha256':ready['production_manifest_sha256'],'probe_sha256':ready['probe']['sha256'],'scope':'authorized unique fixture; fixed 2 tops/no subtests; no new network fault method','go_version':subprocess.check_output([selected['AGENTEAM_GO'],'version'],env=env,text=True).strip()}
save('command.json',command)
with (E/'raw.log').open('wb') as log:
    process=subprocess.Popen(command['argv'],cwd=command['cwd'],env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
    save('running.json',dict(command,pid=process.pid,pgid=process.pid))
    print('DRIVER_STARTED pid='+str(process.pid)+' log='+str(E/'raw.log'),flush=True)
    while process.poll() is None:
        snap=snapshot();changed=False
        for kind,rows in snap.items():
            for row in rows:
                if row['id'] in baseids[kind]:continue
                if not row['name'].lstrip('/').startswith(('agenteam-d03-','agenteam-d04-net-','agenteam-d05-object-')) or not row['labels']:
                    raise RuntimeError('Unexpected concurrent resource; do not mutate it')
                before=owned[kind].get(row['id'])
                if before is None or before['resource']!=row:
                    owned[kind][row['id']]={'first_seen_utc':before['first_seen_utc'] if before else now(),'last_changed_utc':now(),'resource':row};changed=True
        if changed:save('owned-live.json',owned)
        time.sleep(0.1)
    status=process.wait()
command.update(exit_code=status,finished_utc=now(),pid=process.pid,pgid=process.pid,raw_sha256=hashlib.sha256((E/'raw.log').read_bytes()).hexdigest())
save('result.json',command)
print('DRIVER_FINISHED exit='+str(status),flush=True)
def own_processes():
    raw=subprocess.check_output(['ps','-eo','pid=,ppid=,pgid=,args='],text=True)
    rows=[]
    for line in raw.splitlines():
        parts=line.strip().split(None,3)
        if len(parts)>=4 and int(parts[2])==process.pid:rows.append({'pid':int(parts[0]),'ppid':int(parts[1]),'pgid':int(parts[2]),'args':parts[3]})
    return rows
for number in [1,2]:
    current=snapshot();exact={}
    for kind,objects in owned.items():
        exact[kind]=[]
        currentids={x['id'] for x in current[kind]}
        for ident,item in objects.items():
            result=inspect(kind,ident)
            absent=result.returncode!=0 and ident not in currentids and ('no such' in result.stderr.lower() or 'not found' in result.stderr.lower())
            exact[kind].append({'id':ident,'name':item['resource']['name'],'labels':item['resource']['labels'],'absent':absent,'inspect_exit':result.returncode,'inspect_stderr':result.stderr.strip()})
    cleanup={'time':now(),'actual_resources':current,'baseline_unchanged':current==baseline,'exact_owned':exact,'owned_processes':own_processes(),'runtime_entries':[str(x.relative_to(P/'runtime')) for x in (P/'runtime').rglob('*')],'owned_counts':{k:len(v) for k,v in owned.items()}}
    cleanup['clean']=cleanup['owned_counts']=={'containers':4,'networks':3} and cleanup['baseline_unchanged'] and all(x['absent'] for xs in exact.values() for x in xs) and not cleanup['owned_processes'] and not cleanup['runtime_entries']
    save('cleanup0'+str(number)+'.json',cleanup)
    print('CLEANUP'+str(number)+' clean='+str(cleanup['clean'])+' counts='+str(cleanup['owned_counts']),flush=True)
    if number==1:time.sleep(1)
source_checks=[]
for f in ready['files']:
    actual=hashlib.sha256((P/'tree'/f['path']).read_bytes()).hexdigest();source_checks.append({'path':f['path'],'sha256':actual,'matched':actual==f['sha256']})
save('source-final.json',{'files':source_checks,'all_matched':all(x['matched'] for x in source_checks),'probe_sha256':hashlib.sha256((P/'tree/tests/model/independent_structured_wire_test.go').read_bytes()).hexdigest()})
print('RESULT '+str(E/'result.json'),flush=True)
raise SystemExit(status if status else (0 if cleanup['clean'] and all(x['matched'] for x in source_checks) else 2))
