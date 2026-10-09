"""Safe ID projection of the three existing, differently encoded descriptors."""
import json
import os
from pathlib import Path
import re
import stat

SCHEMAS = {
    "object": ("agenteam.d05.objectfixture", {"ContainerID", "NetworkID", "Nonce", "PrivateIP", "Directory", "CAFile", "AccessKey", "SecretKey"}),
    "outbound": ("agenteam.d04.networkfixture", {"ContainerID", "NetworkID", "Nonce", "ControlPort", "PrivateIP", "CAFile"}),
    "postgres": ("agenteam.d03.fixture", {"container_id", "network_id", "nonce", "port", "image", "user", "password", "ca_file", "wrong_ca_file"}),
}
PATTERNS = (
    ("object", "agenteam-d05-object-*/descriptor.json"),
    ("outbound", "agenteam-d05-object-*/agenteam-d04-net-*/fixture.json"),
    ("postgres", "agenteam-d05-object-*/agenteam-d04-net-*/agenteam-d03-*/fixture.json"),
    ("postgres", "agenteam-d05-object-*/agenteam-d04-net-*/agenteam-d03-*/unsupported-fixture.json"),
)


def closed_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("owned descriptor duplicate member")
        result[key] = value
    return result


def projection(raw, family):
    descriptor = json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=closed_object)
    label, keys = SCHEMAS[family]
    if not isinstance(descriptor, dict) or set(descriptor) != keys:
        raise ValueError("owned descriptor closed shape invalid")
    lower = family == "postgres"
    nonce = descriptor["nonce" if lower else "Nonce"]
    if not isinstance(nonce, str) or re.fullmatch(r"[0-9a-f]{32}", nonce) is None:
        raise ValueError("owned descriptor nonce invalid")
    rows = []
    for kind, field in (("container", "container_id" if lower else "ContainerID"), ("network", "network_id" if lower else "NetworkID")):
        identifier = descriptor[field]
        if not isinstance(identifier, str) or re.fullmatch(r"[0-9a-f]{64}", identifier) is None:
            raise ValueError("owned descriptor ID invalid")
        rows.append({"id": identifier, "kind": kind, "label": label, "nonce": nonce})
    return rows


def read_descriptor(path, family, private):
    path, private = Path(path), Path(private).resolve()
    if not path.resolve().is_relative_to(private):
        raise ValueError("owned descriptor path invalid")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600 or not 0 < info.st_size <= 16384:
            raise ValueError("owned descriptor file invalid")
        raw = b""
        while len(raw) <= 16384:
            chunk = os.read(fd, 16385 - len(raw))
            if not chunk:
                break
            raw += chunk
        if len(raw) > 16384:
            raise ValueError("owned descriptor file oversized")
        rows = projection(raw, family)
        if rows[0]["nonce"] not in path.parent.name:
            raise ValueError("owned descriptor directory nonce invalid")
        return rows
    finally:
        os.close(fd)


def collect(private):
    rows = []
    for family, pattern in PATTERNS:
        for path in Path(private).glob(pattern):
            try:
                rows.extend(read_descriptor(path, family, private))
            except (FileNotFoundError, ValueError, OSError):
                # Existing helpers publish after a normal WriteFile, not an
                # atomic rename. Only complete validated snapshots count.
                continue
    return rows


def merge(resources, rows):
    changed = False
    for row in rows:
        previous = resources.get(row["id"])
        if previous is not None and previous != row:
            raise ValueError("owned resource identity changed")
        if previous is None:
            resources[row["id"]] = row
            changed = True
    return changed


def publish(path, resources):
    path = Path(path)
    temporary = path.with_suffix(".tmp")
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, "w") as output:
            json.dump(list(resources.values()), output)
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()
