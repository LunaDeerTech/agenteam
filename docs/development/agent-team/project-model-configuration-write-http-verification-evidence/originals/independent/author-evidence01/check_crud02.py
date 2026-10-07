import hashlib
import json
from pathlib import Path
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

BASE = Path(__file__).parent
source = json.loads((BASE / "crud02-input.json").read_text())
sha = lambda p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
for path, expected in source["files"].items():
    assert sha(path) == expected, "frozen evidence changed"
run = Path(source["run"])
result = json.loads((run / "result.json").read_text())
assert result["driver_exit"] == 0 and result["selected_tests_passed"]
assert result["actual_wait_completed"] and result["double_cleanup"]
assert result["source_files_unchanged"] and result["verification_inputs_unchanged"]
assert result["top_levels"] == [["PASS", "TestModelProjectConfigurationWriteHTTPCRUDAndHistory"]]
command = json.loads((run / "command.json").read_text())
assert command["exit"] == 0 and command["actual_wait_completed"] and command["watchdog_thread_joined"]
tail = json.loads((run / "owned-tail-retirement.json").read_text())
assert tail["actual_owned_completion"] and not tail["actions"] and not tail["remaining"]
cleanup = json.loads((run / "cleanup.json").read_text())
assert len(cleanup) == 2
for check in cleanup:
    assert check["baseline_unchanged"] and not check["owned_processes"]
    assert len(check["exact_absent"]) == 7 and all(v["absent"] for v in check["exact_absent"].values())
watchdog = json.loads((run / "watchdog.json").read_text())
assert watchdog["complete"] and not watchdog["active"] and not watchdog["failures"]
input_sha = sha(run / "frozen-input.json")
assert json.loads((run / "frozen-input.json").read_text())["candidate_sha256"] == source["candidate_sha256"]
verification = json.loads((run / "verification-input.json").read_text())
assert verification["frozen_input_sha256"] == input_sha
assert verification["driver_sha256"] == sha(run / "driver.py.txt")
schema_dir = Path("/workspace/agenteam/api/openapi")
registry = Registry()
for name in ["project-models.json", "common.json"]:
    path = schema_dir / name
    registry = registry.with_resource(path.as_uri(), Resource.from_contents(json.loads(path.read_text()), default_specification=DRAFT202012))

def unique_pairs(pairs):
    out = {}
    for key, value in pairs:
        assert key not in out, "duplicate response member"
        out[key] = value
    return out

bodies = {}
checks = []
for name in source["body_names"]:
    path = run / "safe-http-evidence" / (name + ".json")
    raw = path.read_bytes()
    meta = json.loads(path.with_name(name + "-source.json").read_text())
    assert meta["body_sha256"] == sha(path) and meta["schema_sha256"] == sha(schema_dir / "project-models.json")
    assert meta["run"] == "new-crud02" and meta["input_sha256"] == input_sha
    assert meta["status"] == 200 and meta["content_type"] == "application/json"
    assert int(meta["content_length"]) == len(raw) <= 1024
    assert meta["path"] == meta["target"] and meta["path"].startswith("/api/v1/projects/")
    assert set(meta["response_headers"]) == {"X-Request-ID"} and meta["response_headers"]["X-Request-ID"]
    parsed = json.loads(raw, object_pairs_hook=unique_pairs)
    schema = "ConfigurationLookup" if name.startswith("lookup-") else "ConfigurationReceipt"
    validator = Draft202012Validator({"$ref": (schema_dir / "project-models.json").as_uri() + "#/components/schemas/" + schema}, registry=registry, format_checker=FormatChecker())
    assert not list(validator.iter_errors(parsed)), "real raw response violates schema"
    if schema == "ConfigurationReceipt":
        assert parsed["kind"] == name and parsed["affected_references"] == "0"
        assert meta["method"] == {"create": "POST", "update": "PUT", "delete": "DELETE"}[name.split(".")[1]]
    else:
        assert meta["method"] == "POST" and meta["path"].endswith("/model-commands/lookup")
    bodies[name] = parsed
    checks.append({"name": name, "schema": schema, "body_sha256": sha(path), "bytes": len(raw), "source_sha256": sha(path.with_name(name + "-source.json"))})
for resource in ["provider", "model"]:
    receipts = [bodies[resource + "." + kind] for kind in ["create", "update", "delete"]]
    assert len({r["resource_id"] for r in receipts}) == 1
    assert [r["version"] for r in receipts] == ["1", "2", "3"]
assert bodies["lookup-absent"] == {"found": False, "receipt": None}
assert bodies["lookup-found"]["found"] is True
assert bodies["lookup-found"]["receipt"] == bodies["provider.create"]
print(json.dumps({"status": "PASS", "body_count": len(checks), "checks": checks, "actual_author_run": "new-crud02", "independent_runtime": False, "author_seconds": command["seconds"], "owned_actual_wait_doublecleanup": True, "run_input_sha256": input_sha}, indent=2))
