#!/usr/bin/env python3
"""Validate synthetic actual-handler vectors offline; never invoke Go or HTTP."""
import argparse
import json
from pathlib import Path

from jsonschema import Draft202012Validator
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("vectors", type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    path = root / "api/openapi/knowledge-tree-commands.json"
    common = path.with_name("common.json")
    document = json.loads(path.read_text())
    registry = Registry().with_resources(
        (p.as_uri(), Resource.from_contents(json.loads(p.read_text()), default_specification=DRAFT202012))
        for p in (path, common)
    )
    for schema in document["components"]["schemas"].values():
        Draft202012Validator.check_schema(schema)
    vectors = json.loads(args.vectors.read_text())
    failures = []
    for i, vector in enumerate(vectors):
        schema = {"$ref": path.as_uri() + "#/components/schemas/" + vector["schema"]}
        actual = Draft202012Validator(schema, registry=registry).is_valid(vector["value"])
        if actual is not vector["valid"]:
            failures.append({"index": i, "name": vector["name"], "expected": vector["valid"]})
    print(json.dumps({"vectors": len(vectors), "failures": failures}))
    return bool(failures)


if __name__ == "__main__":
    raise SystemExit(main())
