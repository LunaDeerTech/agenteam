#!/usr/bin/env python3
"""Read original bytes and fixed Git blobs only; never execute archived code."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
INDEX = json.loads((HERE / "index.json").read_text())
BASE = "819aba1b8f764328f1e2e67b53c274fad0db877d"
PRODUCT = "f670cb1fe1f07ebd21bdb90a2b96cd565c33f05a"
TECH = "f62bf7a195d9bf501d33cebf48e43680e5f58db112d00b7dc417583f757aaf15"


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
assert {v["sha256"] for v in INDEX["artifacts"].values() if "object" in v} == set(OBJECTS)
GIT = {}


def blob(name):
    meta = INDEX["artifacts"][name]
    if "git" in meta:
        return GIT[(meta["git"]["git_commit"], meta["git"]["path"])]
    return OBJECTS[meta["sha256"]]


def item(name):
    return json.loads(blob(name))


DELIVERY = item("author/author-delivery10.json")["files"]
NINE = item("author/input01/manifest.json")["files"]
assert len(DELIVERY) == 10 and DELIVERY == INDEX["delivery"]
assert len(NINE) == 9
assert {k: v for k, v in DELIVERY.items() if k != "docs/development/backend/account.md"} == NINE
assert NINE == item("author/author-delivery9.json")["files"]
assert NINE == item("independent/verification-report.json")["delivery9"]
closure02 = item("author/closure02.json")["files"]
closure03 = item("author/closure03.json")["files"]
assert len(closure02) == 621 and len(closure03) == 886
assert all(closure03[k]["sha256"] == v["sha256"] for k, v in closure02.items())
final_input = item("author/driver-stage01/input.json")
dependencies = {k: v["sha256"] for k, v in final_input["accepted_baseline"]["files"].items()}
assert len(dependencies) == 881
assert dependencies == {k: v["sha256"] for k, v in closure03.items() if k not in NINE}

requests = {(BASE, k): v["sha256"] for k, v in closure03.items()}
requests.update({(PRODUCT, k): v for k, v in DELIVERY.items()})
card = INDEX["card"]
requests[(PRODUCT, card)] = None
for meta in INDEX["artifacts"].values():
    if "git" in meta:
        ref = meta["git"]
        requests[(ref["git_commit"], ref["path"])] = meta["sha256"]
order = list(requests)
env = dict(os.environ, GIT_NO_LAZY_FETCH="1", GIT_OPTIONAL_LOCKS="0")
changed = subprocess.run(
    ["git", "diff-tree", "--no-commit-id", "--name-only", "-r", PRODUCT],
    cwd=ROOT, env=env, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
).stdout.decode().splitlines()
assert set(changed) == set(DELIVERY)
response = subprocess.run(
    ["git", "cat-file", "--batch"], cwd=ROOT, env=env, check=True,
    input="".join(f"{commit}:{path}\n" for commit, path in order).encode(),
    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
).stdout
position = 0
for key in order:
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
for name, meta in INDEX["artifacts"].items():
    data = blob(name)
    assert digest(data) == meta["sha256"] and len(data) == meta["bytes"], name
for path, value in DELIVERY.items():
    assert OBJECTS[value] == GIT[(PRODUCT, path)], path
for data in [blob("spec/card-accepted-header.md"), GIT[(PRODUCT, card)], (ROOT / card).read_bytes()]:
    assert digest(data[data.index(b"## 1."):]) == TECH
assert item("author/author-delivery10.json")["nine_product_files_unchanged"] is True

for name, prefix in [
    ("author/input01/manifest.json", "author/input01/source/"),
    ("author/core-stage01/manifest.json", "author/core-stage01/source/"),
]:
    manifest = item(name)
    for path, value in manifest["files"].items():
        assert INDEX["artifacts"][prefix + path]["sha256"] == value
        assert NINE[path] == value

# Preserve the original red input and the exact minimal probe functions.
functions = item("core/original-functions.json")["functions"]
for path, function in [
    ("internal/central/account/validation_test.go", "TestNormalizeEmailCanonicalReentry"),
    ("internal/central/accountmail/smtp_address_test.go", "TestSMTPMessageCanonicalReentry"),
]:
    pattern = rb"(?m)^func " + function.encode() + rb"\([^\n]*\n.*?^}"
    originals = []
    for run in ["old01", "new-minimal01"]:
        data = blob(f"author/runs/{run}/source/{path}")
        matched = re.search(pattern, data, re.S)
        assert matched is not None
        originals.append(matched.group())
    assert originals[0] == originals[1]
    assert digest(originals[0]) == functions[function]["sha256"]

runtime_reference = final_input["runtime"]
assert len(runtime_reference["files"]) == 3501
assert runtime_reference["go_environment"]["GOVERSION"] == "go1.27.1"
runtime_hashes = set(runtime_reference["files"].values())
for name in ["independent/execution01.json", "independent/execution02.json"]:
    runtime = item(name)["runtime"]
    assert len(runtime["files"]) == 3512
    runtime_hashes.update(runtime["files"].values())
known_hashes = set(OBJECTS) | {digest(v) for v in GIT.values()} | runtime_hashes


def bound_hashes(mapping):
    for name, value in mapping.items():
        if value is not None:
            assert value in known_hashes, (name, value)


baseline = None
real_count = 0
for name, expected in INDEX["runs"].items():
    result = item(name + "/result.json")
    command = item(name + "/command.json")
    raw = blob(name + "/raw.log")
    actual_exit = result.get("driver_exit", result.get("exit_code", result.get("exit")))
    assert actual_exit == expected["exit"], name
    assert command.get("seconds", result.get("seconds")) == expected["seconds"]
    assert command.get("argv", command.get("command")) and command["cwd"] and command["env"]
    if "raw_sha256" in result:
        assert digest(raw) == result["raw_sha256"], name
    before = item(name + "/input-before.json")
    assert before == item(name + "/input-after.json"), name
    if expected["kind"] == "check":
        assert result["actual_wait"] is True and result["input_unchanged"] is True
        assert command["env"]["GOTOOLCHAIN"] == "local"
        bound_hashes(before)
        if name.startswith("author/"):
            for path, value in before.items():
                if value is not None:
                    assert INDEX["artifacts"][name + "/source/" + path]["sha256"] == value
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
    frozen = item(name + "/frozen-input.json")
    assert before["product_base"] == frozen["product_base"] == BASE
    assert before["files"] == frozen["files"] == NINE
    assert before["runtime"] == frozen["runtime"]
    assert before["accepted_baseline"]["accepted"] is True
    assert before["accepted_baseline"]["errors"] == []
    assert before["accepted_baseline"]["files"] == dependencies
    assert {k: v["sha256"] for k, v in frozen["accepted_baseline"]["files"].items()} == dependencies
    binding = item(name + "/verification-input.json")
    assert binding["driver_sha256"] == digest(blob(name + "/driver.py.txt")) == frozen["driver_sha256"]
    assert binding["frozen_input_sha256"] == digest(blob(name + "/frozen-input.json"))
    assert frozen["product_manifest"]["sha256"] in OBJECTS
    bound_hashes(frozen.get("private_files", {}))
    if name.startswith("author/"):
        assert frozen["runtime"] == runtime_reference
    else:
        assert len(frozen["private_files"]) == 14
        version = "01" if name.endswith("01") else "02"
        assert frozen == item("independent/execution" + version + ".json")
        assert before["private_files"] == frozen["private_files"]
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
    assert baseline == original_baseline and set(resources).isdisjoint(baseline)
    cleanups = item(name + "/cleanup.json")
    assert len(cleanups) == 2 and cleanups[0]["time"] < cleanups[1]["time"]
    for clean in cleanups:
        assert clean["baseline_unchanged"] is True
        assert clean["owned_processes"] == clean["remaining_new"] == clean["runtime_entries"] == []
        assert set(clean["exact_absent"]) == set(resources)
        assert all(v == {"absent": True} for v in clean["exact_absent"].values())
    for leaf in ["monitor-errors", "adopted-waits", "cancellation"]:
        assert item(name + "/" + leaf + ".json") == []
    assert result["adopted_waits"] == expected["adopted_waits"] == 0

for case in item("independent/verification-report.json")["combination"]:
    raw = blob("independent/runs/" + case["run"] + "/raw.log")
    pattern = rb"--- PASS: [^\s]+/" + case["case"].encode() + rb" \(" + f'{case["seconds"]:.2f}'.encode() + rb"s\)"
    assert re.search(pattern, raw), case
assert INDEX["runs"]["independent/runs/independent01"]["exit"] == 1
assert INDEX["runs"]["independent/runs/independent02"]["exit"] == 0
assert item("independent/execution02.json")["selected_subtests"] == [
    "TestIndependentEmailHistoryAndTestIntent/http-original-history",
    "TestIndependentEmailCanonicalWire/expanded-ipv6",
]
assert (
    "^(TestIndependentEmailHistoryAndTestIntent|TestIndependentEmailCanonicalWire)$/"
    "^(http-original-history|expanded-ipv6)$"
) in item("independent/runs/independent02/command.json")["argv"]

counts = INDEX["counts"]
assert counts == {
    "logical_artifacts": len(INDEX["artifacts"]), "objects": len(OBJECTS),
    "git_reused_artifacts": sum("git" in v for v in INDEX["artifacts"].values()),
    "object_bytes": sum(len(v) for v in OBJECTS.values()), "delivery_paths": 10,
    "stage_closure_files": [621, 886], "accepted_dependencies": 881,
    "author_runtime_files": 3501, "independent_runtime_files": 3512,
    "original_runs": len(INDEX["runs"]), "real_rounds": real_count,
}
assert real_count == 5
print(
    f'PASS: {counts["logical_artifacts"]} originals / {counts["objects"]} SHA objects + '
    f'{counts["git_reused_artifacts"]} Git references / 10 Git delivery paths / '
    '621,886 stage closure files / 881 unchanged real dependencies / '
    '3501 author and 3512 independent runtime fingerprints / '
    f'{counts["original_runs"]} original checks including 5 actual exits and double cleanups; '
    'four independent subcases combined, original failures retained; no product execution'
)
