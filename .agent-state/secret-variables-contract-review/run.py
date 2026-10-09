#!/usr/bin/env python3
"""Read-only Secret A review; --go overlays only an independent external test.

Run from any directory. Requires local Go1.27.1 and installed jsonschema; never
downloads dependencies. Target source is read only; output/cache belong to the
Variables UI tree. This is pure-contract evidence, not a runtime authorization.
"""
import argparse
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile


def schema_checks(target):
    from jsonschema import Draft202012Validator
    from referencing import Registry, Resource
    from referencing.jsonschema import DRAFT202012

    path = target / "api/openapi/secret-variables.json"
    doc = json.loads(path.read_text())
    common = target / "api/openapi/common.json"
    registry = Registry().with_resources([
        (path.as_uri(), Resource(json.loads(path.read_text()), DRAFT202012)),
        (common.as_uri(), Resource(json.loads(common.read_text()), DRAFT202012)),
    ])
    count = 0
    failures = []

    def check(name, instance, valid):
        nonlocal count
        schema = {"$ref": path.as_uri() + "#/components/schemas/" + name}
        got = Draft202012Validator(schema, registry=registry).is_valid(instance)
        if got != valid:
            failures.append((name, "positive" if valid else "negative", count))
        count += 1

    def variant(value, key, item):
        result = copy.deepcopy(value)
        result[key] = item
        return result

    for schema in doc["components"]["schemas"].values():
        Draft202012Validator.check_schema(schema)
    identity = "01900000-0000-7000-8000-000000000001"
    at = "2026-10-09T00:00:00.000000Z"
    metadata = dict(id=identity, project_id=identity, type="secret", name="KEY",
                    description="", version="1", created_at=at, updated_at=at)
    request = dict(variable_id=identity, name="KEY", description="", value="synthetic-fixture")
    for name in ["SecretVariable", "SecretVariableSummary"]:
        check(name, metadata, True)
        for key in ["value", "semantic_digest", "credential_ref", "masked_value", "value_hash", "value_length"]:
            check(name, variant(metadata, key, "synthetic-fixture"), False)
        check(name, variant(metadata, "type", "variable"), False)
    check("SecretVariableCreateBody", {"request": request}, True)
    check("SecretVariableCreateBody", {"request": request, "expected_version": "1"}, False)
    for key, value in [("value", ""), ("value", None), ("value", "\0"), ("name", "agenteam_X"), ("name", "1KEY"), ("name", "KEY\n")]:
        check("SecretVariableCreate", variant(request, key, value), False)
    for request in [{"description": ""}, {"value": " \n\t "}, {"name": "NEW"}]:
        check("SecretVariableUpdateBody", {"expected_version": "1", "request": request}, True)
    for request in [{}, {"value": None}, {"description": None}, {"value": "x", "expected_version": "1"}]:
        check("SecretVariableUpdateBody", {"expected_version": "1", "request": request}, False)
    for command in ["create", "update", "delete"]:
        lookup = {"command": "project.secret_variable." + command, "target_id": identity}
        if command != "create":
            lookup["expected_version"] = "1"
        check("SecretVariableLookupBody", lookup, True)
        for key in ["value", "semantic_digest", "credential_ref", "idempotency_key", "project_id"]:
            check("SecretVariableLookupBody", variant(lookup, key, "synthetic-fixture"), False)
        changed = copy.deepcopy(lookup)
        if command == "create":
            changed["expected_version"] = "1"
        else:
            del changed["expected_version"]
        check("SecretVariableLookupBody", changed, False)
    for command, changed in [("create", True), ("update", True), ("update", False), ("delete", True)]:
        receipt = dict(command="project.secret_variable." + command, changed=changed,
                       event_id=identity if changed else None, audit_id=identity if changed else None)
        if command == "delete":
            receipt["deleted"] = dict(id=identity, project_id=identity, type="secret", version="2", deleted_at=at)
        else:
            receipt["variable"] = variant(metadata, "version", "2" if command == "update" and changed else "1")
        check("SecretVariableMutation", receipt, True)
        check("SecretVariableCommandLookup", {"status": "committed", "receipt": receipt}, True)
        check("SecretVariableCommandLookup", {"status": "not_observed", "receipt": receipt}, False)
        check("SecretVariableMutation", variant(receipt, "changed", not changed), False)
        check("SecretVariableMutation", variant(receipt, "value", "synthetic-fixture"), False)
        bad = variant(receipt, "event_id", None if changed else identity)
        check("SecretVariableMutation", bad, False)
        if command == "update" and changed:
            bad = copy.deepcopy(receipt)
            bad["variable"]["version"] = "1"
            check("SecretVariableMutation", bad, False)
    check("SecretVariableCommandLookup", {"status": "not_observed", "receipt": None}, True)
    for status in ["committed", "in_progress", "unknown"]:
        check("SecretVariableCommandLookup", {"status": status, "receipt": None}, False)
    check("SecretVariableSummaryPage", {"items": [metadata] * 100}, True)
    check("SecretVariableSummaryPage", {"items": [metadata] * 101}, False)
    # Actual schema's intended limit: UTF-8 byte length, duplicate members and
    # cross-field times are Go gates, explicitly not claimed by JSON Schema.
    assert not failures, failures
    print(f"independent schema checks: {count} PASS", flush=True)


def go_checks(target, pattern):
    own = Path(__file__).resolve().parents[2]
    build = own / "output/ai/project-variables-ui/implementation"
    output = own / "output/ai/secret-variables-contract-review"
    output.mkdir(parents=True, exist_ok=True)
    run = Path(tempfile.mkdtemp(prefix="pure-", dir=output))
    overlay = run / "overlay.json"
    source = Path(__file__).with_name("independent_test.go").resolve()
    virtual = target / "internal/central/projectvariable/contract/independent_secret_review_test.go"
    assert not virtual.exists(), "overlay must not hide source"
    overlay.write_text(json.dumps({"Replace": {str(virtual): str(source)}}))
    go = Path("/workspace/toolchains/go1.27.1/bin/go")
    env = dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off", GOTELEMETRY="off", GOMAXPROCS="2",
               GOMODCACHE="/workspace/agenteam/output/ai/model-ui-recovery/go-mod",
               GOCACHE=str(build / "gocache"), GOTMPDIR=str(build / "tmp"))
    env["PATH"] = str(go.parent) + os.pathsep + env.get("PATH", "")
    cmd = [str(go), "test", "-race", "-p=1", "-vet=off", "-count=1", "-timeout=90s",
           "-overlay=" + str(overlay), "-run", pattern, "-v", "./internal/central/projectvariable/contract"]
    result = subprocess.run(cmd, cwd=target, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
    (run / "go.log").write_bytes(result.stdout)
    print(result.stdout.decode("utf-8", "replace"), end="", flush=True)
    print("actual go returncode", result.returncode, "output", run, flush=True)
    return result.returncode


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", type=Path, default=Path("/workspace/agenteam-secret-variables"))
    parser.add_argument("--go", action="store_true")
    parser.add_argument("--run", default="^TestIndependentSecret")
    args = parser.parse_args()
    if args.go:
        raise SystemExit(go_checks(args.target.resolve(), args.run))
    schema_checks(args.target.resolve())
