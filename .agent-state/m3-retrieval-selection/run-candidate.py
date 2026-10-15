#!/usr/bin/env python3
"""One frozen lexical run; receives only a completed answer-free export.

All SQL runs on the nonce-owned container's private Unix socket. No environment
database URL, external credentials, scorer, qrels, labels or network are read.
"""
import argparse
import hashlib
import json
import math
import pathlib
import subprocess


def sql_string(value):
    return "'" + value.replace("'", "''") + "'"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--owned', type=pathlib.Path, required=True)
    ap.add_argument('--export', dest='export_path', type=pathlib.Path, required=True)
    ap.add_argument('--backend', choices=['pgroonga', 'pg_search'], required=True)
    ap.add_argument('--output', type=pathlib.Path, required=True)
    args = ap.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    owned = json.loads(args.owned.read_text())
    cid = owned['container_id']
    actual = json.loads(subprocess.check_output(['docker', 'inspect', cid]))[0]
    assert actual['Id'] == cid and actual['State']['Running']
    assert actual['Config']['Labels']['agenteam.m3.retrieval'] == owned['nonce']
    assert any(n['NetworkID'] == owned['network_id'] for n in actual['NetworkSettings']['Networks'].values())
    frozen = json.loads(pathlib.Path(__file__).with_name('candidates.json').read_text())
    config = frozen[args.backend]
    marker = json.loads((args.export_path / 'complete.json').read_text())
    assert marker == {'format_version': 1, 'kind': 'export', 'files': ['corpus.jsonl', 'queries.jsonl']}
    def read(name, fields, count):
        lines = [json.loads(x) for x in (args.export_path / name).read_text().splitlines()]
        assert len(lines) == count and all(set(x) == set(fields) and x['dataset_revision'] == 'lexical-v2' for x in lines)
        return lines
    corpus = read('corpus.jsonl', ['dataset_revision', 'source_id', 'index_text'], 64)
    queries = read('queries.jsonl', ['dataset_revision', 'query_id', 'text'], 72)
    source_hashes = {n: hashlib.sha256((args.export_path / n).read_bytes()).hexdigest() for n in ['complete.json', 'corpus.jsonl', 'queries.jsonl']}
    sequence = 0
    def sql(body):
        nonlocal sequence
        sequence += 1
        command = ['docker', 'exec', '-i', '--user', 'postgres', cid, 'psql', '-h', '/tmp/retrieval-socket', '-d', 'postgres', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1']
        result = subprocess.run(command, input="SET statement_timeout='10s'; SET standard_conforming_strings=on; SET client_encoding='UTF8';\n" + config['session'] + ';\n' + body, text=True, capture_output=True, timeout=90)
        (args.output / f'sql-{sequence:02d}.sql').write_text(body)
        (args.output / f'sql-{sequence:02d}.stdout').write_text(result.stdout)
        (args.output / f'sql-{sequence:02d}.stderr').write_text(result.stderr)
        assert result.returncode == 0, f'SQL stage {sequence} failed: {result.stderr[:500]}'
        return result.stdout.strip()
    version = json.loads(sql("SELECT json_build_object('postgres',current_setting('server_version_num'),'extensions',(SELECT json_object_agg(extname,extversion) FROM pg_extension));"))
    assert version['postgres'] == '170008' and version['extensions']['vector'] == '0.8.1'
    if args.backend == 'pgroonga':
        sql('CREATE EXTENSION pgroonga;')
        assert sql("SELECT extversion FROM pg_extension WHERE extname='pgroonga';") == '4.1.0'
    else:
        assert version['extensions']['pg_search'] == '0.26.1'
    setup = 'CREATE TABLE candidate_sections(source_id text COLLATE "C" PRIMARY KEY,index_text text NOT NULL);\n'
    setup += 'INSERT INTO candidate_sections VALUES ' + ','.join('('+sql_string(x['source_id'])+','+sql_string(x['index_text'])+')' for x in corpus) + ';\n'
    setup += config['index'] + '; ANALYZE candidate_sections;'
    sql(setup)
    # Observe the actual plan before querying the fixed evaluation inputs.
    plan = json.loads(sql('EXPLAIN (FORMAT JSON) ' + config['query'].replace('$1', "'人工智能 lifecycleprobe'") + ';'))
    assert 'candidate_lexical' in json.dumps(plan), 'the actual plan must use the candidate index'
    def match_count(term):
        query = config['query'].replace('$1', sql_string(term))
        return int(sql('SELECT count(*) FROM (' + query + ') AS hits;'))
    # Extra lifecycle row is removed, then the same original 64 rows are rebuilt.
    sql("INSERT INTO candidate_sections VALUES('lifecycle_marker','zzlifecycleoldtoken');")
    assert match_count('zzlifecycleoldtoken') == 1
    sql("UPDATE candidate_sections SET index_text='zzlifecyclenewtoken' WHERE source_id='lifecycle_marker';")
    assert match_count('zzlifecycleoldtoken') == 0 and match_count('zzlifecyclenewtoken') == 1
    sql("DELETE FROM candidate_sections WHERE source_id='lifecycle_marker';")
    assert match_count('zzlifecyclenewtoken') == 0
    sql('REINDEX INDEX candidate_lexical; ANALYZE candidate_sections;')
    restored = json.loads(sql('SELECT json_agg(t ORDER BY source_id COLLATE "C") FROM (SELECT source_id,index_text FROM candidate_sections) t;'))
    assert restored == [{k:x[k] for k in ['source_id', 'index_text']} for x in corpus]
    # All 72 queries exactly once in one persistent psql connection. Scores are
    # diagnostic; no qrels or score-dependent configuration changes occur here.
    statements = []
    for query in queries:
        query_sql = config['query'].replace('$1', sql_string(query['text']))
        statements.append("SELECT json_build_object('query_id'," + sql_string(query['query_id']) + ", 'hits',COALESCE(json_agg(json_build_object('source_id',source_id,'rank',rank,'native_score',score) ORDER BY rank),'[]'::json)) FROM (SELECT source_id,score,row_number() OVER(ORDER BY score DESC,source_id COLLATE \"C\" ASC) AS rank FROM (" + query_sql + ') ranked) numbered;')
    results = [json.loads(line) for line in sql('\n'.join(statements)).splitlines()]
    assert len(results) == 72
    for result in results:
        assert len(result['hits']) <= 20
        assert all(math.isfinite(hit['native_score']) for hit in result['hits'])
    size = int(sql("SELECT pg_relation_size('candidate_lexical');"))
    sql('DROP TABLE candidate_sections;')
    run = dict(format_version=1, dataset_revision='lexical-v2', backend=args.backend, backend_version=config['version'],
        config=dict(analyzer=config['analyzer'], query_mode=config['query'], ranking=config['ranking'], candidate_k=20, tie_break='source_id_ascii', adapter_revision='m3-lexical-v1'),
        results=results, measurements=dict(index_bytes=size, indexing_ms=None, environment='PostgreSQL17.8/vector0.8.1; nonce container 2CPU/3GiB; private Unix socket/psql; no timing samples; relation size excludes external extension files', latencies=[]))
    (args.output / 'run.json').write_text(json.dumps(run, ensure_ascii=False, indent=2)+'\n')
    (args.output / 'source.json').write_text(json.dumps(dict(owned=owned,version=version,inputs_sha256=source_hashes,config=config,plan=plan,lifecycle='insert/update/delete/reindex and exact 64-row readback passed'), ensure_ascii=False, indent=2)+'\n')
    (args.output / 'complete.json').write_text('{"candidate_complete":true,"queries":72,"cleanup_table":true}\n')
    print(json.dumps({'backend': args.backend, 'queries': len(results), 'candidate_complete': True, 'table_removed': True}))


if __name__ == '__main__':
    main()
