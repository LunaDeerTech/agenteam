#!/usr/bin/env python3
"""Verify original bytes and fixed Git blobs; never execute archived code."""
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
PRODUCT = "a94277982620f01dc15488b09ae6ea9064977b5a"
TECH = "44515e212a996d1a1413022a38ae2b56297ad7e5cd9d195d732225aab818f8dc"


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
    assert digest(data) == value and len(data) == meta["bytes"], path
    OBJECTS[value] = data
assert {p.name for p in (HERE / "objects").iterdir()} == set(OBJECTS)
assert {v["sha256"] for v in INDEX["artifacts"].values() if "object" in v} == set(OBJECTS)
GIT = {}


def blob(name):
    meta = INDEX["artifacts"][name]
    if "git" in meta:
        ref = meta["git"]
        return GIT[(ref["git_commit"], ref["path"])]
    return OBJECTS[meta["sha256"]]


def item(name):
    return json.loads(blob(name))


delivery = item("author/delivery12/manifest.json")["files"]
assert delivery == INDEX["delivery"] and len(delivery) == 12
eleven = item("author/harness-stage02.json")["files"]
assert eleven == item("independent/delivery11.json")["files"] and len(eleven) == 11
assert {k: v for k, v in delivery.items() if k != "docs/development/backend/outbound.md"} == eleven
e1 = item("author/execution-stage01/input.json")
e2 = item("author/execution-stage02/input.json")
ie = item("independent/execution01.json")
assert len(e1["accepted_baseline"]["files"]) == 420
assert len(e2["accepted_baseline"]["files"]) == 421
assert ie["accepted_baseline"] == e2["accepted_baseline"]
assert len(e1["runtime"]["files"]) == len(e2["runtime"]["files"]) == 3503
assert len(ie["runtime"]["files"]) == 3506
assert ie["files"] == e2["files"] == eleven
requests = {(PRODUCT, path): value for path, value in delivery.items()}
requests[(PRODUCT, INDEX["card"])] = None
for path, meta in e2["accepted_baseline"]["files"].items():
    requests[(BASE, path)] = meta["sha256"]
for path, value in item("author/inputs/baseline-closure.json")["files"].items():
    requests[(BASE, path)] = value if isinstance(value, str) else value["sha256"]
for path, meta in item("spec-static/input.json")["fixed_files"].items():
    requests[(BASE, path)] = meta["sha256"]
for meta in INDEX["artifacts"].values():
    if "git" in meta:
        ref = meta["git"]
        requests[(ref["git_commit"], ref["path"])] = meta["sha256"]
env = dict(os.environ, GIT_NO_LAZY_FETCH="1", GIT_OPTIONAL_LOCKS="0")
changed = subprocess.run(
    ["git", "diff-tree", "--no-commit-id", "--name-only", "-r", PRODUCT],
    cwd=ROOT, env=env, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
).stdout.decode().splitlines()
assert set(changed) == set(delivery)
order = list(requests)
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
for path, value in delivery.items():
    assert blob("author/delivery12-source/" + path) == GIT[(PRODUCT, path)]
for data in [blob("spec-static/card-rev1.md"), blob("independent-api/spec.md"),
             GIT[(PRODUCT, INDEX["card"])], (ROOT / INDEX["card"]).read_bytes()]:
    assert digest(data[data.index(b"## 1."):]) == TECH

for stage in ["api-stage01", "harness-stage01", "harness-stage02"]:
    for path, value in item("author/" + stage + ".json")["files"].items():
        assert digest(blob("author/" + stage + "-source/" + path)) == value
old = item("author/harness-stage01.json")["files"]
assert [p for p in old if old[p] != eleven[p]] == ["tests/account/outbound_policy_http_transaction_test.go"]
path = "tests/account/outbound_policy_http_transaction_test.go"
assert blob("author/harness-stage01-source/" + path).replace(
    b"newAccount(t)", b"newB02Account(t).fixture", 1
) == blob("author/harness-stage02-source/" + path)
assert len(item("author/execution-stage02/closure-snapshot.json")["files"]) == 432
for path, value in item("author/execution-stage02/closure-snapshot.json")["files"].items():
    assert value == (eleven[path] if path in eleven else digest(GIT[(BASE, path)]))
assert len(item("independent-api/fixed-input.json")["files"]) == 414
runtime_hashes = set()
for frozen in [e1, e2, ie]:
    assert frozen["runtime"]["go_environment"]["GOVERSION"] == "go1.27.1"
    runtime_hashes.update(frozen["runtime"]["files"].values())
known = set(OBJECTS) | {digest(v) for v in GIT.values()} | runtime_hashes


def known_hashes(mapping):
    for name, value in mapping.items():
        assert value in known, (name, value)


baseline = None
real_count = 0
for name, expected in INDEX["runs"].items():
    result = item(name + "/result.json")
    command = item(name + "/command.json")
    raw = blob(name + "/raw.log")
    code = result.get("driver_exit", result.get("actual_exit", result.get("exit_code", result.get("exit"))))
    assert code == expected["exit"], name
    assert command.get("seconds", result.get("seconds")) == expected["seconds"], name
    assert command.get("argv", command.get("command")) and command["cwd"]
    if "raw_sha256" in result:
        assert digest(raw) == result["raw_sha256"], name
    kind = expected["kind"]
    if kind == "author-offline":
        assert command["env"] and result["cleanup_first"] == result["cleanup_second"] == []
        assert result["adopted_waits"] == []
        for path, value in item(name + "/input.json")["files"].items():
            assert digest(blob(name + "/input/" + path)) == value
        continue
    if kind == "independent-recorded-command":
        assert command["actual_wait"] is True and result["resources_started"] is False
        continue
    before = item(name + "/input-before.json")
    assert before == item(name + "/input-after.json"), name
    if kind in ["api-independent", "independent-offline"]:
        assert command["env"] and result["input_unchanged"] is True
        known_hashes(before)
        if kind == "api-independent":
            assert result["actual_wait_completed"] is True
            assert result["cleanup_first"] == result["cleanup_second"] == []
            assert result["actual_adopted_waits"] == []
        else:
            assert result["actual_wait"] is True and result["resource_execution"] is False
            assert result["first"] == result["second"] == {} and result["adopted_waits"] == []
        continue
    assert kind == "real"
    real_count += 1
    assert command["actual_wait_completed"] is True and command["exit"] == code
    assert command["env"]
    for key in ["accepted_baseline_before", "accepted_baseline_after", "double_cleanup",
                "resource_topology_matches", "source_files_unchanged", "verification_inputs_unchanged"]:
        assert result[key] is True, (name, key)
    assert result["browser_processes"] == result["node_processes"] == result["monitor_errors"] == 0
    tops = [[a.decode(), b.decode()] for a, b in re.findall(rb"^--- (PASS|FAIL): ([^\s(]+)", raw, re.M)]
    assert tops == result["top_levels"] == expected["top_levels"]
    assert result["selected_tests_passed"] == all(x[0] == "PASS" for x in tops)
    frozen = item(name + "/frozen-input.json")
    assert before["product_base"] == frozen["product_base"] == BASE
    assert before["files"] == frozen["files"]
    assert before["runtime"] == frozen["runtime"]
    assert before["accepted_baseline"]["accepted"] is True
    assert before["accepted_baseline"]["errors"] == []
    assert before["accepted_baseline"]["files"] == {
        p: v["sha256"] for p, v in frozen["accepted_baseline"]["files"].items()
    }
    binding = item(name + "/verification-input.json")
    assert binding["driver_sha256"] == digest(blob(name + "/driver.py.txt")) == frozen["driver_sha256"]
    assert binding["frozen_input_sha256"] == digest(blob(name + "/frozen-input.json"))
    assert frozen["product_manifest"]["sha256"] in known
    known_hashes(frozen.get("private_inputs", {}))
    resources = item(name + "/observed-resources.json")
    processes = item(name + "/observed-processes.json")
    assert len(resources) == result["observed_resources"] == expected["observed_resources"] == 7
    assert len(processes) == result["observed_processes"] == expected["observed_processes"]
    for key, process in processes.items():
        assert key == f'{process["pid"]}:{process["starttime"]}'
    original_baseline = item(name + "/baseline.json")
    assert len(original_baseline) == 6
    assert sorted(v["kind"] for v in original_baseline.values()) == ["container"] * 2 + ["network"] * 4
    if baseline is None:
        baseline = original_baseline
    assert baseline == original_baseline and set(resources).isdisjoint(baseline)
    scans = item(name + "/cleanup.json")
    assert len(scans) == 2 and scans[0]["time"] < scans[1]["time"]
    for scan in scans:
        assert scan["baseline_unchanged"] is True
        assert scan["owned_processes"] == scan["remaining_new"] == scan["runtime_entries"] == []
        assert set(scan["exact_absent"]) == set(resources)
        assert all(v == {"absent": True} for v in scan["exact_absent"].values())
    for leaf in ["monitor-errors", "adopted-waits", "cancellation"]:
        assert item(name + "/" + leaf + ".json") == []
    assert result["adopted_waits"] == expected["adopted_waits"] == 0

assert INDEX["runs"]["author/evidence/api03"]["exit"] == 1
assert b"unexpected EOF" in blob("author/evidence/api03/raw.log")
assert INDEX["runs"]["author/runs/outbound-http-real01"]["exit"] == 1
assert INDEX["runs"]["independent/runs/compile-account01"]["exit"] == 1
assert b"IdempotencyKey" in blob("independent/runs/compile-account01/raw.log")
limits = INDEX["historical_limits"]
assert len(limits) == 1 and limits[0]["expected_sha256"] == (
    "73ca61720757dae8393b0170de10446538adeb8e92cd8d22fb65e32e8a995884"
)
assert limits[0]["expected_sha256"] not in known
assert item("author/evidence/harness-compile01/input.json")["harness_closure_sha256"] == limits[0]["expected_sha256"]
counts = INDEX["counts"]
assert counts["logical_artifacts"] == len(INDEX["artifacts"])
assert counts["objects"] == len(OBJECTS)
assert counts["git_reused_artifacts"] == sum("git" in v for v in INDEX["artifacts"].values())
assert counts["object_bytes"] == sum(map(len, OBJECTS.values()))
assert counts["original_runs"] == len(INDEX["runs"]) == 35
assert counts["real_rounds"] == real_count == 3
print(
    f'PASS: {counts["logical_artifacts"]} originals / {counts["objects"]} SHA objects + '
    f'{counts["git_reused_artifacts"]} Git references / 12 Git delivery paths / '
    '431 to 432 source closure / 420 to 421 fixed dependencies / '
    '3503 author and 3506 independent runtime fingerprints / '
    '35 original checks including 3 actual exits and double cleanups; '
    'author 4+1 and independent 2 preserved; no product execution'
)
