#!/usr/bin/env python3
"""Bounded real SQL adapter check, not a relevance benchmark or App acceptance.

Owns one nonce container/network, no published port and no existing database.
The table shapes retain UUID domains and query columns; they are deliberately
not represented as the full application migration/constraint lifecycle.
Run only after the integration owner allocates the PG resource window.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
DEPLOY = ROOT / "deploy/retrieval"
IMAGE = 'pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc'


def uid(n):
    return f'00000000-0000-7000-8000-{n:012x}'


def lit(value):
    if value is None:
        return 'NULL'
    if isinstance(value, int):
        return str(value)
    return "'" + value.replace("'", "''") + "'"


def fixture_sql():
    sql = ['CREATE EXTENSION vector VERSION \'0.8.1\';']
    for schema in ['agenteam_knowledge', 'agenteam_memory']:
        sql += [f'CREATE SCHEMA {schema};', f"CREATE DOMAIN {schema}.safe_id AS uuid CHECK(VALUE::text ~ '^[0-9a-f]{{8}}-[0-9a-f]{{4}}-7[0-9a-f]{{3}}-[89ab][0-9a-f]{{3}}-[0-9a-f]{{12}}$');"]
    sql += ['''CREATE TABLE agenteam_knowledge.index_chunks (
 id agenteam_knowledge.safe_id PRIMARY KEY,project_id agenteam_knowledge.safe_id,
 generation_id agenteam_knowledge.safe_id,dimension integer,index_text text,raw_text text,locator jsonb);
CREATE TABLE agenteam_knowledge.index_generations (
 id agenteam_knowledge.safe_id PRIMARY KEY,project_id uuid,document_id uuid,profile_id uuid,
 state text,snapshot_id uuid,dimension integer,document_version bigint);
CREATE TABLE agenteam_knowledge.documents (
 id agenteam_knowledge.safe_id PRIMARY KEY,project_id uuid,content_version bigint,status text,title text,media_type text,source_kind text);
CREATE TABLE agenteam_knowledge.index_state (project_id uuid PRIMARY KEY,serving_profile_id uuid);
CREATE TABLE agenteam_knowledge.index_profiles (id uuid PRIMARY KEY,project_id uuid,state text,snapshot_id uuid,dimension integer);
CREATE TABLE agenteam_memory.projections (
 generation_id agenteam_memory.safe_id,project_id agenteam_memory.safe_id,agent_id agenteam_memory.safe_id,
 memory_id agenteam_memory.safe_id,revision bigint,index_text text,PRIMARY KEY(generation_id,memory_id,revision));
CREATE TABLE agenteam_memory.memories (project_id uuid,agent_id uuid,memory_id uuid,current_revision bigint,status text,PRIMARY KEY(project_id,agent_id,memory_id));
CREATE TABLE agenteam_memory.revisions (project_id uuid,agent_id uuid,memory_id uuid,revision bigint,body text,status text,type text,tags text[],PRIMARY KEY(project_id,agent_id,memory_id,revision));''']

    def insert(table, *values):
        sql.append('INSERT INTO ' + table + ' VALUES (' + ','.join(lit(v) for v in values) + ');')

    query = '索引恢复 PostgreSQL release_2026'
    plain = query + ' ' + ('普通内容 ' * 80)
    poison = (query + ' ') * 20
    for project in [1, 11]:
        insert('agenteam_knowledge.index_state', uid(project), uid(project+3))
        insert('agenteam_knowledge.index_profiles', uid(project+3), uid(project), 'active', uid(project+2), 4096)
    insert('agenteam_knowledge.index_profiles', uid(24), uid(1), 'retired', uid(3), 4096)
    # Eligible rows: two tied chunks of one document, plus media/source variants.
    for number, media, source in [(6, 'text/plain', 'upload'), (60, 'text/markdown', 'upload'), (61, 'text/plain', 'external')]:
        insert('agenteam_knowledge.documents', uid(number), uid(1), 2, 'active', 'public fixture', media, source)
        insert('agenteam_knowledge.index_generations', uid(number+100), uid(1), uid(number), uid(4), 'ready', uid(3), 4096, 2)
        insert('agenteam_knowledge.index_chunks', uid(number+200), uid(1), uid(number+100), 4096, plain, plain, '{}')
    insert('agenteam_knowledge.index_chunks', uid(207), uid(1), uid(106), 4096, plain, plain, '{}')
    # These rows individually fail a canonical eligibility condition.
    insert('agenteam_knowledge.documents', uid(62), uid(1), 2, 'deleted', 'public fixture', 'text/plain', 'upload')
    for n, doc, profile, state, snapshot, dimension, version in [
        (30, 6, 4, 'ready', 3, 4096, 1), (31, 6, 4, 'retired', 3, 4096, 2),
        (32, 6, 24, 'ready', 3, 4096, 2), (33, 62, 4, 'ready', 3, 4096, 2),
        (34, 6, 4, 'ready', 99, 4096, 2), (35, 6, 4, 'ready', 3, 2, 2),
    ]:
        insert('agenteam_knowledge.index_generations', uid(n), uid(1), uid(doc), uid(profile), state, uid(snapshot), dimension, version)
        insert('agenteam_knowledge.index_chunks', uid(n+200), uid(1), uid(n), 4096, poison, poison, '{}')
    insert('agenteam_knowledge.documents', uid(16), uid(11), 2, 'active', 'public fixture', 'text/plain', 'upload')
    insert('agenteam_knowledge.index_generations', uid(116), uid(11), uid(16), uid(14), 'ready', uid(13), 4096, 2)
    for n in range(300, 330):
        insert('agenteam_knowledge.index_chunks', uid(n), uid(11), uid(116), 4096, poison, poison, '{}')

    for n in range(70, 110):
        project, agent, generation, current, mstatus, rstatus, kind, tags = 1, 2, 5, 2, 'active', 'active', 'fact', '{kept}'
        if n == 72:
            current = 3
        elif n == 73:
            mstatus = 'deleted'
        elif n == 74:
            rstatus = 'deleted'
        elif n == 75:
            generation = 25
        elif n == 76:
            kind = 'preference'
        elif n == 77:
            tags = '{other}'
        elif n >= 78:
            agent = 12
        body = plain if n < 72 else poison
        insert('agenteam_memory.memories', uid(project), uid(agent), uid(n), current, mstatus)
        insert('agenteam_memory.revisions', uid(project), uid(agent), uid(n), 2, body, rstatus, kind, tags)
        insert('agenteam_memory.projections', uid(generation), uid(project), uid(agent), uid(n), 2, body)
    # Same memory_id in another generation and older revision: score must refer
    # to the scanned tuple, not assume a globally unique memory_id key.
    insert('agenteam_memory.projections', uid(25), uid(1), uid(2), uid(70), 2, poison)
    insert('agenteam_memory.revisions', uid(1), uid(2), uid(70), 1, poison, 'active', 'fact', '{kept}')
    insert('agenteam_memory.projections', uid(5), uid(1), uid(2), uid(70), 1, poison)
    sql.append('ANALYZE;')
    return '\n'.join(sql), query


def product_queries():
    k = (ROOT/'internal/central/knowledge/index_query.go').read_text()
    m = (ROOT/'internal/central/memory/retrieval.go').read_text()
    projection = re.search(r'const knowledgeCandidateProjection = `([^`]+)`', k).group(1)
    scope = re.search(r'const knowledgeCandidateFrom = `([^`]+)`', k).group(1)
    knowledge = ('SELECT '+projection+',paradedb.score(c.id)'+scope+
                 " AND (c.id @@@ paradedb.match('index_text',$6,conjunction_mode=>false)) ORDER BY paradedb.score(c.id) DESC,c.id ASC LIMIT $7")
    raw = re.search(r'`SELECT p.memory_id::text.*?LIMIT \$7`', m).group(0)
    memory = raw.replace('`+expressions.Score+`', 'paradedb.score(p.memory_id)').replace('`+expressions.Match+`', "p.memory_id @@@ paradedb.match('index_text',$4,conjunction_mode=>false)")[1:-1]
    assert '`' not in memory and len(scope) > 500
    return knowledge, memory


def readiness_sql():
    source = (ROOT/'internal/central/retrieval/lexical/sql.go').read_text()
    return re.search(r'const PGSearchReadinessSQL = `([^`]+)`', source).group(1)+';'


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--output', type=Path, required=True)
    ap.add_argument('--cache', type=Path, required=True)
    args = ap.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    nonce = uuid.uuid4().hex
    name, label = 'm3-pgsearch-'+nonce, 'agenteam.m3.pgsearch='+nonce
    owned = dict(nonce=nonce, container_id=None, network_id=None)
    result = dict(scope='isolated UUID-domain SQL/bootstrap; not global migrations or App/Tool acceptance', passed=False, checks=[], cleanup={})
    sequence = 0

    def command(argv, timeout=60, data=None, required=True):
        nonlocal sequence
        sequence += 1
        proc = subprocess.run(argv, input=data, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
        (args.output/f'{sequence:03d}.log').write_text(proc.stdout+proc.stderr)
        if required and proc.returncode:
            raise RuntimeError(f'command {sequence} exit {proc.returncode}; see owned output')
        return proc

    def record():
        (args.output/'owned.json').write_text(json.dumps(owned, indent=2)+'\n')

    def sql(value, required=True):
        proc = command(['docker','exec','-i',owned['container_id'],'gosu','postgres','psql','-h','/tmp/pgsearch-socket','-d','postgres','-X','-qAt','-v','ON_ERROR_STOP=1'], data="SET statement_timeout='15s';\n"+value, required=required)
        for line in proc.stderr.splitlines():
            if line.startswith('WARNING:') and line not in result.setdefault('warnings',[]):
                result['warnings'].append(line)
        return proc

    try:
        command(['docker','image','inspect',IMAGE])
        owned['network_id'] = command(['docker','network','create','--label',label,name]).stdout.strip(); record()
        owned['container_id'] = command(['docker','run','-d','--name',name,'--label',label,'--network',owned['network_id'],'--memory','2g','--cpus','2','--mount',f'type=bind,source={args.cache.resolve()},target=/deps,readonly','--mount',f'type=bind,source={DEPLOY},target=/recipe,readonly','--entrypoint','sleep',IMAGE,'1800']).stdout.strip(); record()
        cid = owned['container_id']
        command(['docker','cp','/etc/ssl/certs/ca-certificates.crt',cid+':/tmp/host-ca.crt'])
        proxy = [part for key in ['HTTP_PROXY','HTTPS_PROXY','NO_PROXY','http_proxy','https_proxy','no_proxy'] if key in os.environ for part in ['-e',key]]
        command(['docker','exec']+proxy+[cid,'timeout','--kill-after=5s','180s','bash','-euc', 'sed -i "s|http://|https://|g" /etc/apt/sources.list.d/pgdg.list /etc/apt/sources.list.d/debian.sources; apt-get -o Acquire::https::CaInfo=/tmp/host-ca.crt -o APT::Update::Error-Mode=any update -qq; apt-get -o Acquire::https::CaInfo=/tmp/host-ca.crt install -y --no-install-recommends --no-upgrade libopenblas0=0.3.21+ds-4 libopenblas0-pthread=0.3.21+ds-4'], timeout=195)
        command(['docker','exec',cid,'sh','/recipe/install-pg-search.sh','/deps/postgresql-17-pg-search_0.26.1-1PARADEDB-bookworm_amd64.deb'])
        command(['docker','exec',cid,'bash','-euc','install -d -o postgres -g postgres /tmp/pgsearch-data /tmp/pgsearch-socket; gosu postgres initdb -D /tmp/pgsearch-data --encoding=UTF8 --locale=C --auth-local=trust --auth-host=reject; gosu postgres pg_ctl -D /tmp/pgsearch-data -l /tmp/pgsearch.log -o "-c listen_addresses= -c unix_socket_directories=/tmp/pgsearch-socket -c shared_preload_libraries=pg_search -c max_connections=12 -c shared_buffers=128MB" -w start'])
        seed, query = fixture_sql()
        (args.output/'schema.sql').write_text(seed)
        sql(seed)
        assert sql(readiness_sql()).stdout.strip() == 'f', 'missing extension/index incorrectly ready'
        sql((DEPLOY/'bootstrap-pg-search.sql').read_text())
        assert sql(readiness_sql()).stdout.strip() == 't', 'bootstrap did not satisfy production readiness'
        definitions = sql("SELECT n.nspname,c.relname,i.indisvalid,i.indisready,pg_get_indexdef(c.oid),pg_get_indexdef(c.oid,2,true) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_index i ON i.indexrelid=c.oid WHERE c.relname IN ('knowledge_chunks_pg_search','memory_projections_pg_search') ORDER BY c.relname;").stdout
        (args.output/'index-definitions.txt').write_text(definitions)
        result['checks'].append('exact package install and bootstrap of UUID-domain indexes')
        result['checks'].append('production readiness rejects missing extension/index and accepts exact bootstrap')
        knowledge, memory = product_queries()
        (args.output/'knowledge-query.sql').write_text(knowledge+';\n')
        (args.output/'memory-query.sql').write_text(memory+';\n')
        result['query_sha256'] = {key:hashlib.sha256(value.encode()).hexdigest() for key,value in [('knowledge',knowledge),('memory',memory)]}

        def query_rows(which, params, expected):
            body = knowledge if which == 'knowledge' else memory
            types = 'uuid,uuid,uuid,text,text,text,integer' if which == 'knowledge' else 'uuid,uuid,uuid,text,text[],text[],integer'
            statement = f'PREPARE selected({types}) AS {body};\nEXECUTE selected('+','.join(lit(v) for v in params)+');'
            rows = sql(statement).stdout.strip().splitlines()
            ids = [r.split('|')[0] for r in rows]
            assert ids == [uid(v) for v in expected], (which, ids, expected)
            return statement

        kparams = [uid(1),uid(4),None,'','',query,20]
        query_rows('knowledge',kparams,[206,207,260,261])
        query_rows('knowledge',[uid(1),uid(4),uid(6),'','',query,1],[206])
        query_rows('knowledge',[uid(1),uid(4),None,'text/plain','upload',query,20],[206,207])
        query_rows('knowledge',[uid(1),uid(24),None,'','',query,20],[])
        result['checks'].append('Knowledge production join predicates: project/current/ready/serving/snapshot/dimension and document/media/source before LIMIT, UUID ties')
        mparams = [uid(1),uid(2),uid(5),query,'{fact}','{kept}',20]
        query_rows('memory',mparams,[70,71])
        query_rows('memory',mparams[:-1]+[1],[70])
        result['checks'].append('Memory production join predicates: namespace/canonical active revision/generation/type/tags before LIMIT, duplicate memory_id generations/revisions, UUID ties')
        for which, params in [('knowledge',kparams),('memory',mparams)]:
            body = knowledge if which == 'knowledge' else memory
            types = 'uuid,uuid,uuid,text,text,text,integer' if which == 'knowledge' else 'uuid,uuid,uuid,text,text[],text[],integer'
            plan = sql(f'PREPARE selected({types}) AS {body};\nEXPLAIN (FORMAT JSON) EXECUTE selected('+','.join(lit(v) for v in params)+');').stdout
            assert 'Custom Scan' in plan, plan
            (args.output/(which+'-plan.json')).write_text(plan)
        result['checks'].append('actual CustomScan plans with default enable_seqscan, production joins and secondary UUID ordering')
        # An explicit repeated bootstrap fails instead of silently replacing or
        # accepting an index. The first transaction's complete indexes remain.
        assert sql((DEPLOY/'bootstrap-pg-search.sql').read_text(), required=False).returncode != 0
        result['checks'].append('repeated bootstrap rejects existing indexes without silently claiming a new install')
        sql('DROP INDEX agenteam_memory.memory_projections_pg_search;')
        assert sql(readiness_sql()).stdout.strip() == 'f', 'missing Memory index incorrectly ready'
        sql("CREATE INDEX memory_projections_pg_search ON agenteam_memory.projections USING bm25 (memory_id,index_text) WITH(key_field='memory_id');")
        assert sql(readiness_sql()).stdout.strip() == 'f', 'wrong tokenizer incorrectly ready'
        sql("DROP INDEX agenteam_memory.memory_projections_pg_search; CREATE INDEX memory_projections_pg_search ON agenteam_memory.projections USING bm25 (memory_id,(index_text::pdb.chinese_compatible)) WITH(key_field='memory_id');")
        assert sql(readiness_sql()).stdout.strip() == 't'
        result['checks'].append('production readiness rejects a missing index and a BM25 index with wrong tokenizer, then accepts exact restoration')
        result['passed'] = True
    finally:
        if owned['container_id']:
            cid = owned['container_id']
            observed = command(['docker','inspect','--format','{{ index .Config.Labels "agenteam.m3.pgsearch" }}',cid]).stdout.strip()
            assert observed == nonce, 'refuse cleanup of unowned container'
            result['cleanup']['pg_stop_exit'] = command(['docker','exec',cid,'gosu','postgres','pg_ctl','-D','/tmp/pgsearch-data','-m','fast','-w','stop'],required=False).returncode
            command(['docker','stop','--time','3',cid],required=False)
            result['cleanup']['container_exit'] = command(['docker','inspect','--format','{{.State.ExitCode}}',cid]).stdout.strip()
            command(['docker','rm',cid])
            result['cleanup']['container_absent'] = command(['docker','inspect',cid],required=False).returncode != 0
        if owned['network_id']:
            nid = owned['network_id']
            observed = command(['docker','network','inspect','--format','{{ index .Labels "agenteam.m3.pgsearch" }}',nid]).stdout.strip()
            assert observed == nonce, 'refuse cleanup of unowned network'
            command(['docker','network','rm',nid])
            result['cleanup']['network_absent'] = command(['docker','network','inspect',nid],required=False).returncode != 0
        (args.output/'result.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps(result))


if __name__ == '__main__':
    main()
