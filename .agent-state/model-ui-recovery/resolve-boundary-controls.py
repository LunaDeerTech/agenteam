#!/usr/bin/env python3
"""Actual Account boundary -> formal Problem schema -> client/observer, offline.

The Go helper invokes the production method with a Recorder and the real
request-ID middleware. It does not construct a Service, server or fixture.
The JS layer keeps its explicit transport/jsdom doubles; no browser is run.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile

from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "output/ai/model-ui-recovery/resolve-boundary-controls"
GO = "/workspace/toolchains/go1.27.1/bin/go"
SOURCE = r'''package main
import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "github.com/LunaDeerTech/agenteam/internal/central/account"
 "github.com/LunaDeerTech/agenteam/internal/central/foundation"
 "github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)
type response struct {
 Status int `json:"status"`
 Header http.Header `json:"header"`
 Body json.RawMessage `json:"body"`
}
func main() {
 output := map[string]response{}
 boundary := &account.HTTPBoundary{}
 handler := httpapi.WithRequestID(nil, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
  for name, code := range map[string]foundation.Code{"404":foundation.NotFound, "409":foundation.ProjectNotActive} {
   recorder := httptest.NewRecorder()
   boundary.WriteProblem(recorder, r, foundation.NewFault(code, foundation.NotStarted))
   output[name] = response{recorder.Code, recorder.Header(), append(json.RawMessage(nil), recorder.Body.Bytes()...)}
  }
 }))
 request := httptest.NewRequest("GET", "https://owned.invalid/api/v1/projects/resolve?username=boundary-canary&project_name=private-canary", nil)
 request.Header.Set("X-Request-ID", "caller-canary")
 handler.ServeHTTP(httptest.NewRecorder(), request)
 if err := json.NewEncoder(os.Stdout).Encode(output); err != nil { panic("boundary-control-output") }
}
'''


def main():
    OUTPUT.mkdir(parents=True, exist_ok=True)
    environment = {key: value for key, value in os.environ.items() if not key.startswith("AGENTEAM_")}
    environment.update({
        "GOTOOLCHAIN": "local", "GOENV": "off", "GOWORK": "off", "GOPROXY": "off", "GOSUMDB": "off", "GOTELEMETRY": "off",
        "GOFLAGS": "-mod=readonly -p=2", "GOMAXPROCS": "2",
        "GOCACHE": "/workspace/agenteam/output/ai/model-ui-recovery/go-build",
        "GOMODCACHE": "/workspace/agenteam/output/ai/model-ui-recovery/go-mod",
    })
    schema = json.loads((ROOT / "api/openapi/common.json").read_text())
    schema["$ref"] = "#/components/schemas/Problem"
    validator = Draft202012Validator(schema)
    with tempfile.TemporaryDirectory(prefix="boundary-", dir=OUTPUT) as temporary:
        temporary = Path(temporary)
        source = temporary / "main.go"
        source.write_text(SOURCE)
        environment["GOTMPDIR"] = str(temporary)
        actual = subprocess.run([GO, "run", str(source)], cwd=ROOT, env=environment, capture_output=True, check=True, timeout=90)
        records = json.loads(actual.stdout)
        assert set(records) == {"404", "409"}
        for status, record in records.items():
            validator.validate(record["body"])
            assert record["status"] == record["body"]["status"] == int(status)
            assert record["body"]["instance"] == "/api/v1"
            assert record["header"]["X-Request-Id"] == [record["body"]["request_id"]]
            assert record["header"]["Content-Type"] == ["application/problem+json"]
            assert "canary" not in json.dumps(record)
        assert records["404"]["body"]["request_id"] == records["409"]["body"]["request_id"]
        # Both paths are schema-valid: exact producer identity is an additional
        # diagnostic binding, never an endpoint constraint invented by schema.
        endpoint = {**records["404"]["body"], "instance": "/api/v1/projects/resolve"}
        validator.validate(endpoint)
        invalid = {**endpoint, "instance": "/api/v1?private=canary"}
        assert not validator.is_valid(invalid)
        data = temporary / "boundary.json"
        descriptor = os.open(data, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w") as stream:
            json.dump(records, stream)
        environment["AGENTEAM_RESOLVE_BOUNDARY_INPUT"] = str(data)
        # Run the actual transformed observer first so the old endpoint literal
        # produces a direct red control against the real boundary response.
        for name in ("resolve-publication-observer-controls.cjs", "resolve-publication-controls.cjs", "resolve-rejection-controls.cjs"):
            child = subprocess.run(["node", str(ROOT / ".agent-state/model-ui-recovery" / name)], cwd=ROOT, env=environment, check=False, timeout=60)
            assert child.returncode == 0, f"{name} actual_wait={child.returncode}"
    print(json.dumps({"actual_boundary": True, "formal_schema": True, "statuses": [404, 409], "schema_endpoint_path_valid": True,
                      "schema_query_rejected": True, "original_account_binary_rebuilt": False, "browser": False, "network": False}))


if __name__ == "__main__":
    main()
