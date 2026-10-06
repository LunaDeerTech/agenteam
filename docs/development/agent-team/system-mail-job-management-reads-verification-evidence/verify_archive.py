#!/usr/bin/env python3
"""Read saved bytes and fixed Git only; never execute product or archived code."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
INDEX = json.loads((HERE / "index.json").read_text())
BASE = "3c79fd4069837c4cee0b1ed37e00480ea8f1909f"
PRODUCT = "819aba1b8f764328f1e2e67b53c274fad0db877d"
TECH = "137e6a302a1f1a2b49076d6406ded7ff61a3c3ffccc939d4fb909640d8f176bc"


def digest(data):
    return hashlib.sha256(data).hexdigest()


assert INDEX["accepted_product_commit"] == PRODUCT
assert INDEX["product_base"] == BASE and INDEX["technical_sha256"] == TECH
OBJECTS = {}
for value, meta in INDEX["objects"].items():
    assert re.fullmatch(r"[0-9a-f]{64}", value)
    assert meta["path"] == "objects/" + value
    path = HERE / meta["path"]
    assert not path.is_symlink()
    data = path.read_bytes()
    assert len(data) == meta["bytes"] and digest(data) == value, path
    OBJECTS[value] = data
assert {p.name for p in (HERE / "objects").iterdir()} == set(OBJECTS)
for name, meta in INDEX["artifacts"].items():
    assert len(OBJECTS[meta["sha256"]]) == meta["bytes"], name
assert {x["sha256"] for x in INDEX["artifacts"].values()} == set(OBJECTS)


def blob(name):
    return OBJECTS[INDEX["artifacts"][name]["sha256"]]


def item(name):
    return json.loads(blob(name))


def private_sources(mapping):
    for source, value in mapping.items():
        assert value in OBJECTS, (source, value)


DELIVERY = item("author/author-delivery8.json")["files"]
assert DELIVERY == INDEX["delivery"] and len(DELIVERY) == 8
assert DELIVERY == item("independent/verification-report.json")["submission_files"]
submitted = {}
for line in blob("independent/submission-8.sha256").decode().splitlines():
    value, name = line.split(maxsplit=1)
    submitted[name.lstrip(" *")] = value
assert submitted == DELIVERY

original_base = item("author/baseline.json")["files"]
final = item("author/input04/manifest.json")
dependencies = final["dependencies"]
assert len(original_base) == 937 and len(dependencies) == 935
assert final["base"] == BASE and final["files"] == DELIVERY
assert set(original_base) - set(dependencies) == {
    "api/openapi/account.json", "internal/central/account/http.go"
}
assert all(original_base[k] == v for k, v in dependencies.items())

requests = {(BASE, k): v for k, v in original_base.items()}
requests.update({(PRODUCT, k): v for k, v in DELIVERY.items()})
card = INDEX["card"]
for commit in [PRODUCT, INDEX["card_accepted_spec_commit"]]:
    requests[(commit, card)] = None
request_order = list(requests)
environment = dict(os.environ, GIT_NO_LAZY_FETCH="1", GIT_OPTIONAL_LOCKS="0")
changed = subprocess.run(
    ["git", "diff-tree", "--no-commit-id", "--name-only", "-r", PRODUCT],
    cwd=ROOT, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    check=True,
).stdout.decode().splitlines()
assert set(changed) == set(DELIVERY)
response = subprocess.run(
    ["git", "cat-file", "--batch"],
    input="".join(f"{commit}:{name}\n" for commit, name in request_order).encode(),
    cwd=ROOT, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    check=True,
).stdout
position = 0
GIT = {}
for key in request_order:
    end = response.index(b"\n", position)
    header = response[position:end].split()
    assert len(header) == 3 and header[1] == b"blob", (key, header)
    size = int(header[2])
    data = response[end + 1:end + 1 + size]
    assert len(data) == size and response[end + 1 + size:end + 2 + size] == b"\n"
    position = end + 2 + size
    if requests[key]:
        assert digest(data) == requests[key], key
    GIT[key] = data
assert position == len(response)
for key in [(PRODUCT, card), (INDEX["card_accepted_spec_commit"], card)]:
    data = GIT[key]
    assert digest(data[data.index(b"## 1."):]) == TECH
current_card = (ROOT / card).read_bytes()
assert digest(current_card[current_card.index(b"## 1."):]) == TECH
for path, value in DELIVERY.items():
    assert OBJECTS[value] == GIT[(PRODUCT, path)]

old_api = json.loads(GIT[(BASE, "api/openapi/account.json")])
new_api = json.loads(GIT[(PRODUCT, "api/openapi/account.json")])
for key, value in old_api["paths"].items():
    assert new_api["paths"][key] == value, key
for key, value in old_api["components"]["schemas"].items():
    assert new_api["components"]["schemas"][key] == value, key
assert set(new_api["paths"]) - set(old_api["paths"]) == {
    "/api/v1/system/mail-jobs/management", "/api/v1/system/mail-jobs/{id}/management"
}
assert set(new_api["components"]["schemas"]) - set(old_api["components"]["schemas"]) == {
    "MailJobManagement", "MailJobManagementList"
}

manifests = []
for name in INDEX["manifests"]:
    manifest = item(name)
    assert manifest["base"] == BASE
    private_sources(manifest["files"])
    prefix = name.rsplit("/", 1)[0] + "/source/"
    for path, value in manifest["files"].items():
        assert INDEX["artifacts"][prefix + path]["sha256"] == value
    if "dependencies" in manifest:
        assert manifest["dependencies"] == dependencies
    manifests.append(manifest)
assert len(manifests[0]["files"]) == 5
assert all(len(m["files"]) == 8 for m in manifests[1:])
for path, value in manifests[0]["files"].items():
    assert all(m["files"][path] == value for m in manifests[1:])
for old, new, expected in zip(manifests[1:], manifests[2:], [
    "tests/accountmail/mail_job_management_test.go",
    "tests/account/http_mail_job_management_test.go",
    "tests/accountmail/mail_job_management_test.go",
]):
    assert {k for k in old["files"] if old["files"][k] != new["files"][k]} == {expected}

for name, meta in INDEX["artifacts"].items():
    if name.startswith("independent/") and name.endswith(".json") and (
        "input" in name or "execution" in name
    ):
        value = item(name)
        if isinstance(value, dict):
            private_sources(value.get("private", {}))

final_runtime = item("author/driver-stage03/input.json")["runtime"]
assert len(final_runtime["files"]) == 3501
assert len(final_runtime["modules"]) == 31
assert len(final_runtime["packages"]) == 452
assert len(final_runtime["executables"]) == 9
assert final_runtime["go_environment"]["GOVERSION"] == "go1.27.1"

real_count = 0
baseline = None
for name, expected in INDEX["runs"].items():
    result = item(name + "/result.json")
    command = item(name + "/command.json")
    raw = blob(name + "/raw.log")
    actual_exit = result.get("driver_exit", result.get("exit_code", result.get("exit")))
    assert actual_exit == expected["exit"], name
    assert command.get("seconds", result.get("seconds")) == expected["seconds"]
    assert command.get("argv", command.get("command")) and command["cwd"]
    if "raw_sha256" in result:
        assert digest(raw) == result["raw_sha256"], name
    if expected["kind"] == "check":
        assert result["actual_wait"] is True, name
        if name.startswith("author/"):
            assert result["input_unchanged"] is True
            before = item(name + "/input-before.json")
            assert before == item(name + "/input-after.json")
            private_sources({k: v for k, v in before.items() if v is not None})
            assert command["env"]["GOTOOLCHAIN"] == "local"
        elif "before" in result:
            assert result["inputs_unchanged"] and result["before"] == result["after"]
            assert command["before"] == result["before"]
            assert result["before"]["actual_fixed_files"] == {**dependencies, **DELIVERY}
            private_sources(result["before"]["private"])
            assert result["before"]["execution_sha256"] in OBJECTS
        else:
            assert result["before_sha256"] == result["after_sha256"]
            assert result["before_sha256"] in OBJECTS
        continue

    real_count += 1
    assert command["actual_wait_completed"] is True and command["exit"] == actual_exit
    for key in ["accepted_baseline_before", "accepted_baseline_after", "double_cleanup",
                "resource_topology_matches", "source_files_unchanged", "verification_inputs_unchanged"]:
        assert result[key] is True, (name, key)
    assert result["browser_processes"] == result["node_processes"] == result["monitor_errors"] == 0
    assert result["top_levels"] == expected["top_levels"]
    tops = re.findall(rb"^--- (PASS|FAIL): ([^\s(]+)", raw, re.M)
    assert [[a.decode(), b.decode()] for a, b in tops] == expected["top_levels"], name
    assert result["selected_tests_passed"] == all(x[0] == "PASS" for x in expected["top_levels"])
    before = item(name + "/input-before.json")
    assert before == item(name + "/input-after.json")
    frozen = item(name + "/frozen-input.json")
    assert before["product_base"] == frozen["product_base"] == BASE
    assert before["files"] == frozen["files"]
    private_sources(before["files"])
    assert before["runtime"] == frozen["runtime"]
    runtime = frozen["runtime"]
    assert set(runtime) == set(final_runtime)
    assert all(runtime[k] == final_runtime[k] for k in runtime if k != "discovery_env")
    # Independent discovery uses its private overlay and TMPDIR. All 3501
    # byte fingerprints, modules, tools and other discovery inputs stay fixed.
    env = runtime["discovery_env"]
    original_env = final_runtime["discovery_env"]
    assert set(env) == set(original_env)
    assert all(env[k] == original_env[k] for k in env if k not in {"GOFLAGS", "TMPDIR"})
    if name.startswith("author/"):
        assert env == original_env
    else:
        overlay_name = {
            "independent/runs/independent01": "independent/real-overlay02.json",
            "independent/runs/independent02": "independent/real-overlay03.json",
        }[name]
        assert env["GOFLAGS"] == original_env["GOFLAGS"] + " -overlay=" + INDEX["artifacts"][overlay_name]["origin"]
        assert env["TMPDIR"] == str(Path(INDEX["artifacts"]["independent/verification-report.json"]["origin"]).parent / "tmp")
    assert before["accepted_baseline"]["accepted"] is True
    assert before["accepted_baseline"]["errors"] == []
    assert before["accepted_baseline"]["files"] == dependencies
    assert {k: v["sha256"] for k, v in frozen["accepted_baseline"]["files"].items()} == dependencies
    binding = item(name + "/verification-input.json")
    assert binding["driver_sha256"] == digest(blob(name + "/driver.py.txt")) == frozen["driver_sha256"]
    assert binding["frozen_input_sha256"] == digest(blob(name + "/frozen-input.json"))
    assert frozen["product_manifest"]["sha256"] in OBJECTS
    private_sources(frozen.get("private", {}))
    resources = item(name + "/observed-resources.json")
    processes = item(name + "/observed-processes.json")
    assert len(resources) == result["observed_resources"] == expected["observed_resources"]
    assert len(processes) == result["observed_processes"] == expected["observed_processes"]
    for key, process in processes.items():
        assert key == f'{process["pid"]}:{process["starttime"]}'
    original_baseline = item(name + "/baseline.json")
    assert len(original_baseline) == 6
    assert sorted(v["kind"] for v in original_baseline.values()) == ["container"] * 2 + ["network"] * 4
    if baseline is None:
        baseline = original_baseline
    assert baseline == original_baseline
    assert set(resources).isdisjoint(baseline)
    cleanups = item(name + "/cleanup.json")
    assert len(cleanups) == 2
    for clean in cleanups:
        assert clean["baseline_unchanged"] is True
        assert clean["owned_processes"] == clean["remaining_new"] == clean["runtime_entries"] == []
        assert set(clean["exact_absent"]) == set(resources)
        assert all(v == {"absent": True} for v in clean["exact_absent"].values())
    assert cleanups[0]["time"] < cleanups[1]["time"]
    assert item(name + "/monitor-errors.json") == []
    assert item(name + "/adopted-waits.json") == []
    assert result["adopted_waits"] == expected["adopted_waits"] == 0
    assert item(name + "/cancellation.json") == []

counts = INDEX["counts"]
assert counts == {
    "logical_artifacts": len(INDEX["artifacts"]), "objects": len(OBJECTS),
    "object_bytes": sum(len(v) for v in OBJECTS.values()), "delivery_paths": 8,
    "accepted_dependencies": 935, "runtime_files": 3501,
    "original_runs": len(INDEX["runs"]), "real_rounds": real_count,
}
assert real_count == 7
print(
    f'PASS: {counts["logical_artifacts"]} originals / {counts["objects"]} SHA objects / '
    f'8 Git delivery paths / 935 fixed dependencies / 3501 runtime fingerprints / '
    f'{counts["original_runs"]} original checks including 7 real exits and double cleanups; '
    'old OpenAPI schemas and card technical bytes unchanged; no product execution'
)
