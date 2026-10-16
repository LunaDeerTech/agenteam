#!/usr/bin/env python3
"""Prepare an independent persistent Central deployment; never start services.

The owner runs the generated compose.json services individually and loads env.json
into the native Central child's environment. Do not shell-source it or print
expanded Compose configuration. runtime.json is the final publication marker.
An existing complete preparation is verified and reused; an incomplete directory
or changed input is refused without replacing keys, credentials or application
data. Keep the entire state directory, including object-spool.processes.

The PostgreSQL container starts as its image's root, copies read-only TLS/HBA
inputs into a private tmpfs, chowns those copies to its actual postgres account,
then execs the official entrypoint. No host chown or assumed numeric PG UID is
needed. Central and MinIO use the preparing user's UID/GID. The native Central
continues to use the host's machine-id; this program does not alter it.

Only the fixed account-preview frontend at 127.0.0.1:5245 is proxied. Its process
and source, public frp mapping, service startup, bootstrap credential extraction,
resource-ID recording and all SMTP actions remain the deployment owner's work.
"""

import argparse
import base64
import datetime as dt
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import stat
import subprocess
import sys


FORMAT = 1
ORIGIN = "https://154.12.86.233:18443"
PUBLIC_AUTHORITY = "154.12.86.233:18443"
CENTRAL_ADDRESS = "127.0.0.1:5257"
TLS_ADDRESS = "127.0.0.1:5256"
PG_IMAGE = "pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc"
RUNTIME_IMAGE = "nginx@sha256:30f1c0d78e0ad60901648be663a710bdadf19e4c10ac6782c235200619158284"
MINIO_SHA256 = "dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8"
LABEL = "io.agenteam.persistent-central"
PREFIX = "AGENTEAM_CENTRAL_"


class PrepareError(Exception):
    """Only fixed, nonsecret error codes cross the CLI boundary."""


def require(condition, code):
    if not condition:
        raise PrepareError(code)


def canonical_path(raw):
    path = Path(raw)
    require(path.is_absolute() and str(path) == raw and raw != "/", "INVALID_ABSOLUTE_PATH")
    require(os.path.normpath(raw) == raw, "NONCANONICAL_PATH")
    # Compose interpolates dollar signs even in JSON; forbid ambiguous inputs.
    require(not any(ord(c) < 32 or ord(c) == 127 for c in raw) and "$" not in raw,
            "UNSUPPORTED_PATH_CHARACTER")
    for component in [*reversed(path.parents), path]:
        try:
            info = component.lstat()
        except FileNotFoundError:
            continue
        require(not stat.S_ISLNK(info.st_mode), "SYMLINK_PATH_REFUSED")
    return path


def private_directory(path, create=False):
    if create:
        try:
            path.mkdir(mode=0o700)
        except FileExistsError:
            pass
    info = path.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == 0o700, "UNSAFE_PRIVATE_DIRECTORY")


def private_file(path, executable=False):
    info = path.lstat()
    expected = 0o700 if executable else 0o600
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
            and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == expected,
            "UNSAFE_PRIVATE_FILE")


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def binary_input(raw):
    path = canonical_path(raw)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_size > 0
            and bool(info.st_mode & 0o111), "INVALID_BINARY_INPUT")
    return {"path": str(path), "sha256": digest(path)}


def write_file(path, contents, mode=0o600):
    if isinstance(contents, str):
        contents = contents.encode("utf-8")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, "wb") as output:
        output.write(contents)
        output.flush()
        os.fsync(output.fileno())


def write_json(path, value):
    write_file(path, json.dumps(value, indent=2, sort_keys=True, ensure_ascii=True) + "\n")


def read_json(path):
    private_file(path)
    require(path.stat().st_size <= 1024 * 1024, "OVERSIZED_PREPARATION_FILE")
    with path.open("r", encoding="utf-8") as source:
        return json.load(source)


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def copy_binary(source, target):
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o700)
    with os.fdopen(fd, "wb") as output, Path(source["path"]).open("rb") as original:
        shutil.copyfileobj(original, output, 1024 * 1024)
        output.flush()
        os.fsync(output.fileno())
    require(digest(target) == source["sha256"], "BINARY_CHANGED_DURING_PREPARATION")


def openssl(command, *args, data=None):
    result = subprocess.run([command, *map(str, args)], input=data,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            env={"PATH": os.defpath, "LC_ALL": "C"}, timeout=30,
                            check=False)
    require(result.returncode == 0, "OPENSSL_FAILED")
    return result.stdout


def prepare_certificates(stage, command):
    tls = stage / "tls"
    for name in ("ca", "postgres", "minio", "proxy"):
        private_directory(tls / name, create=True)
    ca_key, ca_cert = tls / "ca/private.key", tls / "ca/ca.crt"
    openssl(command, "genpkey", "-algorithm", "EC", "-pkeyopt", "ec_paramgen_curve:P-256",
            "-out", ca_key)
    openssl(command, "req", "-new", "-x509", "-sha256", "-days", "30", "-key", ca_key,
            "-out", ca_cert, "-subj", "/CN=Agenteam persistent deployment private CA",
            "-addext", "basicConstraints=critical,CA:TRUE,pathlen:0",
            "-addext", "keyUsage=critical,keyCertSign,cRLSign")
    for name in ("postgres", "minio", "proxy"):
        folder = tls / name
        key, cert, request = folder / "private.key", folder / "public.crt", folder / "request.csr"
        extension = folder / "extensions.cnf"
        sans = "DNS:localhost,IP:127.0.0.1"
        if name == "proxy":
            sans += ",IP:154.12.86.233"
        write_file(extension, "basicConstraints=critical,CA:FALSE\n"
                   "keyUsage=critical,digitalSignature\n"
                   "extendedKeyUsage=serverAuth\nsubjectAltName=" + sans + "\n")
        openssl(command, "genpkey", "-algorithm", "EC", "-pkeyopt", "ec_paramgen_curve:P-256",
                "-out", key)
        openssl(command, "req", "-new", "-sha256", "-key", key, "-out", request,
                "-subj", "/CN=Agenteam " + name)
        openssl(command, "x509", "-req", "-sha256", "-days", "30", "-in", request,
                "-CA", ca_cert, "-CAkey", ca_key, "-set_serial", "0x" + secrets.token_hex(16),
                "-extfile", extension, "-out", cert)
        openssl(command, "verify", "-CAfile", ca_cert, "-purpose", "sslserver",
                "-verify_ip", "154.12.86.233" if name == "proxy" else "127.0.0.1", cert)
        openssl(command, "verify", "-CAfile", ca_cert, "-verify_hostname", "localhost", cert)
        request.unlink()
        extension.unlink()
    for path in tls.rglob("*"):
        if path.is_file():
            os.chmod(path, 0o600)
            private_file(path)
            with path.open("rb") as source:
                os.fsync(source.fileno())
    public = openssl(command, "x509", "-in", tls / "proxy/public.crt", "-pubkey", "-noout")
    der = openssl(command, "pkey", "-pubin", "-outform", "DER", data=public)
    return base64.b64encode(hashlib.sha256(der).digest()).decode("ascii")


def keyrings():
    values = {}
    for purpose in ("CURSOR", "SECRET", "OBJECT_DOWNLOAD", "ACCOUNT", "KNOWLEDGE_CONFIRMATION"):
        versioned = purpose == "SECRET"
        name = "version" if versioned else "kid"
        current = "current_version" if versioned else "current_kid"
        identifier = "1" if versioned else purpose.lower() + "1"
        ring = {"format": 1, current: identifier,
                "keys": [{name: identifier, "key_b64": base64.b64encode(secrets.token_bytes(32)).decode("ascii")}]}
        values[PREFIX + purpose + "_KEYRING"] = json.dumps(ring, separators=(",", ":"))
    return values


def mount(source, target, readonly=True):
    return {"type": "bind", "source": str(source), "target": target,
            "read_only": readonly, "bind": {"create_host_path": False}}


PG_START = """#!/bin/sh
set -eu
umask 077
# Root in the fixed image can read the owner's bind-mounted 0600 inputs.
# Resolve the image's actual postgres account; do not chown host TLS files.
work=/run/agenteam-postgres-tls
mkdir -p "$work"
cp /run/agenteam-input/tls/public.crt "$work/server.crt"
cp /run/agenteam-input/tls/private.key "$work/server.key"
cp /run/agenteam-input/pg_hba.conf "$work/pg_hba.conf"
cp /run/agenteam-input/postgres-password "$work/postgres-password"
chmod 700 "$work"
chmod 600 "$work/server.crt" "$work/server.key" "$work/pg_hba.conf" "$work/postgres-password"
chown postgres:postgres "$work" "$work/server.crt" "$work/server.key" "$work/pg_hba.conf" "$work/postgres-password"
POSTGRES_PASSWORD_FILE="$work/postgres-password"
export POSTGRES_PASSWORD_FILE
exec docker-entrypoint.sh postgres \\
  -c ssl=on \\
  -c ssl_min_protocol_version=TLSv1.2 \\
  -c ssl_cert_file="$work/server.crt" \\
  -c ssl_key_file="$work/server.key" \\
  -c hba_file="$work/pg_hba.conf" \\
  -c password_encryption=scram-sha-256
"""

PG_HBA = """# Unix socket bootstrap is confined to this private database container.
local all all trust
hostnossl all all 0.0.0.0/0 reject
hostnossl all all ::/0 reject
hostssl all all 0.0.0.0/0 scram-sha-256
hostssl all all ::/0 scram-sha-256
"""


def nginx_configuration(nonce, revision):
    identity = json.dumps({"nonce": nonce, "source_revision": revision}, separators=(",", ":"))
    return """worker_processes 1;
pid /tmp/agenteam-nginx.pid;
error_log /dev/null crit;
events { worker_connections 1024; }
http {
    access_log off;
    server_tokens off;
    map $http_host $agenteam_host_allowed {
        default 0;
        "154.12.86.233:18443" 1;
    }
    map $http_upgrade $agenteam_connection_upgrade {
        default upgrade;
        '' close;
    }
    server {
        listen 127.0.0.1:5256 ssl;
        server_name 154.12.86.233;
        ssl_certificate /run/agenteam-tls/public.crt;
        ssl_certificate_key /run/agenteam-tls/private.key;
        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_session_tickets off;
        client_max_body_size 8m;
        if ($agenteam_host_allowed = 0) { return 421; }
        location = /__instance__/identity {
            default_type application/json;
            add_header Cache-Control "no-store" always;
            return 200 '""" + identity + """';
        }
        location /api/v1 {
            proxy_pass http://127.0.0.1:5257;
            proxy_http_version 1.1;
            proxy_set_header Host $http_host;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection $agenteam_connection_upgrade;
            proxy_buffering off;
            proxy_next_upstream off;
            proxy_intercept_errors off;
            proxy_redirect off;
            proxy_read_timeout 60s;
        }
        location ~ ^/(livez|readyz|diagnostics)$ {
            proxy_pass http://127.0.0.1:5257;
            proxy_set_header Host $http_host;
            proxy_next_upstream off;
        }
        location / {
            proxy_pass http://127.0.0.1:5245;
            proxy_http_version 1.1;
            proxy_set_header Host $http_host;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection $agenteam_connection_upgrade;
            proxy_buffering off;
            proxy_next_upstream off;
            proxy_redirect off;
            proxy_read_timeout 60s;
        }
    }
}
"""


def compose_configuration(root, project, labels, uid, gid):
    common = {"image": RUNTIME_IMAGE, "pull_policy": "never", "restart": "no", "labels": labels}
    return {
        "name": project,
        "services": {
            "database": {
                **common, "image": PG_IMAGE, "user": "0:0",
                "entrypoint": ["/bin/sh", "/run/agenteam-input/postgres-start.sh"],
                "environment": {
                    "POSTGRES_USER": "agenteam", "POSTGRES_DB": "agenteam",
                    "POSTGRES_PASSWORD_FILE": "/run/agenteam-input/postgres-password",
                    "POSTGRES_HOST_AUTH_METHOD": "scram-sha-256",
                    "POSTGRES_INITDB_ARGS": "--auth-local=trust --auth-host=scram-sha-256",
                    "PGDATA": "/var/lib/postgresql/data/pgdata",
                },
                "ports": ["127.0.0.1:5456:5432"],
                "volumes": [mount(root / "data/postgres", "/var/lib/postgresql/data", False),
                            mount(root / "config/postgres-start.sh", "/run/agenteam-input/postgres-start.sh"),
                            mount(root / "config/pg_hba.conf", "/run/agenteam-input/pg_hba.conf"),
                            mount(root / "secrets/postgres-password", "/run/agenteam-input/postgres-password"),
                            mount(root / "tls/postgres", "/run/agenteam-input/tls")],
                "tmpfs": ["/run/agenteam-postgres-tls:rw,noexec,nosuid,nodev,mode=0700"],
                "stop_grace_period": "40s",
            },
            "minio": {
                **common, "user": str(uid) + ":" + str(gid),
                "entrypoint": ["/usr/local/bin/agenteam-minio"],
                "command": ["--quiet", "server", "/data", "--address", ":9000",
                            "--console-address", ":9001", "--certs-dir", "/run/agenteam-certs"],
                "env_file": [str(root / "secrets/minio.env")],
                "ports": ["127.0.0.1:9456:9000"],
                "volumes": [mount(root / "bin/minio", "/usr/local/bin/agenteam-minio"),
                            mount(root / "tls/minio", "/run/agenteam-certs"),
                            mount(root / "data/minio", "/data", False)],
                "stop_grace_period": "40s",
            },
            "web": {
                **common, "network_mode": "host",
                "entrypoint": ["nginx"],
                "command": ["-c", "/etc/nginx/nginx.conf", "-g", "daemon off;"],
                "volumes": [mount(root / "config/nginx.conf", "/etc/nginx/nginx.conf"),
                            mount(root / "tls/proxy", "/run/agenteam-tls")],
                "stop_grace_period": "15s",
            },
        },
        "networks": {"default": {"driver": "bridge", "labels": labels}},
    }


def new_preparation(root, inputs, command):
    stage = root / ".preparing"
    stage.mkdir(mode=0o700)
    directories = ("bin", "config", "secrets", "tls", "data", "data/postgres", "data/minio",
                   "central", "central/object-spool", "central/object-spool.processes", "central/account-recovery")
    for name in directories:
        private_directory(stage / name, create=True)
    copy_binary(inputs["central_binary"], stage / "bin/agenteam")
    copy_binary(inputs["minio_binary"], stage / "bin/minio")
    spki = prepare_certificates(stage, command)
    nonce = secrets.token_hex(16)
    project = "agenteam-persistent-" + nonce[:12]
    labels = {LABEL: nonce, "io.agenteam.source-revision": inputs["source_revision"]}
    password, minio_access, minio_secret = secrets.token_hex(32), secrets.token_hex(16), secrets.token_hex(32)
    rings = keyrings()
    write_json(stage / "secrets/keyrings.json", rings)
    write_file(stage / "secrets/postgres-password", password + "\n")
    write_file(stage / "secrets/minio.env", "MINIO_ROOT_USER=" + minio_access + "\nMINIO_ROOT_PASSWORD=" + minio_secret
               + "\nMINIO_BROWSER=off\nMINIO_UPDATE=off\nMINIO_CONFIG_ENV_FILE=/dev/null\nHOME=/data\n")
    env = {PREFIX + key: value for key, value in {
        "LOG_LEVEL": "info", "SHUTDOWN_TIMEOUT": "30s", "HTTP_ADDR": CENTRAL_ADDRESS,
        "PUBLIC_ORIGIN": ORIGIN, "DEV_MODE": "false",
        "DATABASE_URL": "postgresql://agenteam:" + password + "@127.0.0.1:5456/agenteam",
        "DATABASE_TLS_MODE": "verify-full", "DATABASE_CA_FILE": str(root / "tls/ca/ca.crt"),
        "DATABASE_MAX_CONNS": "10", "DATABASE_CONNECT_TIMEOUT": "5s",
        "DATABASE_STARTUP_TIMEOUT": "2m", "DATABASE_LOCK_TIMEOUT": "5s",
        "OBJECT_ENDPOINT": "https://127.0.0.1:9456", "OBJECT_BUCKET": "agenteam-central",
        "OBJECT_ACCESS_KEY": minio_access, "OBJECT_SECRET_KEY": minio_secret,
        "OBJECT_TLS_MODE": "verify-full", "OBJECT_CA_FILE": str(root / "tls/ca/ca.crt"),
        "OBJECT_SPOOL_DIR": str(root / "central/object-spool"),
        "ACCOUNT_RECOVERY_LOG": str(root / "central/account-recovery/recovery.jsonl"),
    }.items()}
    env.update(rings)
    write_json(stage / "env.json", env)
    write_file(stage / "central/account-recovery/recovery.jsonl", "")
    write_file(stage / "config/postgres-start.sh", PG_START)
    write_file(stage / "config/pg_hba.conf", PG_HBA)
    write_file(stage / "config/nginx.conf", nginx_configuration(nonce, inputs["source_revision"]))
    write_json(stage / "compose.json", compose_configuration(root, project, labels, inputs["uid"], inputs["gid"]))
    write_json(stage / "resource-ids.json", {"format": FORMAT, "nonce": nonce,
               "compose_project": project, "containers": {}, "networks": {}})
    runtime = {
        "format": FORMAT, "source_revision": inputs["source_revision"], "origin": ORIGIN,
        "local_tls_address": TLS_ADDRESS, "central_address": CENTRAL_ADDRESS,
        "compose_file": str(root / "compose.json"), "compose_project": project,
        "central_binary": str(root / "bin/agenteam"), "central_binary_sha256": inputs["central_binary"]["sha256"],
        "environment_file": str(root / "env.json"), "certificate_ca_file": str(root / "tls/ca/ca.crt"),
        "certificate_file": str(root / "tls/proxy/public.crt"), "certificate_spki": spki,
        "nonce": nonce, "prepared_at": dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z"),
        "credentials_file": None, "resource_ids_file": str(root / "resource-ids.json"),
        "recovery_log": env[PREFIX + "ACCOUNT_RECOVERY_LOG"], "labels": labels,
        "service_names": ["database", "minio", "web"], "inputs": inputs,
    }
    mutable = {"resource-ids.json", "central/account-recovery/recovery.jsonl"}
    immutable = {}
    for path in sorted(stage.rglob("*")):
        relative = path.relative_to(stage).as_posix()
        if path.is_file() and relative not in mutable:
            private_file(path, executable=relative.startswith("bin/"))
            immutable[relative] = {"sha256": digest(path), "mode": "0700" if relative.startswith("bin/") else "0600"}
    # This manifest freezes inputs and generated material, excluding mutable data,
    # owner-recorded resource IDs and later bootstrap credentials/status.
    core = {key: value for key, value in runtime.items() if key != "credentials_file"}
    write_json(stage / "preparation.json", {"format": FORMAT, "inputs": inputs,
               "runtime_core": core, "immutable_files": immutable})
    write_json(stage / "runtime.json", runtime)
    for folder in sorted((p for p in stage.rglob("*") if p.is_dir()), key=lambda p: len(p.parts), reverse=True):
        sync_directory(folder)
    sync_directory(stage)
    for path in sorted(stage.iterdir()):
        if path.name != "runtime.json":
            os.rename(path, root / path.name)
    sync_directory(root)
    os.rename(stage / "runtime.json", root / "runtime.json")
    sync_directory(root)
    stage.rmdir()
    sync_directory(root)


def verify_existing(root, inputs):
    manifest = read_json(root / "preparation.json")
    require(manifest.get("format") == FORMAT and manifest.get("inputs") == inputs, "PREPARATION_INPUT_CHANGED")
    runtime = read_json(root / "runtime.json")
    require(all(runtime.get(k) == v for k, v in manifest["runtime_core"].items()), "RUNTIME_DESCRIPTOR_CHANGED")
    for relative, expected in manifest["immutable_files"].items():
        path = canonical_path(str(root / relative))
        require(path.is_relative_to(root), "INVALID_PREPARATION_MANIFEST")
        private_file(path, executable=expected["mode"] == "0700")
        require(digest(path) == expected["sha256"], "PREPARED_FILE_CHANGED")
    # Application data and recorded IDs can change normally after startup.
    for relative in ("bin", "config", "secrets", "tls", "central", "central/object-spool",
                     "central/object-spool.processes", "central/account-recovery"):
        private_directory(canonical_path(str(root / relative)))
    private_file(root / "resource-ids.json")
    private_file(root / "central/account-recovery/recovery.jsonl")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--state-dir", required=True)
    parser.add_argument("--central-binary", required=True)
    parser.add_argument("--minio-binary", required=True)
    parser.add_argument("--source-revision", required=True)
    args = parser.parse_args()
    os.umask(0o077)
    root = canonical_path(args.state_dir)
    require(re.fullmatch(r"[0-9a-f]{40}", args.source_revision) is not None, "INVALID_SOURCE_REVISION")
    central, minio = binary_input(args.central_binary), binary_input(args.minio_binary)
    require(minio["sha256"] == MINIO_SHA256, "MINIO_BINARY_PIN_MISMATCH")
    command = shutil.which("openssl")
    require(command is not None, "OPENSSL_UNAVAILABLE")
    inputs = {"state_dir": str(root), "source_revision": args.source_revision,
              "central_binary": central, "minio_binary": minio, "uid": os.geteuid(), "gid": os.getegid()}
    require(root.parent.is_dir(), "STATE_PARENT_MISSING")
    private_directory(root, create=True)
    lock_path = root / ".prepare.lock"
    fd = os.open(lock_path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        private_file(lock_path)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise PrepareError("PREPARATION_BUSY") from error
        reused = (root / "runtime.json").exists()
        if reused:
            verify_existing(root, inputs)
        else:
            require({path.name for path in root.iterdir()} == {".prepare.lock"}, "INCOMPLETE_STATE_REQUIRES_OWNER_REVIEW")
            new_preparation(root, inputs, command)
            verify_existing(root, inputs)
        # Only locations and lifecycle facts are printed; credentials stay private.
        print(json.dumps({"prepared": True, "reused": reused, "runtime_file": str(root / "runtime.json")}, separators=(",", ":")))
    finally:
        os.close(fd)


if __name__ == "__main__":
    try:
        main()
    except PrepareError as error:
        print("PREPARE_FAILED: " + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        # Never expose OS/process diagnostics containing private paths or inputs.
        print("PREPARE_FAILED: PREPARATION_IO_OR_FORMAT_ERROR", file=sys.stderr)
        sys.exit(1)
