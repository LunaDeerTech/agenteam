import json,re,subprocess,sys
from pathlib import Path
root=Path('/workspace/agenteam');base=Path('/workspace/scratch/usage-http-author')
source=(root/'internal/central/usage/http/wire_test.go').read_text()
v={'document':json.loads((root/'api/openapi/project-usage.json').read_text()),'common':json.loads((root/'api/openapi/common.json').read_text()),'cases':[]}
for schema,good,bad in [('common:NonnegativeInt64String',['0','1','9007199254740993','9223372036854775807'],['01','9223372036854775808','0\n','1\u2028',1,None]),('common:PositiveInt64String',['1','9223372036854775807'],['0','1\r','1\u2029','１２']),('ProjectName',['Project','a-._'],['.','..','name\n','name\u2028','x%']),('ProviderRequestID',['id','A-_:9.'],['id\n','id\u2028','é']),('Username',['admin','AdMin','User-Name'],['ab','-admin','admin\n','admin\u2029'])]:
 for valid,values in [(True,good),(False,bad)]:
  for n,x in enumerate(values):v['cases'].append({'name':schema+str(n),'schema':schema,'value':x,'valid':valid})
data=json.dumps(v).encode();(base/'schema-preflight-input.json').write_bytes(data)
for label,binary,flag in [('wirePythonSchema',sys.executable,'-c'),('wireNodePatterns','/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node','-e')]:
 script=re.search(r'const '+label+r' = `([\s\S]*?)`',source).group(1)
 result=subprocess.run([binary,flag,script],input=data,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=10)
 sys.stdout.buffer.write(result.stdout)
 if result.returncode:sys.exit(result.returncode)
print('Preflight only: hand-supplied scalar corpus; actual Go projections await fixed dependency recovery.')
