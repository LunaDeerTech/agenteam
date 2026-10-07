from pathlib import Path
import json, hashlib, re

B = Path(__file__).resolve().parent
sha = lambda p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
put = lambda p, d: p.write_text(json.dumps(d, indent=2) + '\n')
ref = lambda p: {'path': str(p), 'sha256': sha(p)}
d = json.loads((B / 'closure02.json').read_text())
parent = json.loads(Path(d['parent_input']['path']).read_text())
overlay = json.loads(Path(d['overlay']['path']).read_text())['Replace']
expected = dict(parent['files'])
for virtual, backing in overlay.items():
    expected.pop(virtual, None)
    expected[backing] = d['files'][backing]
expected.update(d['files'])
run = B / 'offline/g01-race-http'
assert json.loads((run / 'result.json').read_text())['passed']
source = (run / 'stdout.log').read_text()
decoder = json.JSONDecoder()
packages, files, extra, generated, hooks = [], {}, {}, {}, []
fields = ['GoFiles', 'TestGoFiles', 'XTestGoFiles', 'CgoFiles', 'SFiles', 'CFiles', 'HFiles', 'CXXFiles', 'MFiles', 'FFiles', 'SysoFiles', 'SwigFiles', 'SwigCXXFiles', 'EmbedFiles', 'TestEmbedFiles', 'XTestEmbedFiles']
while source.strip():
    row, n = decoder.raw_decode(source.lstrip())
    source = source.lstrip()[n:]
    assert not row.get('Error') and not row.get('DepsErrors')
    packages.append(row['ImportPath'])
    selected = row['ImportPath'].split(' [')[0] in {'github.com/LunaDeerTech/agenteam/internal/central/project/http', 'github.com/LunaDeerTech/agenteam/internal/central/project/http_test'}
    paths = []
    for field in fields:
        if field in {'TestGoFiles', 'XTestGoFiles', 'TestEmbedFiles', 'XTestEmbedFiles'} and not selected:
            continue
        paths.extend(str(Path(row['Dir']) / name) for name in row.get(field, []))
    module = row.get('Module') or {}
    module = module.get('Replace') or module
    if module.get('GoMod'):
        paths.append(module['GoMod'])
    for virtual in paths:
        path = overlay.get(virtual, virtual)
        if not path:
            continue
        value = sha(path)
        if row['ImportPath'].endswith('.test') and path.startswith('/workspace/.cache/go-build/'):
            generated[path] = value
            text = Path(path).read_text()
            assert 'os.Exit(m.Run())' in text and '._testmain' not in text
        else:
            files[path] = value
            if expected.get(path) != value:
                extra[path] = value
        if selected and Path(path).suffix == '.go' and re.search(r'^func (TestMain|init)\s*\(', Path(path).read_text(), re.M):
            hooks.append(path)
record = {'raw': ref(run / 'stdout.log'), 'result': ref(run / 'result.json'), 'packages_count': len(packages), 'files_count': len(files), 'effective_files_digest': hashlib.sha256(json.dumps(files, sort_keys=True, separators=(',', ':')).encode()).hexdigest(), 'parent_input': d['parent_input'], 'overlay': d['overlay'], 'extra': extra, 'generated_testmain': generated, 'selected_package_init_or_TestMain': sorted(set(hooks))}
put(B / 'actual-graph-check.json', record)
print(json.dumps({'packages': len(packages), 'files': len(files), 'extra': extra, 'hooks': hooks}))
assert not extra and not hooks
d['independent_graphs'] = [ref(B / 'actual-graph-check.json')]
d['files'].update(generated)
d['files'][str(B / 'close-graph.py')] = sha(__file__)
put(B / 'closure03.json', d)
p = json.loads((B / 'graph-plan.json').read_text())
p['scope'] = 'HTTP private exact pure probe, no resources; race compile and vet only in this plan.'
p['closure'] = ref(B / 'closure03.json')
(B / 'bin').mkdir(exist_ok=True)
binary = str(B / 'bin/http-race.test')
go = '/workspace/toolchains/go1.27.1/bin/go'
p['commands'] = [{'label': 'c01-http-race', 'argv': [go, 'test', '-race', '-p=1', '-c', '-o', binary, './internal/central/project/http'], 'binary_output': binary}, {'label': 'v01-http-race', 'argv': [go, 'vet', '-race', './internal/central/project/http']}]
put(B / 'compile-plan.json', p)
print('compile-plan-sha256', sha(B / 'compile-plan.json'))
