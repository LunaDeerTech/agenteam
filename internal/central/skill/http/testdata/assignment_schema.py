#!/usr/bin/env python3
"""Validate actual assignment projections with local schemas, without retrieval."""
import json
import sys
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry
from referencing.jsonschema import DRAFT202012

root = Path(__file__).resolve().parents[5]
base = "https://schemas.invalid/api/"
registry = Registry()
documents = {}
for name in ("skill-assignment.json", "common.json"):
    document = json.loads((root / "api/openapi" / name).read_text())
    documents[name] = document
    registry = registry.with_resource(base + name, DRAFT202012.create_resource(document))
spec = documents["skill-assignment.json"]
path = "/api/v1/projects/{project_id}/agents/{agent_id}/skills"
assert set(spec["paths"]) == {path, path + "/assign", path + "/remove", path + "/commands/lookup"}
assert set(spec["paths"][path]) == {"parameters", "get", "head"}
for endpoint in (path + "/assign", path + "/remove", path + "/commands/lookup"):
    assert set(spec["paths"][endpoint]) == {"parameters", "post"}
for response in spec["paths"][path]["head"]["responses"].values():
    assert "content" not in response
    assert response["headers"]["Cache-Control"]["schema"] == {"const": "no-store"}
schemas = spec["components"]["schemas"]
for schema in schemas.values():
    Draft202012Validator.check_schema(schema)
cases = json.load(sys.stdin)["cases"]
assert cases
for case in cases:
    assert case["schema"] in schemas
    validator = Draft202012Validator(
        {"$ref": base + "skill-assignment.json#/components/schemas/" + case["schema"]},
        registry=registry,
        format_checker=FormatChecker(),
    )
    assert (not list(validator.iter_errors(case["value"]))) == case["valid"], case["label"]
print(f"assignment actual Schema controls={len(cases)}; bodyless HEAD; no network")
