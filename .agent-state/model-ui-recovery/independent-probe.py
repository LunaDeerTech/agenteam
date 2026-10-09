#!/usr/bin/env python3
"""Read-only D27 harness probe; no browser, database, or dispatcher is started.

Run from any directory with Python 3. Extracts only the two reviewed sources
from the recorded Git revision into a temporary directory that is removed.
The args-admission test is expected to fail until typed admission is added.
"""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
GO = "/workspace/toolchains/go1.27.1/bin/go"
REVISION = "02e3daaf"

GO_PROBE = r'''
package account_test

import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestIndependentPrivateBoundaries(t *testing.T) {
    path := filepath.Join(t.TempDir(), "private")
    for _, input := range []struct{ raw []byte; limit int64; accept bool }{
        {[]byte("01234567"), 8, true},
        {[]byte("012345678"), 8, false},
        {nil, 8, false},
        {[]byte{0xff}, 8, false},
    } {
        if err := os.WriteFile(path, input.raw, 0600); err != nil { t.Fatal("probe fixture write failed") }
        raw, err := projectModelsWebReadPrivate(path, input.limit)
        if (err == nil) != input.accept || !input.accept && len(raw) != 0 { t.Error("private boundary disagreed") }
    }
}

func TestIndependentJSONBoundaries(t *testing.T) {
    for _, raw := range []string{
        `{"x":0,"\u0078":1}`,
        `{"x":[{"a":0,"\u0061":1}]}`,
        `{"\ud800":0}`,
        `{"x":"\udbff\udbff"}`,
        `{"x":"\udbff\udfff\udc00"}`,
        `{"x":"\"\ud800"}`,
        `{"x":1}null`,
        `{"x":1}x`,
        `{"x":` + strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66) + "}",
    } {
        if _, err := projectModelsWebObject([]byte(raw), "x"); err == nil { t.Error("invalid JSON boundary admitted") }
    }
    for _, raw := range []string{
        `{"x":"\ud800\udc00"}`,
        `{"x":"\udbff\udfff"}`,
        `{"x":"\\ud800"}`,
        `{"x":"\"safe"}`,
        "{\"x\":\"\ufffd\"}\n \t",
    } {
        if _, err := projectModelsWebObject([]byte(raw), "x"); err != nil { t.Error("valid JSON boundary rejected") }
    }
}

func TestIndependentIPCValueAdmission(t *testing.T) {
    inputHash := strings.Repeat("a", 64)
    for _, tc := range []struct { name, action string; args any }{
        {"snapshot_null", "snapshot", map[string]any{"project": nil}},
        {"arm_invalid_values", "arm", map[string]any{"operation": "unknown", "project": 0, "target_id": false, "query": []string{}, "effect": "arbitrary"}},
        {"control_wrong_type", "control-state", map[string]any{"arm_id": 4}},
        {"release_null_token", "release", map[string]any{"arm_id": "a0001", "request_token": nil}},
        {"logout_wrong_type", "logout", map[string]any{"session_id": map[string]any{}}},
        {"archive_wrong_union", "archive-recovery-project", map[string]any{"project": "main", "expected_version": "0"}},
        {"reference_wrong_union", "reference-fact", map[string]any{"project": "main", "state": "arbitrary"}},
        {"rename_wrong_union", "rename-reuse", map[string]any{"project": "referenced"}},
    } {
        t.Run(tc.name, func(t *testing.T) {
            raw, err := json.Marshal(map[string]any{"protocol": projectModelsWebProtocol, "input_hash": inputHash, "sequence": 1, "action": tc.action, "args": tc.args})
            if err != nil { t.Fatal("probe envelope encoding failed") }
            if _, code := decodeProjectModelsWebIPC(raw, inputHash, 1); code == "" {
                t.Error("invalid typed/union argument admitted by envelope-only decoder; full admission remains incomplete")
            }
        })
    }
}
'''

NODE_PROBE = r'''
import { mkdirSync, writeFileSync, symlinkSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
const root = process.argv[1], temp = process.argv[2];
const dir = join(temp, "private"), evidence = join(temp, "evidence"), dist = join(temp, "dist");
for (const path of [dir, evidence, dist]) mkdirSync(path, {mode: 0o700});
writeFileSync(join(dist, "index.html"), "<!doctype html><title>owned probe</title>");
symlinkSync(dir, join(temp, "private-link"));
const base = {
  AGENTEAM_AUTH_WEB_ORIGIN: "http://127.0.0.1:43123",
  AGENTEAM_AUTH_WEB_PRIVATE: dir,
  AGENTEAM_AUTH_WEB_CHROMIUM: "/owned-probe/chromium",
  AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE: evidence,
  AGENTEAM_PROJECT_MODELS_WEB_DIST: dist,
  AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH: "a".repeat(64),
  AGENTEAM_PROJECT_MODELS_WEB_CASE: "configuration",
  AGENTEAM_AUTH_WEB_IMAGES: join(temp, "images"),
};
const cases = [
  ...["configuration", "credential", "recovery", "read", "authority", "navigation"].map(mode => ({name: mode, env: {AGENTEAM_PROJECT_MODELS_WEB_CASE: mode}, accept: true})),
  {name: "unknown-case", env: {AGENTEAM_PROJECT_MODELS_WEB_CASE: "other"}, accept: false},
  {name: "bad-hash", env: {AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH: "A".repeat(64)}, accept: false},
  {name: "nonloopback", env: {AGENTEAM_AUTH_WEB_ORIGIN: "http://example.invalid:43123"}, accept: false},
  {name: "origin-path", env: {AGENTEAM_AUTH_WEB_ORIGIN: "http://127.0.0.1:43123/"}, accept: false},
  {name: "relative-private", env: {AGENTEAM_AUTH_WEB_PRIVATE: "relative"}, accept: false},
  {name: "symlink-private", env: {AGENTEAM_AUTH_WEB_PRIVATE: join(temp, "private-link")}, accept: false},
  {name: "navigation-no-images", env: {AGENTEAM_PROJECT_MODELS_WEB_CASE: "navigation", AGENTEAM_AUTH_WEB_IMAGES: ""}, accept: false},
];
let failures = 0, n = 0;
for (const tc of cases) {
  Object.assign(process.env, base, tc.env);
  let config, accepted = false;
  try {
    ({default: config} = await import(pathToFileURL(join(temp, "project-owner-models.config.js")).href + `?independent=${++n}`));
    accepted = true;
  } catch {}
  let valid = accepted === tc.accept;
  if (accepted) {
    const chosen = process.env.AGENTEAM_PROJECT_MODELS_WEB_CASE;
    valid &&= config.timeout === 45000 && config.workers === 1 && config.retries === 0 && config.forbidOnly === true;
    valid &&= config.testMatch === "project-owner-models.spec.ts";
    valid &&= config.grep.test(`test [${chosen}]`) && !config.grep.test("test [other]");
    valid &&= config.use.screenshot === "off" && config.use.trace === "off" && config.use.video === "off";
  }
  if (!valid) failures++;
  console.log(`${valid ? "PASS" : "FAIL"} config ${tc.name}`);
}
console.log("No browser, HTTP, or database process was started.");
process.exitCode = failures ? 1 : 0;
'''


def main():
    with tempfile.TemporaryDirectory(prefix="independent-run-", dir=Path(__file__).parent) as name:
        temporary = Path(name)
        fixture = temporary / "fixture_test.go"
        for destination, source in [
            (fixture, "tests/account/project_owner_models_web_fixture_test.go"),
            (temporary / "project-owner-models.config.js", "tests/account-captcha-web/project-owner-models.config.js"),
        ]:
            content = subprocess.run(["git", "show", f"{REVISION}:{source}"], cwd=ROOT, check=True, capture_output=True).stdout
            destination.write_bytes(content)
        (temporary / "package.json").write_text('{"type":"module"}')
        (temporary / "node_modules").symlink_to(ROOT / "tests/account-captcha-web/node_modules")
        probe = temporary / "probe_test.go"
        probe.write_text(GO_PROBE)
        env = dict(os.environ, GOTOOLCHAIN="local")
        go = subprocess.run([GO, "test", "-race", "-count=1", "-v", str(fixture), str(probe)], cwd=ROOT, env=env)
        node = subprocess.run(["node", "--input-type=module", "-e", NODE_PROBE, str(ROOT), name], cwd=ROOT)
        print(f"REVISION={REVISION}; GO_PROBE_EXIT={go.returncode}; NODE_CONFIG_EXIT={node.returncode}", flush=True)
        return 1 if go.returncode or node.returncode else 0


if __name__ == "__main__":
    raise SystemExit(main())
