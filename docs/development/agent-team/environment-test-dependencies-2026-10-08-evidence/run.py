#!/usr/bin/env python3
"""Capture one dependency command and its actual wait result; no services."""
import hashlib
import json
import os
import pathlib
import subprocess
import sys
import time

root = pathlib.Path(__file__).resolve().parent
evidence = root / 'evidence'
evidence.mkdir(exist_ok=True)
name, *argv = sys.argv[1:]
if not argv or not name.replace('-', '').replace('_', '').isalnum():
    raise SystemExit('usage: run.py NAME COMMAND [ARG ...]')
raw = evidence / (name + '.raw')
meta_path = evidence / (name + '.meta.json')
if raw.exists() or meta_path.exists():
    raise SystemExit('evidence already exists: ' + name)
keys = ('PATH', 'LANG', 'TMPDIR', 'GOTOOLCHAIN', 'GOENV', 'GOWORK', 'GOPROXY',
        'GOSUMDB', 'GOMODCACHE', 'GOCACHE', 'GOOS', 'GOARCH', 'GOAMD64',
        'CGO_ENABLED', 'GOTELEMETRY', 'GOFLAGS', 'GOPATH', 'GOROOT',
        'NODE_PATH', 'PLAYWRIGHT_BROWSERS_PATH')
env = {key: os.environ[key] for key in keys if key in os.environ}
for key in ('HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY', 'http_proxy', 'https_proxy', 'no_proxy'):
    if key in os.environ:
        env[key] = '<inherited-present>'
meta = {'argv': argv, 'cwd': os.getcwd(), 'env': env, 'started_ns': time.time_ns()}
with raw.open('xb') as output:
    process = subprocess.Popen(argv, stdout=output, stderr=subprocess.STDOUT)
    meta['pid'] = process.pid
    meta_path.write_text(json.dumps(meta, indent=2) + '\n')
    print(json.dumps({'started': name, 'pid': process.pid}), flush=True)
    meta['exit_code'] = process.wait()
meta['ended_ns'] = time.time_ns()
meta['elapsed_seconds'] = (meta['ended_ns'] - meta['started_ns']) / 1e9
meta['raw_sha256'] = hashlib.sha256(raw.read_bytes()).hexdigest()
meta_path.write_text(json.dumps(meta, indent=2) + '\n')
print(json.dumps({'finished': name, 'exit_code': meta['exit_code'], 'elapsed_seconds': meta['elapsed_seconds'], 'raw': str(raw)}), flush=True)
raise SystemExit(meta['exit_code'])
