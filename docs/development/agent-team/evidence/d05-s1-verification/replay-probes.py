"""Recreate the tested overlay in a new owned directory.

Run only with an explicitly assigned Docker fixture window, on the fixed S1
code (49c6589c3919cad62a4bae2c993b5fd953d36b1e). This helper does not grant a
window or reproduce the independent exact-ID resource audit.
"""
import hashlib
import json
import os
import pathlib
import subprocess
import tempfile

here = pathlib.Path(__file__).resolve().parent
repo = here.parents[4]
record = json.loads((here / 'inputs.json').read_text())['start']
allowed = {'db/migrations/00016_object_project_work_maintenance.sql',
           'tests/objects/project_stop_work_migration_test.go'}
for name, digest in record['frozen_files'].items():
    if hashlib.sha256((repo / name).read_bytes()).hexdigest() != digest:
        raise SystemExit('Fixed input changed: ' + name)
changed = subprocess.check_output(['git', 'diff', '--name-only', record['freeze_initial_head'],
    '--', '*.go', '*.sql', 'scripts/*.sh', 'go.mod', 'go.sum'], cwd=repo, text=True).splitlines()
untracked = subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard',
    '--', '*.go', '*.sql'], cwd=repo, text=True).splitlines()
if (set(changed) | set(untracked)) - allowed:
    raise SystemExit('Use a separate checkout of the fixed S1 code; later runtime changes are outside this evidence.')
probe = here / 's1_independent_probe.go.txt'
if hashlib.sha256(probe.read_bytes()).hexdigest() != 'cf0f4db8a747e6b6a7bc244f096acf939a1aee9ae31882fac37e3fa30b4fafa0':
    raise SystemExit('Independent probe changed')
owned = pathlib.Path(tempfile.mkdtemp(prefix='agenteam-d05-s1-replay-'))
for name in ['gocache', 'docker-config', 'runtime-tmp']:
    (owned / name).mkdir(mode=0o700)
(owned / 'overlay.json').write_text(json.dumps({'Replace': {
    str(repo / 'tests/objects/d05_s1_independent_probe_test.go'): str(probe)}}) + '\n')
command = json.loads((here / 'command.json').read_text())
env = {k: os.environ[k] for k in ['PATH', 'HOME', 'USER', 'LANG'] if k in os.environ}
env.update(command['explicit_environment'])
env.update({'GOCACHE': str(owned / 'gocache'), 'DOCKER_CONFIG': str(owned / 'docker-config'),
    'TMPDIR': str(owned / 'runtime-tmp'),
    'GOFLAGS': '-mod=readonly -v -p=2 -overlay=' + str(owned / 'overlay.json')})
print('Owned replay directory:', owned, flush=True)
with (owned / 'fixture.log').open('w') as log:
    result = subprocess.run(command['command'], cwd=repo, env=env, stdout=log, stderr=subprocess.STDOUT)
(owned / 'result.json').write_text(json.dumps({'exit_code': result.returncode}) + '\n')
raise SystemExit(result.returncode)
