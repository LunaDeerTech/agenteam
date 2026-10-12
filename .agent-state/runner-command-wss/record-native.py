#!/usr/bin/python3
"""Thin recorder: validate frozen request, original Popen and actual Wait only."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time


def sha(path):
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
        if sha(directory / name) != expected:
            raise ValueError('frozen recorder identity changed')
    recipe = json.loads((directory / 'invocation.json').read_text())
    inputs = json.loads((directory / 'input-hashes.json').read_text())
    for name, expected in inputs.items():
        path = Path(name)
        if (not path.is_file() or path.is_symlink() or path.resolve(strict=True) != path
                or sha(path) != expected):
            raise ValueError('frozen runtime input changed')
    command = recipe['command']
    output = Path(command[command.index('--output') + 1])
    if output.exists() or output.is_symlink():
        raise ValueError('fresh native namespace required')
    for name in ('process.json', 'terminal.json', 'outer.log'):
        if (directory / name).exists():
            raise ValueError('original recording exists; no retry or overwrite')
    started = time.monotonic()
    with (directory / 'outer.log').open('xb') as log:
        child = subprocess.Popen(command, cwd=recipe['cwd'], env=dict(os.environ, **recipe['environment']),
                                 stdout=log, stderr=subprocess.STDOUT)
        save(directory / 'process.json', {'supervisor_pid': child.pid, 'wrapper_pid': os.getpid()})
        print(json.dumps({'supervisor_pid': child.pid}), flush=True)
        code = child.wait()
    result = {'supervisor_pid': child.pid, 'actual_process_exit': code,
              'elapsed_seconds': round(time.monotonic() - started, 3)}
    save(directory / 'terminal.json', result)
    print(json.dumps(result), flush=True)
    return code


if __name__ == '__main__':
    raise SystemExit(main())
