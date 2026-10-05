from pathlib import Path
import os
import hashlib
import json
import shutil
import subprocess
import time

ROOT = Path(__file__).parent
INPUT = ROOT / 'input'
NODE = str(Path(shutil.which('node')).resolve())
MODULES = INPUT / 'node_modules'
(ROOT / 'runtime').mkdir(exist_ok=True)
selected_env = {'NODE_ENV': 'test', 'TZ': 'UTC', 'LANG': 'C.UTF-8', 'CI': '1',
                'TMPDIR': str(ROOT / 'runtime'), 'npm_config_offline': 'true',
                'npm_config_ignore_scripts': 'true'}
env = os.environ.copy()
env.update(selected_env)
paths = [p for p in INPUT.rglob('*') if p.is_file() and 'node_modules' not in p.parts]
files = [{'path': str(p.relative_to(ROOT)), 'sha256': hashlib.sha256(p.read_bytes()).hexdigest(),
          'bytes': p.stat().st_size} for p in sorted(paths)]
manifest = {'base': '457b1979c9d6563740543b2011eedc06cce34c71',
            'core_manifest_sha256': '0bd7611e421549ef4d13a148b50e04359b3b53845fabfb6a6b597fdddf74eece',
            'node_executable': NODE, 'node_sha256': hashlib.sha256(Path(NODE).read_bytes()).hexdigest(),
            'selected_env': selected_env,
            'environment_note': 'Other environment inherited, not dumped; no external credentials read or used.',
            'files': files}
(ROOT / 'input-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
manifest_sha = hashlib.sha256((ROOT / 'input-manifest.json').read_bytes()).hexdigest()
commands = [
    ('typecheck-01', [NODE, str(MODULES / 'typescript/bin/tsc'), '--noEmit', '-p', 'tsconfig.json']),
    ('independent-01', [NODE, str(MODULES / 'vitest/vitest.mjs'), 'run', '--config', 'vitest.config.mjs',
                        '--configLoader', 'native', '--reporter', 'verbose', 'src/tests/d26-independent.spec.ts']),
]
for name, argv in commands:
    start = time.time()
    with (ROOT / 'logs' / (name + '.log')).open('wb') as log:
        p = subprocess.run(argv, cwd=INPUT, env=env, stdout=log, stderr=subprocess.STDOUT, check=False)
    result = {'argv': argv, 'cwd': str(INPUT), 'selected_env': selected_env,
              'input_manifest_sha256': manifest_sha, 'started_unix': start,
              'finished_unix': time.time(), 'exit': p.returncode,
              'raw_sha256': hashlib.sha256((ROOT / 'logs' / (name + '.log')).read_bytes()).hexdigest()}
    (ROOT / 'logs' / (name + '.json')).write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({'name': name, 'exit': p.returncode, 'raw': str(ROOT / 'logs' / (name + '.log'))}), flush=True)
    if p.returncode != 0:
        raise SystemExit(p.returncode)
for f in files:
    assert hashlib.sha256((ROOT / f['path']).read_bytes()).hexdigest() == f['sha256'], f['path']
(ROOT / 'post-input-check.json').write_text(json.dumps({'input_manifest_sha256': manifest_sha,
                                                      'all_files_unchanged': True,
                                                      'count': len(files)}, indent=2) + '\n')
