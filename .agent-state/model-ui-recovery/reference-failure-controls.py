#!/usr/bin/env python3
"""Run actual reference-failure projection source and tests without a fixture.

Only named pure declarations are extracted. Foundation/postgres/pgconn remain
real imports; no database, app, browser, TestMain or replacement type is used.
"""
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "output/ai/model-ui-recovery/reference-failure-control"
GO = "/workspace/toolchains/go1.27.1/bin/go"


def declaration(path, name):
    source = path.read_text()
    marker = "func " + name + "("
    assert source.count(marker) == 1
    start = source.index(marker)
    end = source.find("\nfunc ", start + len(marker))
    return source[start:] if end == -1 else source[start:end]


fixture = ROOT / "tests/account/project_owner_models_web_fixture_test.go"
tests = ROOT / "tests/account/project_owner_models_web_test.go"
source = '''package referencecontrol
import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "testing"
 "github.com/LunaDeerTech/agenteam/internal/central/foundation"
 "github.com/LunaDeerTech/agenteam/internal/central/postgres"
 "github.com/jackc/pgx/v5/pgconn"
)
'''
for path, names in ((fixture, ("projectModelsWebReferenceFailure", "projectModelsWebSQLState", "projectModelsWebIPCFailure")),
                    (tests, ("TestProjectModelsWebReferenceFailureProjection", "TestProjectModelsWebReferenceSQLState", "TestProjectModelsWebIPCFailureProjection"))):
    source += "\n".join(declaration(path, name) for name in names) + "\n"
OUTPUT.mkdir(parents=True, exist_ok=True)
target = OUTPUT / "projection_test.go"
target.write_text(source)
environment = {key: value for key, value in os.environ.items() if not key.startswith("AGENTEAM_")}
environment.update({"GOTOOLCHAIN": "local", "GOENV": "off", "GOWORK": "off", "GOPROXY": "off", "GOSUMDB": "off", "GOTELEMETRY": "off", "GOFLAGS": "-mod=readonly -p=2", "GOMAXPROCS": "2",
                    "GOCACHE": str(ROOT / "output/ai/model-ui-recovery/go-build"), "GOMODCACHE": str(ROOT / "output/ai/model-ui-recovery/go-mod")})
os.chdir(ROOT)
os.execve(GO, [GO, "test", "-race", "-count=1", "-v", "-timeout=30s", "-run=^TestProjectModelsWeb(ReferenceFailureProjection|ReferenceSQLState|IPCFailureProjection)$", str(target)], environment)
