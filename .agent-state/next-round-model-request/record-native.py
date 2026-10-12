#!/usr/bin/python3
"""Record one authorized original supervisor invocation; no cleanup or gate."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def save(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')


def main():
    directory = Path(__file__).resolve().parent
    identity = json.loads((directory / 'recorder-identity.json').read_text())
    for name, expected in identity['sha256'].items():
        if digest(directory / name) != expected:
            raise ValueError('frozen recorder inputs changed')
    recipe = json.loads((directory / 'invocation.json').read_text())
    inputs = json.loads((directory / 'input-hashes.json').read_text())
    for name, expected in inputs.items():
        path = Path(name)
        if (not path.is_file() or path.is_symlink()
                or path.resolve(strict=True) != path or digest(path) != expected):
            raise ValueError('frozen original runtime input changed')
    command = recipe['command']
    output = Path(command[command.index('--output') + 1])
    if output.exists() or output.is_symlink():
        raise ValueError('fresh original supervisor output required')
    for name in ('process.json', 'terminal.json', 'outer.log'):
        if (directory / name).exists():
            raise ValueError('recording already exists; no retry or overwrite')
    for key in ('GOTMPDIR', 'TMPDIR', 'XDG_CONFIG_HOME', 'XDG_CACHE_HOME', 'DOCKER_CONFIG'):
        Path(recipe['environment'][key]).mkdir(mode=0o700)
    environment = os.environ.copy()
    environment.update(recipe['environment'])
    started = time.monotonic()
    with (directory / 'outer.log').open('xb') as log:
        child = subprocess.Popen(command, cwd=recipe['cwd'], env=environment,
                                 stdout=log, stderr=subprocess.STDOUT)
        save(directory / 'process.json', {'pid': child.pid,
                                         'started_monotonic': started})
        print(json.dumps({'supervisor_pid': child.pid}), flush=True)
        code = child.wait()
    terminal = {'supervisor_pid': child.pid, 'actual_process_exit': code,
                'elapsed_seconds': round(time.monotonic() - started, 3)}
    save(directory / 'terminal.json', terminal)
    print(json.dumps(terminal), flush=True)
    return code


if __name__ == '__main__':
    raise SystemExit(main())
