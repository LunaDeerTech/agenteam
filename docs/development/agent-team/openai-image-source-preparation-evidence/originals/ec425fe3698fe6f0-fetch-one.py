from pathlib import Path
import json,hashlib,http.client,ssl,signal,time,sys,datetime
ROOT=Path('/workspace/scratch/architecture-image-pinned-source01')
COMMIT='becc1d20eed83c1b8d85e15dc131a372d9dc7813'
TREE='f1b9a07a4b1f2d908bcb08d9cac4449f93616933'
SOURCES=['src/openai/types/image_generate_params.py','src/openai/types/images_response.py','src/openai/types/image.py','src/openai/resources/images.py']
ALLOW={'tree':('api.github.com','/repos/openai/openai-python/git/trees/'+TREE+'?recursive=1')}
ALLOW.update({'source'+str(i+1):('raw.githubusercontent.com','/openai/openai-python/'+COMMIT+'/'+p) for i,p in enumerate(SOURCES)})
slug=sys.argv[1]
assert slug in ALLOW
ledger_path=ROOT/'requests.json'
ledger=json.loads(ledger_path.read_text())
assert not ledger['stopped_on_failure'],'Prior failure requires STOP'
assert len(ledger['attempts'])<6
assert slug not in [r['slug'] for r in ledger['attempts']],'No retry'
assert slug==list(ALLOW)[len(ledger['attempts'])],'Sequential closed plan only'
host,path=ALLOW[slug]
entry={'ordinal':len(ledger['attempts'])+1,'slug':slug,'method':'GET','url':'https://'+host+path,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'total_deadline_seconds':30,'body_limit_bytes':2097152,'state':'STARTED'}
ledger['attempts'].append(entry)
ledger_path.write_text(json.dumps(ledger,indent=2)+'\n')
body_path=ROOT/(slug+'.body')
headers_path=ROOT/(slug+'.headers.json')
result_path=ROOT/(slug+'.result.json')
start=time.monotonic()
conn=None
status=None
headers=[]
count=0
failure=None
complete=False
def expired(sig,frame):
    raise TimeoutError('total_request_deadline_30s')
signal.signal(signal.SIGALRM,expired)
signal.setitimer(signal.ITIMER_REAL,30)
try:
    # Direct public HTTPS; no environment proxy, credential, netrc, cookie or SDK access.
    conn=http.client.HTTPSConnection(host,timeout=30,context=ssl.create_default_context())
    conn.request('GET',path,headers={'User-Agent':'agenteam-pinned-source-read/1','Accept':'application/vnd.github+json' if slug=='tree' else 'text/plain','Accept-Encoding':'identity','Connection':'close'})
    response=conn.getresponse()
    status=response.status
    headers=response.getheaders()
    headers_path.write_text(json.dumps({'status':status,'reason':response.reason,'http_version':response.version,'headers_in_observed_order':headers,'representation':'HTTP library header values; not packet capture'},indent=2)+'\n')
    lengths=response.getheader('Content-Length')
    if lengths is not None and (not lengths.isdecimal() or int(lengths)>2097152):
        raise ValueError('response_content_length_invalid_or_above_2MiB')
    enc=response.getheader('Content-Encoding')
    if enc not in (None,'identity'):
        raise ValueError('unexpected_content_encoding')
    with body_path.open('xb') as target:
        while count<2097152:
            chunk=response.read(min(65536,2097152-count))
            if not chunk:
                complete=True
                break
            target.write(chunk)
            count+=len(chunk)
        if count==2097152:
            complete=response.length==0
        if not complete:
            raise ValueError('response_cap_reached_without_complete_EOF')
    if not 200<=status<300:
        raise ValueError('HTTP_failure_'+str(status))
except Exception as error:
    failure={'type':type(error).__name__,'message':str(error)}
finally:
    if conn is not None:
        conn.close()
    signal.setitimer(signal.ITIMER_REAL,0)
elapsed=time.monotonic()-start
if not body_path.exists():
    body_path.write_bytes(b'')
if not headers_path.exists():
    headers_path.write_text(json.dumps({'status':status,'headers_in_observed_order':headers,'response_received':False},indent=2)+'\n')
body=body_path.read_bytes()
receipt={**entry,'state':'PASS' if failure is None else 'FAIL_STOP','status':status,'response_complete':complete,'body_bytes':len(body),'body_sha256':hashlib.sha256(body).hexdigest(),'git_blob_sha1':hashlib.sha1(b'blob '+str(len(body)).encode()+b'\0'+body).hexdigest() if complete and failure is None and slug!='tree' else None,'elapsed_seconds':elapsed,'connection_closed':True,'failure':failure,'redirect_followed':False,'retry_count':0,'response_paths':[str(body_path),str(headers_path)]}
result_path.write_text(json.dumps(receipt,indent=2)+'\n')
ledger['attempts'][-1]=receipt
ledger['stopped_on_failure']=failure is not None
ledger_path.write_text(json.dumps(ledger,indent=2)+'\n')
print(json.dumps({'slug':slug,'state':receipt['state'],'status':status,'bytes':len(body),'sha256':receipt['body_sha256'],'elapsed_seconds':elapsed,'failure':failure}))
sys.exit(0 if failure is None else 1)
