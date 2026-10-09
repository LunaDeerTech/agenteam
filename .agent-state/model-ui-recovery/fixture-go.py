#!/usr/bin/env python3
"""Closed adapter: accepted fixture helpers plus one precompiled account top.

The public fixture scripts remain byte-for-byte unchanged. Their broad go test
invocation is admitted exactly, then scoped to the already compiled account
binary. No source compilation occurs inside a timed business run.
"""
import json
import os
from pathlib import Path
import re
import shutil
import sys

GO = "/workspace/toolchains/go1.27.1/bin/go"
ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "output/ai/model-ui-recovery"
DELIVERY = Path("/workspace/agenteam-delivery")
BUILD = {
    "./tests/testsupport/objectstore/cmd/fixture": "object-fixture",
    "./tests/testsupport/outbound/cmd/fixture": "outbound-fixture",
    "./tests/testsupport/outbound/cmd/server": "outbound-server",
    "./tests/testsupport/postgres/cmd/fixture": "postgres-fixture",
}
PACKAGES = ["./internal/central/postgres/...", "./tests/database/...", "./internal/central/app/...", "./tests/process/...", "./tests/security/...", "./tests/outbox/...", "./internal/central/outbox/...", "./tests/account/...", "./tests/accountmail/...", "./internal/central/accountmail/...", "./internal/central/recoverylog/...", "./tests/project/...", "./internal/central/model/...", "./tests/model/...", "./tests/objects/...", "./internal/central/object/..."]


def main():
    args = sys.argv[1:]
    if args == ["env", "GOVERSION"]:
        os.execv(GO, [GO, *args])
    if len(args) == 4 and args[:2] == ["build", "-o"] and args[3] in BUILD:
        target = Path(args[2]).resolve()
        private = Path(os.environ["MODELS_PRIVATE_ROOT"]).resolve()
        assert target.is_relative_to(private) and not target.exists()
        shutil.copyfile(OUTPUT / "helpers" / BUILD[args[3]], target)
        target.chmod(0o500)
        return 0
    selector = os.environ["MODELS_EXACT_SELECTOR"]
    assert args == ["test", "-tags=integration", "-race", "-count=1", "-timeout=6m", "-run=" + selector, *PACKAGES]
    resources = {}
    for variable, label in [("AGENTEAM_OBJECT_FIXTURE", "agenteam.d05.objectfixture"), ("AGENTEAM_OUTBOUND_FIXTURE", "agenteam.d04.networkfixture"), ("AGENTEAM_PG_FIXTURE", "agenteam.d03.fixture"), ("AGENTEAM_PG_UNSUPPORTED_FIXTURE", "agenteam.d03.fixture")]:
        path = Path(os.environ[variable])
        assert path.resolve().is_relative_to(Path(os.environ["MODELS_PRIVATE_ROOT"]).resolve())
        descriptor = json.loads(path.read_bytes())
        assert re.fullmatch(r"[0-9a-f]{32}", descriptor["nonce"])
        for key, kind in [("container_id", "container"), ("network_id", "network")]:
            resource = descriptor[key]
            assert re.fullmatch(r"[0-9a-f]{64}", resource)
            row = {"id": resource, "kind": kind, "label": label, "nonce": descriptor["nonce"]}
            assert resource not in resources or resources[resource] == row
            resources[resource] = row
    assert len(resources) == 7 and sum(row["kind"] == "container" for row in resources.values()) == 4
    evidence = Path(os.environ["AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE"])
    path = evidence / "resources.json"
    with path.open("x") as output:
        json.dump(list(resources.values()), output)
    path.chmod(0o600)
    print("MODELS exact precompiled account selector; four containers and three networks registered", flush=True)
    binary = OUTPUT / "account-delivery.test"
    os.chdir(DELIVERY / "tests/account")
    os.execv(binary, [str(binary), "-test.v", "-test.count=1", "-test.timeout=6m", "-test.run=" + selector])


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:
        print("MODELS_OWNED_FIXTURE_ADAPTER_REJECTED", file=sys.stderr)
        sys.exit(1)
