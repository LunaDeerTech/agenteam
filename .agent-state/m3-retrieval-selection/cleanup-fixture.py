#!/usr/bin/env python3
"""Retire only the exact recorded nonce-owned retrieval fixture."""
import argparse
import json
import pathlib
import subprocess

p = argparse.ArgumentParser()
p.add_argument('--owned', type=pathlib.Path, required=True)
args = p.parse_args()
owned = json.loads(args.owned.read_text())
cid, nid = owned['container_id'], owned['network_id']
container = json.loads(subprocess.check_output(['docker', 'inspect', cid]))[0]
network = json.loads(subprocess.check_output(['docker', 'network', 'inspect', nid]))[0]
key = 'agenteam.m3.retrieval'
assert container['Id'] == cid and container['Config']['Labels'][key] == owned['nonce']
assert network['Id'] == nid and network['Labels'][key] == owned['nonce']
assert set(network.get('Containers', {})) == {cid}
result = subprocess.run(['docker', 'exec', '--user', 'postgres', cid, 'pg_ctl', '-D', '/tmp/retrieval-pg', '-m', 'fast', '-w', '-t', '15', 'stop'], capture_output=True, text=True, timeout=20)
assert result.returncode == 0, result.stderr
subprocess.run(['docker', 'stop', '-t', '5', cid], check=True, capture_output=True, timeout=10)
wait = subprocess.check_output(['docker', 'wait', cid], timeout=5).decode().strip()
subprocess.run(['docker', 'rm', '-v', cid], check=True, capture_output=True, timeout=10)
subprocess.run(['docker', 'network', 'rm', nid], check=True, capture_output=True, timeout=10)
checks = []
for _ in range(2):
    checks.append(all(subprocess.run(command, capture_output=True).returncode != 0 for command in [['docker', 'inspect', cid], ['docker', 'network', 'inspect', nid]]))
assert all(checks)
record = dict(owned=owned, pg_stop_exit=result.returncode, container_wait_exit=int(wait), exact_resource_absent=checks)
args.owned.with_name('cleanup.json').write_text(json.dumps(record, indent=2)+'\n')
print(json.dumps(record))
