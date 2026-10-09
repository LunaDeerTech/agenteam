#!/usr/bin/env python3
"""Independent descriptor projection checks; no files or resources are created.

Preserves the 35 checks first executed from stdin against the frozen repair in
0dad9f04. These checks do not exercise Docker, browser work, or final retirement.
"""
from pathlib import Path
import hashlib
import json

p = Path(__file__).resolve().with_name("owned_resources.py")
raw = p.read_bytes()
scope = {"__name__": "independent_projection"}
exec(compile(raw, str(p), "exec"), scope)
forms = {
    "object": dict(ContainerID="1" * 64, NetworkID="2" * 64, Nonce="a" * 32,
                   PrivateIP="fixture", Directory="fixture", CAFile="fixture",
                   AccessKey="private-probe-marker", SecretKey="private-probe-marker"),
    "outbound": dict(ContainerID="3" * 64, NetworkID="4" * 64, Nonce="b" * 32,
                     ControlPort="9000", PrivateIP="fixture", CAFile="fixture"),
    "postgres": dict(container_id="5" * 64, network_id="6" * 64, nonce="c" * 32,
                     port="5432", image="fixture", user="fixture",
                     password="private-probe-marker", ca_file="fixture", wrong_ca_file="fixture"),
}
checks = 0
resources = {}
for family, form in forms.items():
    encoded = json.dumps(form).encode()
    rows = scope["projection"](encoded, family)
    assert len(rows) == 2 and [x["kind"] for x in rows] == ["container", "network"]
    assert all(set(x) == {"id", "kind", "label", "nonce"} for x in rows)
    assert "private-probe-marker" not in json.dumps(rows)
    scope["merge"](resources, rows)
    checks += 1
    idkey = "container_id" if family == "postgres" else "ContainerID"
    noncekey = "nonce" if family == "postgres" else "Nonce"
    bad = []
    renamed = dict(form)
    renamed[idkey.swapcase()] = renamed.pop(idkey)
    bad.append(json.dumps(renamed).encode())
    for field, value in [(idkey, None), (idkey, "D" * 64), (idkey, "d" * 63), (noncekey, "e" * 31)]:
        changed = dict(form)
        changed[field] = value
        bad.append(json.dumps(changed).encode())
    extra = dict(form, unregistered=True)
    bad.append(json.dumps(extra).encode())
    missing = dict(form)
    missing.pop(idkey)
    bad.append(json.dumps(missing).encode())
    bad += [encoded[:-1] + b',"' + idkey.encode() + b'":"' + b"1" * 64 + b'"}',
            encoded + b"{}", b"\xff" + encoded]
    for candidate in bad:
        try:
            scope["projection"](candidate, family)
        except (ValueError, TypeError):
            checks += 1
        else:
            raise SystemExit("independent descriptor invalid projection admitted")
second = dict(forms["postgres"], container_id="7" * 64)
scope["merge"](resources, scope["projection"](json.dumps(second).encode(), "postgres"))
assert len(resources) == 7 and sum(x["kind"] == "container" for x in resources.values()) == 4
checks += 1
collision = dict(next(iter(resources.values())), nonce="f" * 32)
try:
    scope["merge"](resources, [collision])
except ValueError:
    checks += 1
else:
    raise SystemExit("independent resource conflicting identity admitted")
assert p.read_bytes() == raw
print("INDEPENDENT_DESCRIPTOR_PROJECTION_CHECKS=" + str(checks) + " PASS; NO_FILES_WRITTEN; NO_RESOURCES_STARTED")
print("OWNED_RESOURCES_SHA256=" + hashlib.sha256(raw).hexdigest())
