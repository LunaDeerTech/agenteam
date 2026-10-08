import hashlib
import json
import pathlib
import re

root = pathlib.Path(__file__).resolve().parent
repository = pathlib.Path('/workspace/agenteam')
evidence = root / 'evidence'
required = re.findall(r'^\s+([^\s]+) (v[^\s]+)', (repository / 'go.mod').read_text(), re.M)
sums = {(name, version): value for name, version, value in (line.split() for line in (repository / 'go.sum').read_text().splitlines())}
decoder = json.JSONDecoder()

def decode_stream(raw):
    values = []
    while raw.lstrip().startswith('{'):
        value, offset = decoder.raw_decode(raw.lstrip())
        values.append(value)
        raw = raw.lstrip()[offset:]
    return values, raw.strip()

values, remainder = decode_stream((evidence / 'root-go-download02.raw').read_text())
assert not remainder
assert {(value['Path'], value['Version']) for value in values} == set(required)
records = []
for value in values:
    name, version = value['Path'], value['Version']
    assert 'Error' not in value
    assert value['Sum'] == sums[name, version]
    assert value['GoModSum'] == sums[name, version + '/go.mod']
    assert pathlib.Path(value['Dir']).is_dir()
    zip_path = pathlib.Path(value['Zip'])
    assert zip_path.with_suffix('.ziphash').read_text().strip() == sums[name, version]
    records.append({
        'path': name, 'version': version, 'sum': value['Sum'], 'gomod_sum': value['GoModSum'],
        'dir': value['Dir'], 'zip_sha256': hashlib.sha256(zip_path.read_bytes()).hexdigest(),
        'gomod_sha256': hashlib.sha256(pathlib.Path(value['GoMod']).read_bytes()).hexdigest(),
    })
original = json.loads((evidence / 'root-go-cache-preflight.json').read_text())
for name, key in (('go.mod', 'go_mod_sha256'), ('go.sum', 'go_sum_sha256')):
    assert hashlib.sha256((repository / name).read_bytes()).hexdigest() == original[key]
initial_values, initial_remainder = decode_stream((evidence / 'root-go-download.raw').read_text())
assert 'go.sum: read-only file system' in initial_remainder
assert not any('Error' in value for value in initial_values)
result = {
    'scope': 'Separate root-module dependency recovery; previous successful MinIO source/build/binary records were not rerun.',
    'cache': '/workspace/go/pkg/mod',
    'go_mod_sha256': original['go_mod_sha256'],
    'go_sum_sha256': original['go_sum_sha256'],
    'lock_files_unchanged': True,
    'required_module_count': len(records),
    'initial_required_source_directories_missing': sum(not value['directory_exists'] for value in original['required_modules']),
    'locked_sum_and_gomod_sum_match_count': len(records),
    'required_source_directories_now_present': len(records),
    'initial_all_failure': {
        'exit_code': 1, 'reason': initial_remainder,
        'reported_module_count': len(initial_values),
        'reported_modules_outside_explicit_go_mod_requirements': len({(value['Path'], value['Version']) for value in initial_values} - set(required)),
        'new_download_count_in_wider_graph': 'Not measured; raw records include reused cache entries.',
        'cache_action': 'Kept downloaded public module cache; no shared cache deletion attempted.',
    },
    'exact_required_download_exit_code': 0,
    'modules': records,
}
(evidence / 'root-go-result.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({key: value for key, value in result.items() if key != 'modules'}, indent=2))
