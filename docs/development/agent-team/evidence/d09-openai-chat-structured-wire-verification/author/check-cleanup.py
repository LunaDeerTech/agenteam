import json,os,pathlib,re,sys,time
from resources import cleanup_observation
root=pathlib.Path(__file__).resolve().parent;label=sys.argv[1];assert re.fullmatch('[a-z0-9-]+',label)
path=root/'evidence'/(label+'-cleanup.json');assert not path.exists()
env=dict(os.environ)
for key in ['DOCKER_CONTEXT','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH']:env.pop(key,None)
env.update(DOCKER_HOST='unix:///var/run/docker.sock',DOCKER_CONFIG=str(root/'docker-config'))
checks=[cleanup_observation(label,env)];time.sleep(1);checks.append(cleanup_observation(label,env))
path.write_text(json.dumps({'observations':checks,'complete':all(x['complete'] for x in checks)},indent=2)+'\n')
print(json.dumps({'path':str(path),'complete':all(x['complete'] for x in checks),'resources_each':[len(x['created_resources']) for x in checks],'processes_each':[len(x['owned_processes']) for x in checks],'runtime_entries_each':[len(x['runtime_entries']) for x in checks]}))
assert all(x['complete'] for x in checks),'cleanup incomplete; preserve evidence and report'
