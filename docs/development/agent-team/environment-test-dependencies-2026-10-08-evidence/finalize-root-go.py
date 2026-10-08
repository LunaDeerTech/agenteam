import hashlib
import json
import pathlib
import time

root = pathlib.Path(__file__).resolve().parent
evidence = root / 'evidence'
result_path = evidence / 'root-go-result.json'
result = json.loads(result_path.read_text())
commands = []
for path in evidence.glob('root-go-*.meta.json'):
    meta = json.loads(path.read_text())
    name = path.name.removesuffix('.meta.json')
    assert 'exit_code' in meta
    assert hashlib.sha256((evidence / (name + '.raw')).read_bytes()).hexdigest() == meta['raw_sha256']
    assert not pathlib.Path('/proc', str(meta['pid'])).exists(), name
    commands.append({'name': name, 'started_ns': meta['started_ns'], 'pid': meta['pid'], 'exit_code': meta['exit_code'], 'raw_sha256': meta['raw_sha256']})
commands.sort(key=lambda value: value['started_ns'])
assert len(commands) == 6
assert [(value['name'], value['exit_code']) for value in commands if value['exit_code']] == [('root-go-download', 1)]
assert (evidence / 'root-go-inputs.raw').read_bytes() == (evidence / 'root-go-inputs-after.raw').read_bytes()
assert (evidence / 'root-go-verify.raw').read_text().strip() == 'all modules verified'
result.update({
    'finalized_ns': time.time_ns(),
    'offline_readonly_go_mod_verify_exit_code': 0,
    'root_go_input_fingerprints_unchanged': True,
    'commands': commands,
    'all_recorded_children_actually_waited': True,
    'recorded_direct_pids_remaining': [],
    'no_product_graph_or_test_run_by_dependency_worker': True,
    'cache_write_window': {
        'source': 'Current task coordination messages, recorded after the actions.',
        'writer': 'fixture_recovery',
        'root_authorized': True,
        'cache_owner_uid_and_worker_uid': 1000,
        'backend_worker_ack_no_go_readers_before_write': True,
        'verification_worker_ack_no_go_readers_before_write': True,
        'repository_readonly_mount': True,
        'write_paths': ['/workspace/go/pkg/mod', str(root)],
        'released_to': ['root', 'backend_worker', 'verification_worker'],
        'writer_stopped': True,
    },
})
result_path.write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({'commands': len(commands), 'nonzero': 1, 'required_modules': result['required_module_count'], 'offline_verify': 'passed', 'locks_unchanged': True, 'cache_window_released': True}))
