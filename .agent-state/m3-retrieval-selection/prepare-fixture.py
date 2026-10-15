#!/usr/bin/env python3
"""Reproduce the isolated candidate fixture; never use an existing database.

Dependencies are confined to the new container. Downloaded artifacts stay in an
explicit shared cache; the three primary artifacts must match sources.json.
"""
import argparse
import hashlib
import json
import os
import pathlib
import subprocess
import urllib.request
import uuid

HERE = pathlib.Path(__file__).resolve().parent
IMAGE = 'pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc'
PACKAGES = ['cmake=3.25.1-1', 'make=4.3-4.1', 'gcc=4:12.2.0-3', 'g++=4:12.2.0-3', 'pkg-config=1.8.1-1', 'meson=1.0.1-5', 'ninja-build=1.11.1-2~deb12u1', 'libmsgpack-dev=4.0.0-3', 'libopenblas0=0.3.21+ds-4', 'libxxhash-dev=0.8.1-1']


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--cache', type=pathlib.Path, required=True)
    ap.add_argument('--output', type=pathlib.Path, required=True)
    args = ap.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    args.cache.mkdir(parents=True, exist_ok=True)
    artifacts = json.loads((HERE/'sources.json').read_text())['artifacts'] + [dict(name='pg17dev.deb', url='https://apt-archive.postgresql.org/pub/repos/apt/pool/main/p/postgresql-17/postgresql-server-dev-17_17.8-1.pgdg12%2B1_amd64.deb', bytes=1332112, sha256='4a0f84a2714b85ecd8d051effb82b92d2cce5606b23f9acf58e0befb14c3725e')]
    for item in artifacts:
        target = args.cache/item['name']
        if not target.exists():
            # urllib uses the inherited default proxy and verifies TLS.
            with urllib.request.urlopen(item['url'], timeout=60) as response:
                data = response.read(item['bytes']+1)
            assert len(data) == item['bytes'] and hashlib.sha256(data).hexdigest() == item['sha256']
            with target.open('xb') as stream:
                stream.write(data)
        assert target.stat().st_size == item['bytes']
        assert hashlib.sha256(target.read_bytes()).hexdigest() == item['sha256']
    subprocess.run(['docker', 'image', 'inspect', IMAGE], check=True, stdout=subprocess.DEVNULL)
    nonce = uuid.uuid4().hex
    name, label = 'm3-retrieval-'+nonce, 'agenteam.m3.retrieval='+nonce
    nid = subprocess.check_output(['docker', 'network', 'create', '--label', label, name], text=True).strip()
    owned = dict(nonce=nonce, name=name, network_id=nid, container_id=None)
    record = args.output/'owned.json'
    record.write_text(json.dumps(owned, indent=2)+'\n')
    cid = subprocess.check_output(['docker', 'run', '-d', '--name', name, '--label', label, '--network', nid, '--memory', '3g', '--cpus', '2', '--mount', 'type=bind,source='+str(args.cache.resolve())+',target=/deps,readonly', '--entrypoint', 'sleep', IMAGE, '3600'], text=True).strip()
    owned['container_id'] = cid
    record.write_text(json.dumps(owned, indent=2)+'\n')
    subprocess.run(['docker', 'cp', '/etc/ssl/certs/ca-certificates.crt', cid+':/tmp/host-ca.crt'], check=True)
    subprocess.run(['docker', 'cp', str(HERE/'build-groonga.sh'), cid+':/tmp/build-groonga.sh'], check=True)
    env_args = []
    for key in ['HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY', 'http_proxy', 'https_proxy', 'no_proxy']:
        if key in os.environ:
            env_args += ['-e', key]
    stages = [
        ('apt', 180, ['bash', '-euc', 'sed -i "s|http://|https://|g" /etc/apt/sources.list.d/pgdg.list /etc/apt/sources.list.d/debian.sources; apt-get -o Acquire::https::CaInfo=/tmp/host-ca.crt -o APT::Update::Error-Mode=any update -qq; apt-get -o Acquire::https::CaInfo=/tmp/host-ca.crt install -y --no-install-recommends --no-upgrade '+' '.join(PACKAGES)]),
        ('build', 420, ['bash', '/tmp/build-groonga.sh']),
        ('pg', 60, ['bash', '-euc', 'dpkg -i /deps/postgresql-17-pg-search_0.26.1-1PARADEDB-bookworm_amd64.deb; install -d -o postgres -g postgres /tmp/retrieval-pg /tmp/retrieval-socket; gosu postgres initdb -D /tmp/retrieval-pg --encoding=UTF8 --locale=C --auth-local=trust --auth-host=reject; gosu postgres pg_ctl -D /tmp/retrieval-pg -l /tmp/retrieval-pg.log -o "-c listen_addresses= -c unix_socket_directories=/tmp/retrieval-socket -c shared_preload_libraries=pg_search -c max_connections=20 -c shared_buffers=128MB" -w start; gosu postgres psql -h /tmp/retrieval-socket -d postgres -X -v ON_ERROR_STOP=1 -c "CREATE EXTENSION vector; CREATE EXTENSION pg_search;"']),
    ]
    for stage, seconds, command in stages:
        with (args.output/(stage+'.log')).open('w') as log:
            result = subprocess.run(['docker', 'exec']+env_args+[cid, 'timeout', '--kill-after=5s', str(seconds)+'s']+command, stdout=log, stderr=subprocess.STDOUT, timeout=seconds+15)
        (args.output/(stage+'-exit.json')).write_text(json.dumps({'exit_code':result.returncode})+'\n')
        assert result.returncode == 0, f'{stage} failed; preserve owned.json and retire its exact resources'
    print(json.dumps(owned))


if __name__ == '__main__':
    main()
