import hashlib
import json
import os
import pathlib
import time

root = pathlib.Path(__file__).resolve().parent
evidence = root / 'evidence'
archive_old = pathlib.Path('/workspace/agenteam/docs/development/agent-team/environment-test-dependencies-2026-10-07-evidence/evidence')
expected_sha = 'dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'
commands = []
for path in evidence.glob('*.meta.json'):
    meta = json.loads(path.read_text())
    name = path.name.removesuffix('.meta.json')
    raw = evidence / (name + '.raw')
    assert 'exit_code' in meta, name
    assert hashlib.sha256(raw.read_bytes()).hexdigest() == meta['raw_sha256'], name
    commands.append({'name': name, 'pid': meta['pid'], 'started_ns': meta['started_ns'], 'exit_code': meta['exit_code'], 'raw_sha256': meta['raw_sha256']})
commands.sort(key=lambda value: value['started_ns'])
assert len(commands) == 33
assert [(c['name'], c['exit_code']) for c in commands if c['exit_code']] == [
    ('cache-inventory', 1), ('pg17-inspect', 1), ('pg16-inspect', 1), ('npm-installed', 1)
]
assert (evidence / 'source-inputs.raw').read_bytes() == (evidence / 'preserved-inputs.raw').read_bytes()
assert (evidence / 'node-inputs.raw').read_bytes() == (evidence / 'node-inputs-after.raw').read_bytes()
binary = root / 'bin/minio'
digest = hashlib.sha256(binary.read_bytes()).hexdigest()
assert digest == expected_sha
assert binary.stat().st_size == 109289632
version = (evidence / 'minio-version.raw').read_text()
assert 'RELEASE.2025-10-15T17-29-55Z' in version
assert '9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a' in version
assert 'Runtime: go1.27.1 linux/amd64' in version
source = json.loads((evidence / 'source-verification.json').read_text())
old_source = json.loads((archive_old / 'source-verification.json').read_text())
for field in ('zip_sha256', 'zip_bytes', 'module_sum', 'gomod_sum', 'files'):
    assert source[field] == old_source[field], field
for name, expected in source['files'].items():
    assert hashlib.sha256((pathlib.Path(source['source']) / name).read_bytes()).hexdigest() == expected
images = []
for name, digest, pg_version in (
    ('pg17', '99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc', '17.8-1.pgdg12+1'),
    ('pg16', '16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782', '16.12-1.pgdg12+1'),
):
    observed = json.loads((evidence / (name + '-inspect02.raw')).read_text())[0]
    ref = 'pgvector/pgvector@sha256:' + digest
    assert ref in observed['RepoDigests']
    assert observed['Os'] == 'linux' and observed['Architecture'] == 'amd64'
    assert 'PG_VERSION=' + pg_version in observed['Config']['Env']
    images.append({'reference': ref, 'image_id': observed['Id'], 'os': observed['Os'], 'architecture': observed['Architecture'], 'configured_pg_version': pg_version})
node_versions = [json.loads(line) for line in (evidence / 'lock-installed-check.raw').read_text().splitlines()]
assert all(not value['required_missing'] and not value['mismatch'] for value in node_versions)
assert (evidence / 'locked-playwright-version.raw').read_text().strip() == 'Version 1.56.1'
assert 'not found' not in (evidence / 'chromium-libs.raw').read_text()
remaining_pids = []
for command in commands:
    pid = command['pid']
    if pathlib.Path('/proc', str(pid)).exists():
        remaining_pids.append(pid)
assert not remaining_pids, remaining_pids
result = {
    'recorded_ns': time.time_ns(),
    'scope': 'dependency recovery only; no product tests, browser launch, containers, databases or listeners started',
    'root_provided_baseline': 'ff396a4e (no Git commands executed by recovery worker)',
    'minio': {'path': str(binary), 'bytes': binary.stat().st_size, 'sha256': expected_sha, 'matches_fixture_binary_sha': True, 'version_output': version, 'source': source, 'build_exit_code': 0, 'build_elapsed_seconds': json.loads((evidence / 'minio-build-01.meta.json').read_text())['elapsed_seconds']},
    'images': images,
    'node': {'version': 'v24.19.0', 'playwright': '1.56.1', 'lock_checks': node_versions, 'package_and_lock_fingerprints_unchanged': True, 'web_action': 'copied only go-captcha-vue@2.0.7 from the successful harness npm ci after identical locked integrity was checked'},
    'browser': {'path': '/usr/bin/chromium', 'version_output': (evidence / 'chromium-version.raw').read_text().strip(), 'missing_ldd_libraries': [], 'fingerprints': (evidence / 'chromium-sha.raw').read_text(), 'official_playwright_browser_downloaded': False, 'browser_launched': False, 'compatibility_with_playwright_not_dynamically_verified': True},
    'fixed_repository_inputs_unchanged': True,
    'commands': commands,
    'command_count': len(commands),
    'zero_exit_count': sum(c['exit_code'] == 0 for c in commands),
    'nonzero_exit_count': sum(c['exit_code'] != 0 for c in commands),
    'all_recorded_children_actually_waited': True,
    'recorded_child_pids_present_at_finalization': remaining_pids,
    'pid_scope_limit': 'Recorded direct command PIDs only; no whole-machine process cleanup claim.',
}
(evidence / 'recovery-result.json').write_text(json.dumps(result, indent=2, ensure_ascii=False) + '\n')
print(json.dumps({'command_count': len(commands), 'nonzero_exit_count': result['nonzero_exit_count'], 'binary_sha_matches': True, 'recorded_pids_remaining': remaining_pids, 'result': str(evidence / 'recovery-result.json')}, ensure_ascii=False))
