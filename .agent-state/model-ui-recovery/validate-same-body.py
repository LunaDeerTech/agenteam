"""Validate retained original safe bytes. No HTTP resolver or request material."""
import calendar
import hashlib
import json
import pathlib
import re
import sys

from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012


def main():
    root, evidence = map(pathlib.Path, sys.argv[1:])
    base = root / "api/openapi"
    names = ("common.json", "project-models.json", "project-model-credentials.json")
    docs = {name: json.loads((base / name).read_bytes()) for name in names}
    registry = Registry().with_resources(
        ((base / name).as_uri(), Resource.from_contents(doc, default_specification=DRAFT202012))
        for name, doc in docs.items()
    )
    checker = FormatChecker()

    @checker.checks("date-time")
    def instant(value):
        if not isinstance(value, str):
            return True
        match = re.fullmatch(r"(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?Z", value)
        if not match:
            return False
        year, month, day, hour, minute, second = map(int, match.groups())
        return 1 <= month <= 12 and 1 <= day <= calendar.monthrange(year, month)[1] and hour < 24 and minute < 60 and second < 60

    selected = json.loads((evidence / "same-body-input.json").read_bytes())
    assert isinstance(selected, list) and 0 < len(selected) <= 512
    attachment = json.loads((root / "docs/development/work-items/d27-project-owner-model-settings-ui-endpoints.json").read_bytes())
    operations = {op["operation"]: op for op in attachment["operations"]}
    tokens = set()
    for row in selected:
        assert set(row) == {"sidecar", "request_token", "browser_eof", "bytes", "sha256"}
        assert re.fullmatch(r"response-\d+\.json", row["sidecar"])
        meta = json.loads((evidence / row["sidecar"]).read_bytes())
        assert meta["protocol"] == "project-owner-models.v1" and meta["source"] == "browser"
        assert meta["body_stage"] == "complete_formal_upstream" and meta["transfer_kind"] in ("forwarded", "hold")
        assert row["browser_eof"] is True and row["request_token"] == meta["request_token"]
        assert meta["request_token"] not in tokens
        tokens.add(meta["request_token"])
        assert re.fullmatch(r"[0-9a-f]{64}", meta["body_sha256"])
        assert meta["body_file"] == "body-" + meta["body_sha256"] + ".json"
        raw = (evidence / meta["body_file"]).read_bytes()
        assert len(raw) == row["bytes"] == meta["body_bytes"]
        assert hashlib.sha256(raw).hexdigest() == row["sha256"] == meta["body_sha256"]
        op = operations[meta["operation"]]
        assert meta["method"] == op["method"]
        route = attachment["base"] + op["path"]
        expected_path = route.replace("{project_id}", meta["project_id"])
        for parameter in ("provider_id", "model_id", "credential_id"):
            expected_path = expected_path.replace("{" + parameter + "}", meta["resource_id"] or "")
        assert expected_path == meta["endpoint"]
        name = "project-model-credentials.json" if "Credential" in meta["operation"] else "project-models.json"
        method = docs[name]["paths"][route][meta["method"].lower()]
        response = method["responses"].get(str(meta["status"]), method["responses"].get("default"))
        assert response is not None
        media = meta["content_type"].split(";")[0].strip().lower()
        schema = dict(docs[name])
        schema["$id"] = (base / name).as_uri()
        schema.update(response["content"][media]["schema"])
        Draft202012Validator(schema, registry=registry, format_checker=checker).validate(json.loads(raw))
    print(len(selected))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print("PROJECT_MODELS_SAME_BODY_SCHEMA_FAILED", file=sys.stderr)
        sys.exit(1)
