from pathlib import Path
import datetime
import difflib
import hashlib
import json
import subprocess

OUT = Path(__file__).parent
AUTHOR = Path('/workspace/scratch/project-model-credentials-http-author')
BASE = 'a0b012ce7acc6dd885407fe70bd7ee5b4448071b'
RUN = AUTHOR / 'pg-driver-v01/runs/new2'
TARGET = 'tests/model/project_credentials_http_unknown_test.go'


def sha(data):
    return hashlib.sha256(data).hexdigest()


def bind(path):
    p = Path(path)
    return {'path': str(p), 'sha256': sha(p.read_bytes())}


def readjson(path):
    return json.loads(Path(path).read_bytes())


def save(name, value):
    (OUT / name).write_text(json.dumps(value, indent=2, ensure_ascii=False) + '\n')


old = readjson(AUTHOR / 'candidate02/manifest.json')
new = readjson(AUTHOR / 'candidate03/manifest.json')
assert sha((AUTHOR / 'candidate03/manifest.json').read_bytes()) == 'a362e55ac9007d0a39dcbe885b7c73a46a05c1e6dc05ad7ad56ed20b022fed45'
assert sha((AUTHOR / 'candidate03/delta.diff').read_bytes()) == '124361ed277538b349f891c21e5aa26bb19a441e848c045b7e4d0213b1a61bbf'
assert old['files'].keys() == new['files'].keys()
changed = [p for p in new['files'] if old['files'][p]['sha256'] != new['files'][p]['sha256']]
assert changed == [TARGET]
assert len(new['files']) == 22
for m in (old, new):
    for p, row in m['files'].items():
        assert sha(Path(row['snapshot']).read_bytes()) == row['sha256'], p
before = Path(old['files'][TARGET]['snapshot']).read_text()
after = Path(new['files'][TARGET]['snapshot']).read_text()
(OUT / 'observed-source-delta.diff').write_text(''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True), fromfile='candidate02/' + TARGET, tofile='candidate03/' + TARGET)))

git_inputs = []
excerpts = []
for name, needles in {
    'db/migrations/00003_secret.sql': ['CREATE TABLE agenteam_secret.secret_command_receipts', 'project_id uuid', 'scope_key text GENERATED', 'command_digest text'],
    'internal/central/postgres/error.go': ['func failure(', 'validSQLState', 'func (e *Error) SQLState', 'len(state) != 5', "state[i] >= '0'"],
    'internal/central/postgres/sql.go': ['func (e *txExecutor) Query(', 't.poisonWith(failure(SQLFailed', 'func (e *txExecutor) QueryRow', 'func (r oneRow) Scan'],
    'internal/central/secret/write.go': ['NewCommandsCause', 'WithinTx(', 'AcquireAll(', 'PrepareWrite('],
}.items():
    argv = ['git', 'show', BASE + ':' + name]
    r = subprocess.run(argv, cwd='/workspace/agenteam', stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
    git_inputs.append({'git_commit': BASE, 'repository_path': name, 'sha256': sha(r.stdout), 'argv': argv, 'exit': r.returncode})
    lines = r.stdout.decode().splitlines()
    selected = set()
    for i, line in enumerate(lines):
        if any(n in line for n in needles):
            selected.update(range(max(0, i - 1), min(len(lines), i + 5)))
    excerpts.append(name + '\n' + '\n'.join(f'{i+1}: {lines[i]}' for i in sorted(selected)))
(OUT / 'fixed-dependency-excerpts.txt').write_text('\n\n'.join(excerpts) + '\n')

result = readjson(RUN / 'result.json')
cleanup = readjson(RUN / 'cleanup.json')
tail = readjson(RUN / 'owned-tail-retirement.json')
watchdog = readjson(RUN / 'watchdog.json')
assert result['driver_exit'] == 1 and result['actual_wait_completed'] and result['double_cleanup']
assert result['source_files_unchanged'] and result['verification_inputs_unchanged']
assert result['forced_tail_actions'] == 0 and result['watchdog_thread_joined']
assert len(cleanup) == 2
for row in cleanup:
    assert row['baseline_unchanged'] and row['owned_processes'] == []
    assert len(row['exact_absent']) == 7 and all(x['absent'] for x in row['exact_absent'].values())
assert tail == {'actions': [], 'actual_owned_completion': True, 'remaining': []}
assert watchdog['complete'] and not watchdog['active'] and not watchdog['failures']
assert (RUN / 'input-before.json').read_bytes() == (RUN / 'input-after.json').read_bytes()
raw = (RUN / 'raw.log').read_text().splitlines()
(OUT / 'original-raw-excerpt.txt').write_text('\n'.join(f'{i+1}: {line}' for i,line in enumerate(raw) if 'CredentialHTTP' in line or 'checkpoint' in line or 'physical target' in line) + '\n')

probe = Path('/workspace/scratch/project-owner-credential-http-verification/runtime-prep01/independent_project_credentials_formatted_test.go')
probe_text = probe.read_text()
assert 'r.project_id=$1 AND r.command_digest=$2' in probe_text
assert 'scope_key=$1' not in probe_text

sources = {
    'candidate02': bind(AUTHOR / 'candidate02/manifest.json'),
    'candidate03': bind(AUTHOR / 'candidate03/manifest.json'),
    'candidate03_delta': bind(AUTHOR / 'candidate03/delta.diff'),
    'old_target': old['files'][TARGET], 'new_target': new['files'][TARGET],
    'original_run': {n: bind(RUN / n) for n in ['raw.log', 'result.json', 'command.json', 'cleanup.json', 'owned-tail-retirement.json', 'watchdog.json', 'input-before.json', 'input-after.json']},
    'fixed_dependencies': git_inputs,
    'private_probe_unchanged': bind(probe),
    'prior_full_static': bind(OUT.parent / 'integration-static01/result.json'),
    'prior_store_wrapper': bind(OUT.parent / 'integration-static01/fixed-project_usage_http_fixture_test.go'),
    'prior_held_proxy': bind(OUT.parent / 'integration-static01/fixed-project_owner_update_http_unknown_test.go'),
}
save('sources.json', sources)
save('result.json', {
    'time_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'status': 'PASS bounded candidate03 delta STATIC; original new2 remains FAIL; resource rerun and full product acceptance pending',
    'scope': changed, 'all_22_snapshots_verified': True, 'unchanged_paths_reused': 21,
    'original_observation': {'driver_exit': 1, 'passive_lookup': 'PASS', 'prepare_nonce': 'PASS', 'three_final_modes': 'checkpoint timeout at original line121; original HTTP status/SQLSTATE not recorded'},
    'attribution': 'High-confidence static test-hook SQL defect: one inferred parameter was compared with both uuid project_id and text scope_key. No claim of an observed original SQLSTATE or observed final target backend.',
    'repair_static': 'Independent explicitly cast UUID/text parameters plus separate digest; fixed diagnostic stage/status and validated SQLSTATE only; original 503/CommitUnknown assertion now precedes proxy checkpoint wait.',
    'preserved': ['one tagged original Execute', 'same original Tx; Command EX/User SH/Project SH', 'exact Project/receipt/digest and unique backend owner', 'held pending writer then separate actual terminal completion', 'original no-confirm/zero-candidate assertions', 'release/owned proxy and Logout actual wait cleanup', 'unchanged production and unit02'],
    'original_cleanup': {'actual_wait': True, 'adopted_waits': result['adopted_waits'], 'forced_tail_actions': 0, 'seven_exact_resources_absent_twice': True, 'owned_empty_twice': True, 'inputs_identical': True, 'watchdog_joined': True, 'nonowned_pid1_zombies': 'Remain explicitly outside task ownership/wait; no whole-machine clean claim'},
    'private_probe': 'No equivalent scope_key same-parameter expression; no change or rerun made.',
    'checks_executed': 'Read-only fixed snapshot hash/diff checks, accepted Git reads and fixed author raw/result/cleanup inspection via Python; no Go/test/DB/socket/container execution.',
    'limitations': ['Original raw does not prove the exact SQLSTATE', 'No candidate03 runtime acceptance yet', 'Author held proxy is pending-to-terminal, not terminal-first ACK loss', 'Independent A/B resource execution remains unstarted and requires root explicit authorization'],
    'source_index': bind(OUT / 'sources.json'),
})
print(json.dumps({'result': bind(OUT / 'result.json'), 'sources': bind(OUT / 'sources.json'), 'changed': changed, 'all_snapshots': 22}, indent=2))
