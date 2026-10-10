#!/usr/bin/env python3
"""Validate actual HTTP projections against local frozen schemas; no retrieval."""
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
for name in ("skill-owner.json", "common.json"):
    document = json.loads((root / "api/openapi" / name).read_text())
    documents[name] = document
    registry = registry.with_resource(base + name, DRAFT202012.create_resource(document))
schemas = documents["skill-owner.json"]["components"]["schemas"]
spec = documents["skill-owner.json"]
head_count = 0
collection = "/api/v1/projects/{project_id}/skills"
read_paths = {
    collection: 9,
    collection + "/{skill_id}": 9,
    collection + "/catalog": 12,
}
lookup = collection + "/commands/lookup"
assert set(spec["paths"]) == {*read_paths, lookup}, "unexpected route set"
assert set(spec["paths"][lookup]) == {"parameters", "post"}, "lookup must remain POST-only"
assert set(spec["paths"][collection]) == {"parameters", "get", "head", "post"}
for route, statuses in read_paths.items():
    endpoint = spec["paths"][route]
    assert len(endpoint["head"]["responses"]) == statuses, "HEAD status set changed"
    for status, response in endpoint["head"]["responses"].items():
        if "$ref" in response:
            response = spec["components"]["responses"][response["$ref"].rsplit("/", 1)[1]]
        assert "content" not in response, "HEAD must never declare a response body"
        assert response["headers"]["Cache-Control"]["schema"] == {"const": "no-store"}
        assert "Content-Length" in response["headers"]
        head_count += 1
    assert endpoint["get"]["responses"]["400"] == {"$ref": "#/components/responses/Problem"}
assert head_count == 30
assert "application/problem+json" in spec["components"]["responses"]["Problem"]["content"]
for schema in schemas.values():
    Draft202012Validator.check_schema(schema)
cases = json.load(sys.stdin)["cases"]
assert cases, "no schema controls"
for case in cases:
    name = case["schema"]
    assert name in schemas or name == "Problem", "unknown local schema"
    document = "common.json" if name == "Problem" else "skill-owner.json"
    validator = Draft202012Validator(
        {"$ref": base + document + "#/components/schemas/" + name},
        registry=registry, format_checker=FormatChecker(),
    )
    accepted = not list(validator.iter_errors(case["value"]))
    assert accepted == case["valid"], case["label"]
print("actual local Schema controls=" + str(len(cases)) + "; 30 bodyless HEAD statuses PASS; no network")
