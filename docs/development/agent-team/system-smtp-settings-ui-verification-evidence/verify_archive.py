#!/usr/bin/env python3
"""Read-only archive/Git verification; never execute archived code or tests."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
INDEX = json.loads((HERE / "index.json").read_bytes())
PRODUCT = "628612cdfc730cc1d89cad4e24a5d20f36cc6812"
REAL_BASE = "f670cb1fe1f07ebd21bdb90a2b96cd565c33f05a"
MAIN_BASE = "63de0ac675e0fd4cd26e2cecde91f417a63d3bca"
TECH = "910b51cf93f8a97e04641f31d9bc1e76f9b9a7c95850b2a72edad7da3e269d86"


def digest(data):
    return hashlib.sha256(data).hexdigest()


assert INDEX["accepted_product_commit"] == PRODUCT
assert INDEX["real_product_base"] == REAL_BASE
assert INDEX["main_go_base"] == MAIN_BASE and INDEX["technical_sha256"] == TECH
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
        return GIT[(meta["git"]["commit"], meta["git"]["path"])]
    return OBJECTS[meta["sha256"]]


def item(name):
    return json.loads(blob(name))


delivery = item("author/delivery29-v2/manifest.json")["files"]
assert delivery == INDEX["delivery"] and len(delivery) == 29
original28 = item("author/input04/manifest.json")["files"]
assert len(original28) == 28
assert {p: s for p, s in delivery.items() if p != "docs/development/frontend/README.md"} == original28
assert item("independent/delivery28.json")["files"] == original28
requests = {(PRODUCT, p): s for p, s in delivery.items()}
requests[(PRODUCT, INDEX["card"])] = None
stages = {}
for name, stage in INDEX["stage_baselines"].items():
    frozen = item(stage["manifest"])
    stages[name] = frozen
    assert len(frozen["files"]) == stage["sources"] == 28
    assert len(frozen["dist"]) == stage["dist_fingerprints_only"] == 36
    assert frozen["product_base"] == stage["base"]
    assert len(frozen["accepted_baseline"]["files"]) == stage["accepted_files"]
    for p, meta in frozen["accepted_baseline"]["files"].items():
        requests[(stage["base"], p)] = meta["sha256"]
main = item("main-combination/input01.json")
assert main["fixed_base"] == MAIN_BASE and main["candidate28"] == original28
assert len(main["go_inputs"]) == 604 and len(main["external_go_inputs"]) == 3504
assert len(main["accepted_git_inputs"]) == 602
for p, value in main["go_inputs"].items():
    requests[(PRODUCT if p in delivery else MAIN_BASE, p)] = value
for meta in INDEX["artifacts"].values():
    if "git" in meta:
        requests[(meta["git"]["commit"], meta["git"]["path"])] = meta["sha256"]
env = dict(os.environ, GIT_NO_LAZY_FETCH="1", GIT_OPTIONAL_LOCKS="0")
changed = subprocess.run(
    ["git", "diff-tree", "--no-commit-id", "--name-only", "-r", PRODUCT],
    cwd=ROOT, env=env, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
).stdout.decode().splitlines()
assert set(changed) == set(delivery)
order = list(requests)
raw = subprocess.run(
    ["git", "cat-file", "--batch"], cwd=ROOT, env=env, check=True,
    input="".join(f"{commit}:{path}\n" for commit, path in order).encode(),
    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
).stdout
offset = 0
for key in order:
    end = raw.index(b"\n", offset)
    header = raw[offset:end].split()
    assert len(header) == 3 and header[1] == b"blob", (key, header)
    size = int(header[2])
    data = raw[end + 1:end + 1 + size]
    assert len(data) == size and raw[end + size + 1:end + size + 2] == b"\n"
    offset = end + size + 2
    if requests[key]:
        assert digest(data) == requests[key], key
    GIT[key] = data
assert offset == len(raw)
for name, meta in INDEX["artifacts"].items():
    data = blob(name)
    assert digest(data) == meta["sha256"] and len(data) == meta["bytes"], name
for p in delivery:
    assert blob("author/delivery29-v2-source/" + p) == GIT[(PRODUCT, p)]
for data in [blob("author/card.md"), GIT[(PRODUCT, INDEX["card"])],
             (ROOT / INDEX["card"]).read_bytes()]:
    assert digest(data[data.index(b"## 1."):]) == TECH
known = {digest(v) for v in GIT.values()} | set(OBJECTS)
for category in INDEX["source_versions"].values():
    for path, variants in category.items():
        for value, meta in variants.items():
            assert digest(blob(meta["artifact"])) == value
            assert meta["consumers"], path
for frozen in stages.values():
    assert set(frozen["files"].values()) <= known
for stage in ["api-stage01", "api-stage02", "owner-stage01", "web-stage01", "focus-stage01"]:
    values = item("author/" + stage + "/manifest.json")["files"].values()
    assert {v if isinstance(v, str) else v["sha256"] for v in values} <= known
v2 = item("author/delivery29-v2/validation.json")
replacement = v2["exact_replacement"]
readme = "docs/development/frontend/README.md"
assert blob("author/delivery29-source/" + readme).replace(
    replacement["before"].encode(), replacement["after"].encode(), 1
) == blob("author/delivery29-v2-source/" + readme)
assert v2["one_replacement_only"] and v2["links_unchanged"]


def exit_code(result):
    for key in ["driver_exit", "exit_code", "actual_exit", "exit"]:
        if key in result:
            return result[key]
    raise AssertionError("No recorded exit")


def check_raw(prefix, result):
    data = blob(prefix + "/raw.log")
    if "raw_sha256" in result:
        assert digest(data) == result["raw_sha256"], prefix
    return data


real_count = 0
baseline = None
for prefix, expected in INDEX["runs"].items():
    kind = expected["kind"]
    if kind == "author-tool-record-only":
        # The original tool records are retained, not upgraded to child wait evidence.
        if prefix.endswith("harness-format00"):
            assert exit_code(item(prefix + "/result.json")) == 1
        else:
            assert prefix.endswith("harness-type00")
            assert prefix + "/result.json" not in INDEX["artifacts"]
            assert prefix + "/invocation.json" in INDEX["artifacts"]
        assert prefix + "/raw.log" in INDEX["artifacts"]
        continue
    result = item(prefix + "/result.json")
    assert exit_code(result) == expected["exit"], prefix
    output = check_raw(prefix, result)
    command_name = prefix + "/command.json"
    command = item(command_name) if command_name in INDEX["artifacts"] else result
    assert command["argv"] and command["cwd"] and isinstance(command["env"], dict), prefix
    assert command.get("seconds", result.get("seconds", result.get("duration_seconds"))) == expected["seconds"], prefix
    if prefix + "/input-before.json" in INDEX["artifacts"]:
        before = item(prefix + "/input-before.json")
        assert before == item(prefix + "/input-after.json"), prefix
    else:
        before = result["input_before"]
        assert before == result["input_after"], prefix
    if "input_before_sha256" in result:
        assert result["input_before_sha256"] == digest(blob(prefix + "/input-before.json"))
        assert result["input_after_sha256"] == digest(blob(prefix + "/input-after.json"))
    if kind in ["author-offline", "independent-preparation"]:
        assert result["actual_wait"] is True, prefix
        if kind == "author-offline":
            assert result["input_unchanged"] is True, prefix
        else:
            assert result["resources_started"] is False, prefix
        if "candidates" in before:
            assert set(before["candidates"].values()) <= known, prefix
        continue
    if kind == "main-combination":
        assert result["actual_wait"] is True and result["inputs_unchanged"] is True
        assert result["resources_started"] is False
        assert result["first_scan"] == result["second_scan"] == {}
        assert result["adopted_waits"] == []
        continue
    assert kind in ["author-real", "real"], (prefix, kind)
    real_count += 1
    assert command["actual_wait_completed"] is True and command["exit"] == result["driver_exit"]
    for key in ["accepted_baseline_before", "accepted_baseline_after", "double_cleanup",
                "source_files_unchanged", "verification_inputs_unchanged", "browser_runtime_removed"]:
        assert result[key] is True, (prefix, key)
    assert result["monitor_errors"] == 0
    tops = [[a.decode(), b.decode()] for a, b in re.findall(rb"^--- (PASS|FAIL): ([^\s(]+)", output, re.M)]
    assert tops == result["top_levels"] == expected["top_levels"], prefix
    assert result["selected_tests_passed"] == all(x[0] == "PASS" for x in tops)
    assert before["accepted_baseline"]["accepted"] is True
    assert before["accepted_baseline"]["errors"] == []
    assert len(before["files"]) == 28 and len(before["dist"]) == 36
    assert set(before["files"].values()) <= known
    for p, value in before["accepted_baseline"]["files"].items():
        assert digest(GIT[(before["product_base"], p)]) == value
    binding = item(prefix + "/verification-input.json")
    assert binding["driver_sha256"] == digest(blob(prefix + "/driver.py.txt"))
    resources = item(prefix + "/observed-resources.json")
    processes = item(prefix + "/observed-processes.json")
    assert len(resources) == result["observed_resources"] == 7
    assert len(processes) == result["observed_processes"] == expected.get("owned_pid_starttime"), prefix
    for key, value in processes.items():
        assert key == f'{value["pid"]}:{value["starttime"]}'
    current_baseline = item(prefix + "/baseline.json")
    assert sorted(v["kind"] for v in current_baseline.values()) == ["container"] * 2 + ["network"] * 4
    if baseline is None:
        baseline = current_baseline
    assert current_baseline == baseline and set(resources).isdisjoint(baseline)
    scans = item(prefix + "/cleanup.json")
    assert len(scans) == 2 and scans[0]["time"] < scans[1]["time"]
    for scan in scans:
        assert scan["baseline_unchanged"] is True
        assert set(scan["exact_absent"]) == set(resources)
        assert all(v["absent"] is True for v in scan["exact_absent"].values())
        for key in ["owned_processes", "remaining_new", "runtime_entries", "browser_runtime_entries"]:
            assert scan[key] == [], (prefix, key)
    waits = item(prefix + "/adopted-waits.json")
    assert len(waits) == result["adopted_waits"] == expected.get("adopted_waits", expected.get("actual_adopted_waits"))
    assert item(prefix + "/monitor-errors.json") == []
assert real_count == 13

# Retain the original independent contract classifications and actual exits.
for prefix, code in [
    ("independent/api01/checks/api01", 1), ("independent/api02/checks/api02", 0),
    ("independent/owner/checks/owner01", 1), ("independent/page/checks/app02", 0),
]:
    result = item(prefix + "/result.json")
    assert exit_code(result) == code and result["actual_wait"] is True
    check_raw(prefix, result)
    assert item(prefix + "/input-before.json") == item(prefix + "/input-after.json")
assert b"1 passed | 11 skipped" in blob("independent/api02/checks/api02/raw.log")
assert b"1 failed | 6 passed" in blob("independent/owner/checks/owner01/raw.log")
assert b"1214 passed" in blob("author/runs/web-check04/raw.log")
assert b"35 passed" in blob("author/runs/web-check04/raw.log")
independent = "independent/runs/independent01"
execution = item(independent + "/execution.json")
assert len(execution["private_files"]) == 10
assert len(execution["harness_runtime"]) == 521 and len(execution["tools_runtime"]) == 14
assert execution["product_base"] == REAL_BASE
assert digest(blob(independent + "/execution.json")) == item(independent + "/verification-input.json")["execution_sha256"]
assert digest(blob(independent + "/frozen-input.json")) == item(independent + "/verification-input.json")["author_input_sha256"]
assert set(execution["private_files"].values()) <= known
for value in [
    "d50dc30178b4981a861bd12f7ba53b37e24ae76841591e40469d146223540043",
    "90c5c5e12d928f6a68db16fdc3b22e0dfc753fc0f07d08415ab4a66cab2c20b4",
    "fe429f596e6e9becb2f610e08dc65edcb37f9a39de5f779a48b488a12ab7b2b2",
]:
    assert value in known
main_tops = re.findall(rb"^--- PASS: ([^\s(]+)", blob("main-combination/runs/app-pure01/raw.log"), re.M)
assert set(main_tops) == {b"TestOutboundPolicyRootRoutePreservationAndSingleMiddleware", b"TestModelRootRouteOwnershipAndRequestPreservation"}
assert item("main-combination/result.json")["exit"] == 0
images = [k for k in INDEX["artifacts"] if k.startswith("author/runs/navigation04/images/")]
assert len(images) == 8 and all(blob(k).startswith(b"\x89PNG\r\n\x1a\n") for k in images)
assert INDEX["counts"]["logical_artifacts"] == len(INDEX["artifacts"])
assert INDEX["counts"]["physical_objects"] == len(OBJECTS)
print(json.dumps({
    "status": "PASS: original bytes and fixed Git only; no product execution",
    "logical_artifacts": len(INDEX["artifacts"]), "physical_objects": len(OBJECTS),
    "accepted_paths": len(delivery), "git_blobs": len(GIT), "real_rounds": real_count,
    "recorded_run_entries": len(INDEX["runs"]), "main_go_local": 604, "main_go_external_fingerprints": 3504,
    "dist_fingerprints_only": 36, "author_images": len(images),
    "historical_limits_preserved": True,
}, ensure_ascii=False))
