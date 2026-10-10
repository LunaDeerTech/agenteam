#!/usr/bin/env python3
"""Check actual HTTP vectors with only local Schema resources; no network."""
import json
from pathlib import Path
import sys

from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry
from referencing.jsonschema import DRAFT202012

root = Path(__file__).resolve().parents[2]
base = "https://schemas.invalid/api/"
registry = Registry()
documents = {}
for name in ("knowledge-content.json", "common.json"):
    document = json.loads((root / "api/openapi" / name).read_text())
    documents[name] = document
    registry = registry.with_resource(base + name, DRAFT202012.create_resource(document))
spec = documents["knowledge-content.json"]
assert len(spec["paths"]) == 1
endpoint = next(iter(spec["paths"].values()))
assert set(endpoint) == {"parameters", "get", "head"}
head_count = 0
for response in endpoint["head"]["responses"].values():
    if "$ref" in response:
        response = spec["components"]["responses"][response["$ref"].rsplit("/", 1)[1]]
    assert "content" not in response, "HEAD response must not declare a body"
    assert response["headers"]["Cache-Control"]["schema"] == {"const": "no-store"}
    assert "Content-Length" in response["headers"]
    head_count += 1
assert head_count == 10
for schema in spec["components"]["schemas"].values():
    Draft202012Validator.check_schema(schema)
cases = json.load(sys.stdin)["cases"]
assert cases
for case in cases:
    document = case.get("document", "knowledge-content.json")
    assert document in documents
    validator = Draft202012Validator(
        {"$ref": base + document + "#/components/schemas/" + case["schema"]},
        registry=registry, format_checker=FormatChecker(),
    )
    accepted = not list(validator.iter_errors(case["value"]))
    assert accepted == case["valid"], case["label"]
print(f"PASS {len(cases)} actual-handler Schema controls; ten bodyless HEAD statuses; no network")
