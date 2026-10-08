import json
import os
import pathlib
import re
import sys

root = pathlib.Path(__file__).resolve().parent
repository = pathlib.Path('/workspace/agenteam')
required = re.findall(r'^\s+([^\s]+) (v[^\s]+)', (repository / 'go.mod').read_text(), re.M)
sums = {(name, version): value for name, version, value in (line.split() for line in (repository / 'go.sum').read_text().splitlines())}
assert len(required) == 31
for name, version in required:
    assert (name, version) in sums and (name, version + '/go.mod') in sums
meta = json.loads((root / 'evidence/root-go-download.meta.json').read_text())
argv = meta['argv'][:-1] + [name + '@' + version for name, version in required]
assert meta['argv'][-1] == 'all'
env = {key: (os.environ[key] if value == '<inherited-present>' else value) for key, value in meta['env'].items() if value != '<inherited-present>' or key in os.environ}
os.chdir(repository)
os.execve(sys.executable, [sys.executable, str(root / 'run.py'), 'root-go-download02', *argv], env)
