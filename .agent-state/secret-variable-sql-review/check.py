#!/usr/bin/env python3
"""Limited offline DDL/source review. Does not parse/execute PostgreSQL SQL."""
import json, os, re, subprocess
from pathlib import Path
ROOT=Path('/workspace/agenteam-secret-variable-storage')
HERE=Path(__file__).resolve().parent
OWN=HERE.parents[1]
BASE=OWN/'output/ai/runner-control'
OUT=BASE/'tmp/secret-variable-sql-review'
OUT.mkdir(parents=True,exist_ok=True)
name='db/migrations/00029_project_variable_receipts.sql'
sql=(ROOT/name).read_text()
frozen=subprocess.check_output(['git','show','17969f85:'+name],cwd=ROOT,text=True)
assert sql==frozen, 'reviewed DDL drift'
def normalize(raw):
 return ' '.join(re.sub(r'--[^\n]*','',raw).split())
assert normalize(sql)==normalize((ROOT/'.agent-state/secret-variable-storage/project-variable-storage.draft.sql').read_text())
# Pin the limited DDL boundary; no legacy table, consumer or FK expansion.
assert re.findall(r'ALTER TABLE\s+([\w.]+)',sql)==['agenteam_secret.secret_payloads']*2+['agenteam_secret.secrets']*2
assert re.findall(r'CREATE TABLE\s+([\w.]+)',sql)==['agenteam_secret.project_variable_receipts']
assert re.findall(r'REFERENCES\s+([\w.]+)',normalize(sql))==['agenteam_secret.secret_payloads']
assert 'DEFERRABLE INITIALLY DEFERRED' in sql and 'CASCADE' not in normalize(sql)
assert "CHECK (owner_kind <> 3 OR scope = 'project')" in sql
assert 'CHECK (owner_kind <> 3 OR octet_length(ciphertext) = 48)' in sql
assert "CHECK (purpose <> 'project_variable' OR scope = 'project')" in sql
assert 'UNIQUE (project_id, command_digest)' in sql and 'digest_payload_id agenteam_secret.safe_id NOT NULL UNIQUE' in sql
assert 'ON agenteam_secret.project_variable_receipts(project_id, id)' in sql
assert not subprocess.check_output(['git','diff','8cb0a953..17969f85','--','db/migrations/00003_secret.sql'],cwd=ROOT)
# UUIDv7 syntax domain is additive and not an existing same-schema type.
previous='\n'.join(p.read_text() for p in (ROOT/'db/migrations').glob('*.sql') if p.name[:5].isdigit() and int(p.name[:5])<29)
assert not re.search(r'CREATE\s+DOMAIN\s+agenteam_secret.safe_id',previous,re.I)
print('frozen DDL/draft/legacy isolation/ownership FK and access-path declarations: PASS',flush=True)
virtual=ROOT/'.agent-state/secret-variable-storage/independent-ddl-manifest.go'
overlay=OUT/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(virtual):str(HERE/'manifest.go')}}))
env=dict(os.environ,PATH='/workspace/toolchains/go1.27.1/bin:'+os.environ['PATH'],AGENTEAM_GO='/workspace/toolchains/go1.27.1/bin/go',GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOPROXY='off',GOSUMDB='off',GOTELEMETRY='off',GOFLAGS='-mod=readonly -p=1',GOMAXPROCS='2',GOCACHE=str(BASE/'gocache'),GOMODCACHE=str(BASE/'go-mod'),GOTMPDIR=str(BASE/'tmp'),XDG_CONFIG_HOME=str(BASE/'go-config'))
r=subprocess.run(['/workspace/toolchains/go1.27.1/bin/go','run','-overlay',str(overlay),str(virtual)],cwd=ROOT,env=env,timeout=60)
raise SystemExit(r.returncode)
