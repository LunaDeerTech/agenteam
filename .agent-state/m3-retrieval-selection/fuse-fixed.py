#!/usr/bin/env python3
"""Compute the frozen dense leg and three equal-weight RRF comparisons.

No network, database, qrels, query labels or model calls. Output uses the existing
scorer's backend field to identify the lexical leg, with the actual dense/hybrid
method explicit in config; it never presents a fusion run as a new lexical run.
"""
import argparse
from fractions import Fraction
import gzip
import hashlib
import json
import math
from pathlib import Path

HERE = Path(__file__).resolve().parent
VECTOR_SHA256 = '948074800e4cc25bff2e88305f5edd70ea82f2c677565821e0d96f1a4953276d'


def normalized(vector, dimensions):
    if len(vector) != dimensions or any(type(x) not in (float, int) or not math.isfinite(x) for x in vector):
        raise ValueError('invalid embedding dimension or non-finite value')
    norm = math.sqrt(math.fsum(x*x for x in vector))
    if norm == 0 or not math.isfinite(norm):
        raise ValueError('zero or non-finite embedding norm')
    return [x/norm for x in vector]


def dense_rank(query, documents, k=20):
    values = [(sid, math.fsum(a*b for a, b in zip(query, vector, strict=True))) for sid, vector in documents.items()]
    return sorted(values, key=lambda pair: (-pair[1], pair[0]))[:k]


def fuse(dense, lexical, k=60, output_k=20):
    # Exact rational accumulation makes rank-pair ties independent of floating
    # point addition, input leg order and dictionary insertion order.
    values = {}
    for leg in (dense, lexical):
        if len(leg) != len(set(leg)):
            raise ValueError('duplicate source within retrieval leg')
        for rank, sid in enumerate(leg, 1):
            values[sid] = values.get(sid, Fraction()) + Fraction(1, k+rank)
    return sorted(values.items(), key=lambda pair: (-pair[1], pair[0]))[:output_k]


def hits(values):
    return [dict(source_id=sid, rank=rank, native_score=float(value)) for rank, (sid, value) in enumerate(values, 1)]


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2)+'\n')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--vectors', type=Path, required=True)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--export', dest='export_dir', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    config = json.loads((HERE/'candidates.json').read_text())['dense']
    assert (config['candidate_k'], config['rrf_k'], config['weights'], config['output_k']) == (20, 60, [1, 1], 20)
    compressed = args.vectors.read_bytes()
    assert hashlib.sha256(compressed).hexdigest() == VECTOR_SHA256
    raw = gzip.decompress(compressed)
    evidence = json.loads(raw)
    manifest = json.loads(args.manifest.read_text())
    assert manifest['actual_wait'] == 0
    assert manifest['snapshot'] == evidence['snapshot']
    assert manifest['files']['vectors.json.gz']['sha256'] == VECTOR_SHA256
    assert manifest['files']['vectors.json.gz']['uncompressed_sha256'] == hashlib.sha256(raw).hexdigest()
    assert manifest['inputs_sha256'] == evidence['inputs_sha256']
    assert evidence['snapshot']['dimensions'] == 4096
    assert [call['items'] for call in evidence['calls']] == [64, 64, 8]
    assert manifest['http_statuses'] == [200, 200, 200]
    export = {}
    for name, field, count in [('corpus.jsonl', 'source_id', 64), ('queries.jsonl', 'query_id', 72)]:
        value = (args.export_dir/name).read_bytes()
        assert hashlib.sha256(value).hexdigest() == evidence['inputs_sha256'][name]
        rows = [json.loads(line) for line in value.splitlines()]
        assert len(rows) == count
        ids = [row[field] for row in rows]
        assert ids == sorted(set(ids)) and all(row['dataset_revision'] == 'lexical-v2' for row in rows)
        export[field] = set(ids)
    vectors = {}
    for collection, field in [('corpus', 'source_id'), ('queries', 'query_id')]:
        rows = evidence[collection]
        ids = [row[field] for row in rows]
        assert len(ids) == len(set(ids)) and set(ids) == export[field]
        vectors[collection] = {row[field]: normalized(row['embedding'], 4096) for row in rows}
    lexical = {}
    lexical_hashes = {}
    for name, backend in [('pgroonga', 'pgroonga'), ('pg-search', 'pg_search')]:
        path = HERE/'results'/(name+'-run.json')
        original = path.read_bytes()
        run = json.loads(original)
        source = json.loads((HERE/'results'/(name+'-source.json')).read_text())
        assert run['backend'] == backend and run['dataset_revision'] == 'lexical-v2'
        assert run['config']['candidate_k'] == 20 and run['config']['tie_break'] == 'source_id_ascii'
        assert all(source['inputs_sha256'][key] == value for key, value in evidence['inputs_sha256'].items())
        rows = run['results']
        assert len(rows) == 72 and {row['query_id'] for row in rows} == export['query_id']
        lexical[backend] = {}
        for row in rows:
            assert [hit['rank'] for hit in row['hits']] == list(range(1, len(row['hits'])+1))
            assert len(row['hits']) <= 20
            ids = [hit['source_id'] for hit in row['hits']]
            assert len(ids) == len(set(ids)) and set(ids) <= export['source_id']
            lexical[backend][row['query_id']] = ids
        lexical_hashes[backend] = hashlib.sha256(original).hexdigest()
    # Reuse the independently accepted explicit historical statement that all
    # 72 original native FTS results were empty. The missing historical run is
    # not reconstructed, rerun or presented as an observed original artifact.
    lexical['native_pg_fts'] = {qid: [] for qid in sorted(export['query_id'])}
    dense = {qid: dense_rank(vector, vectors['corpus']) for qid, vector in sorted(vectors['queries'].items())}
    output = {
        'dense': ('native_pg_fts', [dict(query_id=qid, hits=hits(values)) for qid, values in dense.items()])
    }
    for backend, rows in lexical.items():
        output[backend+'-rrf'] = (backend, [dict(query_id=qid, hits=hits(fuse([sid for sid, _ in values], rows[qid]))) for qid, values in dense.items()])
    args.output.mkdir(parents=True, exist_ok=False)
    for name, (backend, rows) in output.items():
        run = dict(format_version=1, dataset_revision='lexical-v2', backend=backend,
            backend_version='fixed-runtime-snapshot '+evidence['snapshot']['id'],
            config=dict(analyzer='deerling-embedding 4096; immutable openai-embeddings-float-v2 snapshot; configured identity only',
                query_mode='dense cosine on all fixed 64 sources; '+('dense only (backend label retained for scorer compatibility)' if name == 'dense' else 'fusion with frozen '+backend+' lexical rank'),
                ranking='cosine descending' if name == 'dense' else 'equal-weight RRF 1/(60+dense_rank)+1/(60+lexical_rank); absent leg=0',
                candidate_k=20, tie_break='source_id_ascii', adapter_revision='m3-fixed-dense-rrf-v1'),
            results=rows, measurements=None)
        write_json(args.output/(name+'-run.json'), run)
    provenance = dict(snapshot=evidence['snapshot'], vectors_sha256=VECTOR_SHA256,
        vector_source_commit='784c6468', vector_source_path='.agent-state/m3-model-runtime/runtime-embeddings-v2/vectors.json.gz',
        inputs_sha256=evidence['inputs_sha256'], lexical_run_sha256=lexical_hashes,
        native_leg_source='D13 section1.1 independently accepted historical 72 empty results; original run file unavailable',
        provider_response_model_observed=evidence['provider_response_model_observed'],
        config=config, counts=dict(corpus=64, queries=72, dimensions=4096),
        scope='fixed benchmark synthetic sources; production scope/current-version/serving filters not exercised',
        actual_model_calls_reused=3, new_model_calls=0, new_database_queries=0)
    write_json(args.output/'provenance.json', provenance)
    write_json(args.output/'complete.json', dict(kind='fixed-dense-and-three-rrf', queries=72, runs=list(output)))
    print(json.dumps(dict(runs=list(output), queries=72, vectors_sha256=VECTOR_SHA256)))


if __name__ == '__main__':
    main()
