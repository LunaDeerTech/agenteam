#!/usr/bin/env python3
"""Independent hand-worked evaluator/CLI controls; never backend or data judgments.

Run from any cwd: python3 .agent-state/search-benchmark-review/evaluator-controls.py
Only private temporary data and ignored output are written. Expected metrics are
specified here without importing the evaluator or the author's test helpers.
"""
import copy
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import tempfile

TARGET = Path(__file__).resolve().parents[2]
SCRIPT = TARGET / 'scripts/search-benchmark.py'
ORIGINAL = TARGET / 'tests/search-benchmark/data'
HERE = TARGET / 'output/ai/search-benchmark-review/evaluator'
HERE.mkdir(parents=True, exist_ok=True)
checks = []

def check(name, condition):
    assert condition, name
    checks.append(name)

def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, allow_nan=True) + '\n')

def lines(path, rows):
    path.write_text(''.join(json.dumps(row, ensure_ascii=False) + '\n' for row in rows))

def cli(*args):
    return subprocess.run([sys.executable, str(SCRIPT), *map(str, args)], cwd=TARGET,
                          env={**os.environ, 'PYTHONDONTWRITEBYTECODE': '1'}, capture_output=True, text=True, timeout=10)

with tempfile.TemporaryDirectory(prefix='isolated-', dir=HERE) as tmp:
    tmp = Path(tmp)
    data = tmp / 'data'; data.mkdir()
    original = {name: json.loads((ORIGINAL/name).read_text()) if name == 'dataset.json' else
                [json.loads(row) for row in (ORIGINAL/name).read_text().splitlines()]
                for name in ('dataset.json', 'corpus.jsonl', 'queries.jsonl', 'qrels.jsonl')}
    synthetic = copy.deepcopy(original)
    synthetic['dataset.json']['source_notice'] = 'metadata-label-canary'
    by_query = {row['query_id']: row for row in synthetic['queries.jsonl']}
    check('fixed q01/q02 category and split for hand-worked macro',
          by_query['q01']['category'] == 'zh' and by_query['q02']['category'] == 'en'
          and by_query['q01']['split'] == by_query['q02']['split'] == 'dev')
    sources = [f'd{n:02d}-s{s}' for n in range(1, 33) for s in (1, 2)]
    qrels = {row['query_id']: row for row in synthetic['qrels.jsonl']}
    for qid, grades in (
        ('q01', {'d01-s1':3, 'd01-s2':3, 'd02-s1':2, 'd02-s2':1, 'd03-s1':2}),
        ('q02', {'d32-s2':3, 'd32-s1':2}),
    ):
        qrels[qid]['grades'] = {sid: grades.get(sid, 0) for sid in sources}
        qrels[qid]['reasons'] = {sid:'judgment-label-canary' for sid in (*grades, 'd31-s2')}
        qrels[qid]['intent'] = 'intent-label-canary'
    for name, value in synthetic.items():
        if name == 'dataset.json': dump(data/name, value)
        else: lines(data/name, reversed(value))
    valid = cli('validate', '--data', data)
    check('private structural fixture accepted; not semantic evidence', valid.returncode == 0)
    result_map = {f'q{n:02d}': [] for n in range(1, 73)}
    result_map['q01'] = ['d02-s2', 'd01-s1', 'd03-s2', 'd02-s1', 'd04-s1', 'd03-s1']
    result_map['q02'] = sources[:19] + ['d32-s2']
    result_map['q65'] = sources[:20]
    result_map['q69'] = ['d31-s1']
    run = {'format_version':1, 'dataset_revision':original['dataset.json']['dataset_revision'],
           'backend':'pg_search', 'backend_version':'independent-control-not-a-backend',
           'config':{'analyzer':'control-only', 'query_mode':'control-only', 'ranking':'hand-worked-independent',
                     'candidate_k':20, 'tie_break':'source_id_ascii', 'adapter_revision':'independent-control'},
           'results':[{'query_id':q, 'hits':[{'source_id':s, 'rank':i, 'native_score':None}
                       for i,s in enumerate(hits,1)]} for q,hits in reversed(result_map.items())],
           'measurements':{'index_bytes':0, 'indexing_ms':0, 'environment':'synthetic controls, no backend', 'latencies':[
               {'query_id':'q01','mode':'warm','samples_ms':list(range(20,0,-1))},
               {'query_id':'q02','mode':'warm','samples_ms':[1000]},
               {'query_id':'q01','mode':'cold','samples_ms':[30000,10000,20000]},
           ]}}
    run_file = tmp/'run.json'; dump(run_file, run)
    first = tmp/'first'; complete = cli('score','--data',data,'--run',run_file,'--output',first)
    check('actual score CLI exits zero', complete.returncode == 0)
    report = json.loads((first/'report.json').read_text())
    rows = {row['query_id']:row for row in report['per_query']}
    # Independently specified grades/ranks. No evaluator function calculates expected values.
    ideal = 7 + 7/math.log2(3) + 3/2 + 3/math.log2(5) + 1/math.log2(6)
    dcg5 = 1 + 7/math.log2(3) + 3/math.log2(5)
    q1 = {'mrr_at_20':.5, 'recall_at_1':0, 'recall_at_5':.5, 'recall_at_10':.75, 'recall_at_20':.75,
          'ndcg_at_1':1/7, 'ndcg_at_5':dcg5/ideal,
          'ndcg_at_10':(dcg5 + 3/math.log2(7))/ideal, 'ndcg_at_20':(dcg5 + 3/math.log2(7))/ideal}
    q2 = {'mrr_at_20':.05, 'recall_at_1':0, 'recall_at_5':0, 'recall_at_10':0, 'recall_at_20':.5,
          'ndcg_at_1':0, 'ndcg_at_5':0, 'ndcg_at_10':0,
          'ndcg_at_20':(7/math.log2(21))/(7 + 3/math.log2(3))}
    for qid, expected in (('q01',q1),('q02',q2)):
        for key, value in expected.items():
            check(f'{qid} hand worked {key}', math.isclose(rows[qid]['metrics'][key],value,abs_tol=1e-14))
    for group, denominator, expected in (
        ('overall',64,{key:q1[key]+q2[key] for key in q1}),
        ('split:dev',32,{key:q1[key]+q2[key] for key in q1}),
        ('category:zh',8,q1),('category:en',8,q2),
        ('category_split:zh:dev',4,q1),('category_split:en:dev',4,q2),
    ):
        check(group+' explicit denominator',report['groups'][group]['queries']==denominator)
        for key in q1:
            check(group+' '+key,math.isclose(report['groups'][group][key],expected[key]/denominator,abs_tol=1e-14))
    check('all explicit empty answerable queries kept as zero',all(all(v==0 for v in rows[f'q{n:02d}']['metrics'].values()) for n in range(3,65)))
    check('all no-answer query metrics isolated',all(rows[f'q{n:02d}']['metrics'] is None for n in range(65,73)))
    check('no-answer macro and returned count',report['no_answer']=={'queries':8,'queries_with_hits':2,'fraction_with_hits':.25,'returned_candidates':21})
    check('provenance explicitly imported',report['provenance']=='imported_run')
    measured=json.loads((first/'measurements.json').read_text())
    check('mode sample count and nearest rank 21',measured['by_mode']['warm']=={'samples':21,'p50_ms':11,'p95_ms':20})
    check('cold mode isolated',measured['by_mode']['cold']=={'samples':3,'p50_ms':20000,'p95_ms':30000})
    check('reported measurements, legitimate zero preserved',measured['provenance']=='reported_measurements' and measured['index_bytes']==0 and measured['indexing_ms']==0)
    second=tmp/'second'; reordered=copy.deepcopy(run); reordered['results'].reverse(); dump(run_file,reordered)
    check('reordered query input scores',cli('score','--data',data,'--run',run_file,'--output',second).returncode==0)
    check('ID association deterministic full outputs',all((first/name).read_bytes()==(second/name).read_bytes() for name in ('report.json','measurements.json','complete.json')))
    exported=tmp/'export'; check('actual export CLI',cli('export','--data',data,'--output',exported).returncode==0)
    for name, fields, count in (('corpus.jsonl',{'dataset_revision','source_id','index_text'},64),('queries.jsonl',{'dataset_revision','query_id','text'},72)):
        raw=(exported/name).read_text(); exported_rows=[json.loads(line) for line in raw.splitlines()]
        check(name+' exact fields and count',len(exported_rows)==count and all(set(row)==fields for row in exported_rows))
        check(name+' no hidden qrel/meta labels','label-canary' not in raw)
    source_rows={row['source_id']:row for row in original['corpus.jsonl']}
    check('index_text exact original bytes',all(row['index_text']==source_rows[row['source_id']]['title']+'\n'+source_rows[row['source_id']]['section']+'\n\n'+source_rows[row['source_id']]['raw_text'] for row in [json.loads(line) for line in (exported/'corpus.jsonl').read_text().splitlines()]))
    def first_hit(value): return next(row for row in value['results'] if row['query_id']=='q01')['hits'][0]
    def first_hits(value): return next(row for row in value['results'] if row['query_id']=='q01')['hits']
    mutations=[
        ('bool format',lambda r:r.update(format_version=True)), ('wrong revision',lambda r:r.update(dataset_revision='lexical-other')),
        ('unknown field',lambda r:r.update(extra=0)), ('unknown backend',lambda r:r.update(backend='fake')),
        ('bool K',lambda r:r['config'].update(candidate_k=True)), ('duplicate query',lambda r:r['results'].append(copy.deepcopy(r['results'][0]))),
        ('missing empty query',lambda r:r['results'].pop(2)), ('empty all results',lambda r:r.update(results=[])),
        ('unknown source',lambda r:first_hit(r).update(source_id='missing')), ('bool rank',lambda r:first_hit(r).update(rank=True)),
        ('rank zero',lambda r:first_hit(r).update(rank=0)), ('rank gap',lambda r:first_hit(r).update(rank=2)),
        ('duplicate source',lambda r:first_hits(r)[1].update(source_id=first_hit(r)['source_id'])),
        ('bool score',lambda r:first_hit(r).update(native_score=True)), ('nan score',lambda r:first_hit(r).update(native_score=float('nan'))),
        ('infinite score',lambda r:first_hit(r).update(native_score=float('inf'))), ('string score',lambda r:first_hit(r).update(native_score='1')),
        ('21 hits',lambda r:first_hits(r).__setitem__(slice(None),[{'source_id':s,'rank':i,'native_score':None} for i,s in enumerate(sources[:21],1)])),
        ('bool latency',lambda r:r['measurements']['latencies'][0].update(samples_ms=[True])),
        ('negative latency',lambda r:r['measurements']['latencies'][0].update(samples_ms=[-1])),
        ('wrong mode',lambda r:r['measurements']['latencies'][0].update(mode='mixed')),
        ('duplicate measurement',lambda r:r['measurements']['latencies'].append(copy.deepcopy(r['measurements']['latencies'][0]))),
        ('fractional bytes',lambda r:r['measurements'].update(index_bytes=.5)),
    ]
    for n,(name,mutate) in enumerate(mutations):
        value=copy.deepcopy(run); mutate(value); dump(run_file,value); out=tmp/f'bad-{n}'
        failed=cli('score','--data',data,'--run',run_file,'--output',out)
        check(name+' rejected without output',failed.returncode==1 and not out.exists() and '"status": "ok"' not in failed.stdout and failed.stderr.startswith('search_benchmark_error='))
    raw=json.dumps(run)
    for n,(name,bad) in enumerate((('duplicate member',raw.replace('"format_version": 1','"format_version": 1, "format_version": 1',1)),('exponent overflow',raw.replace('"native_score": null','"native_score": 1e309',1)),('second JSON',raw+' {}'))):
        run_file.write_text(bad+'\n'); out=tmp/f'rawbad-{n}'
        failed=cli('score','--data',data,'--run',run_file,'--output',out)
        check(name+' rejected',failed.returncode==1 and not out.exists())
    # Dataset guards use private input variants, never edits to author data.
    for name, mutate in (
        ('incomplete qrels',lambda q:q[0]['grades'].pop('d01-s1')),
        ('bool grade',lambda q:q[0]['grades'].update({'d01-s1':True})),
        ('different qrel revision',lambda q:q[0].update(dataset_revision='other')),
    ):
        value=copy.deepcopy(synthetic['qrels.jsonl']); mutate(value); lines(data/'qrels.jsonl',value)
        check(name+' rejected by public validate',cli('validate','--data',data).returncode==1)
    lines(data/'qrels.jsonl',synthetic['qrels.jsonl'])
    data_queries=copy.deepcopy(synthetic['queries.jsonl']); data_queries[0]['answerable']=1; lines(data/'queries.jsonl',data_queries)
    check('numeric answerable rejected',cli('validate','--data',data).returncode==1)
    data_queries=copy.deepcopy(synthetic['queries.jsonl'])
    dev=next(row for row in data_queries if row['split']=='dev'); test=next(row for row in data_queries if row['split']=='test')
    test['family_id']=dev['family_id']; lines(data/'queries.jsonl',data_queries)
    check('declared family cross split rejected',cli('validate','--data',data).returncode==1)
    check('query rank21 is out of range by CLI contract',any(name=='21 hits rejected without output' for name in checks))

result={'evaluator_code_review':'5dc38928','dataset_revision':original['dataset.json']['dataset_revision'],'python':sys.version.split()[0],'checks':len(checks),'passed':True,'expected_from_evaluator':False,'backend_run':False,'semantic_data_acceptance':False}
(HERE/'result.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result))
