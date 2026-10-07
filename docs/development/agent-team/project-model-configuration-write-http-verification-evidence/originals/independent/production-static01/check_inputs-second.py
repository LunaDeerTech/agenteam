from pathlib import Path
import hashlib
import json

OUT = Path(__file__).resolve().parent
AUTHOR = Path('/workspace/scratch/project-model-configuration-write-http-author')
PROD = AUTHOR / 'production01'
def sha(p):
    return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def verified(path, expected):
    assert sha(path) == expected, str(path)
    return json.loads(Path(path).read_text())

manifest = verified(PROD / 'manifest.json', '36105d193d9f28d3522d9e5a1d02774829aed3228576822868b5c7cfdd1e6b07')
handoff = verified(AUTHOR / 'handoff01.json', '02cfe052108c8db0c1a6049e9e11383d6198fc7f23318e3cdf89f0233938aa89')
assert sha(PROD / 'changes.patch') == '033f5f6162bfd9892c86e40f646f75a7cc8c2222b7df18973397a58456b330d3'
for entry in manifest['files'].values():
    assert sha(entry['snapshot']) == entry['sha256']
for entry in handoff['old_five_current'].values():
    assert sha(entry['snapshot']) == entry['sha256']

# Token spans preserve the exact source text of a JSON value, including its
# original internal whitespace. This is a read-only structural comparison.
decoder = json.JSONDecoder()
def spans(text):
    found = {}
    def ws(i):
        while i < len(text) and text[i].isspace(): i += 1
        return i
    def value(i, path):
        i = ws(i)
        start = i
        if text[i] == '{':
            i = ws(i + 1)
            while text[i] != '}':
                key, i = decoder.raw_decode(text, i)
                i = ws(i)
                assert text[i] == ':'
                i = ws(value(i + 1, path + (key,)))
                if text[i] == ',': i = ws(i + 1)
                else: assert text[i] == '}'
            i += 1
        elif text[i] == '[':
            i = ws(i + 1)
            n = 0
            while text[i] != ']':
                i = ws(value(i, path + (n,)))
                n += 1
                if text[i] == ',': i = ws(i + 1)
                else: assert text[i] == ']'
            i += 1
        else:
            _, i = decoder.raw_decode(text, i)
        found[path] = text[start:i]
        return i
    assert ws(value(0, ())) == len(text)
    return found

schema_key = 'api/openapi/project-models.json'
old_path = Path(handoff['old_five_current'][schema_key]['snapshot'])
new_path = Path(manifest['files'][schema_key]['snapshot'])
old_text, new_text = old_path.read_text(), new_path.read_text()
old, new = json.loads(old_text), json.loads(new_text)
old_spans, new_spans = spans(old_text), spans(new_text)
methods = {'get', 'head', 'post', 'put', 'delete', 'patch', 'options', 'trace'}
old_ops = {(p, m) for p, item in old['paths'].items() for m in item if m in methods}
new_ops = {(p, m) for p, item in new['paths'].items() for m in item if m in methods}
assert len(old_ops) == 10 and len(new_ops - old_ops) == 7
for p, m in old_ops:
    key = ('paths', p, m)
    assert old_spans[key] == new_spans[key], key
    assert old['paths'][p][m] == new['paths'][p][m]
assert len(old['components']['schemas']) == 12
for name, obj in old['components']['schemas'].items():
    key = ('components', 'schemas', name)
    assert old_spans[key] == new_spans[key], key
    assert obj == new['components']['schemas'][name]
for key in old.keys() - {'paths', 'components', 'info'}:
    assert old[key] == new[key], key
assert set(new['info']) == set(old['info']) == {'title', 'version', 'description'}
assert new['info']['version'] == '1.1.0'
for key in old['components'].keys() - {'schemas'}:
    assert old['components'][key] == new['components'][key], key
prefix = '/api/v1/projects/{project_id}/'
expected_ops = {(prefix + x, m) for x, m in [
    ('model-providers', 'post'), ('model-providers/{provider_id}', 'put'),
    ('model-providers/{provider_id}', 'delete'), ('models', 'post'),
    ('models/{model_id}', 'put'), ('models/{model_id}', 'delete'),
    ('model-commands/lookup', 'post')]}
assert new_ops - old_ops == expected_ops
all_ids = [new['paths'][p][m]['operationId'] for p,m in new_ops]
assert len(set(all_ids)) == len(all_ids)
new_schemas = sorted(new['components']['schemas'].keys() - old['components']['schemas'].keys())
assert len(new_schemas) == 9

def refs(obj):
    if isinstance(obj, dict):
        if '$ref' in obj: yield obj['$ref']
        for value in obj.values(): yield from refs(value)
    elif isinstance(obj, list):
        for value in obj: yield from refs(value)
for ref in refs(new):
    if ref.startswith('#/'):
        target = new
        for component in ref[2:].split('/'):
            target = target[component.replace('~1', '/').replace('~0', '~')]
external_before = {r for r in refs(old) if not r.startswith('#/')}
external_after = {r for r in refs(new) if not r.startswith('#/')}
assert external_after == external_before

credential = Path('/workspace/scratch/project-model-credentials-http-author/candidate01/src/internal/central/model/http_project_credentials.go')
assert sha(credential) == 'af55eb294e131d779bc7cfc1fc892c522f07da86666e088b73af51ac4513a878'
current = Path(manifest['files']['internal/central/model/http_project_configuration.go']['snapshot']).read_text()
def io_section(text, name):
    return text[text.index('type ' + name + 'IOWriter struct'):text.index('func (h *' + name + 'HTTP) ServeHTTP')]
assert io_section(credential.read_text(), 'credential').replace('credential', 'configuration') == io_section(current, 'configuration')
result = {
    'status': 'PASS_STATIC_FINGERPRINT_AND_STRUCTURAL_CHECKS_ONLY',
    'candidate_manifest_sha256': sha(PROD / 'manifest.json'),
    'candidate_patch_sha256': sha(PROD / 'changes.patch'),
    'four_snapshot_hashes_match': True,
    'old_five_handoff_snapshot_hashes_match': True,
    'old_operation_exact_value_bytes_unchanged': len(old_ops),
    'old_schema_exact_value_bytes_unchanged': len(old['components']['schemas']),
    'new_operation_set_exact': sorted([list(item) for item in expected_ops]),
    'new_schema_names': new_schemas,
    'metadata_info_delta': {'old': old['info'], 'new': new['info']},
    'internal_refs_resolve': True,
    'external_refs_set_unchanged': sorted(external_after),
    'io_helper_exact_after_private_name_substitution': True,
    'credential_io_source_sha256': sha(credential),
    'Go_or_tests_or_resources_executed': False,
}
(OUT / 'checks.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps(result, indent=2))
