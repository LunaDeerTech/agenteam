#!/usr/bin/env python3
"""Offline discovery checks against the actual receipt-fixed test executable.

Never invokes a business -run or any fixture main. The small Go overlay executes
the actual lower selectedTestTarget/command/matchesListing methods with -list.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parents[2]
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
BINARY = ROOT / 'output/ai/knowledge-independent/knowledge-independent-receipt-fixed-race.test'
PARENT = '^TestKnowledgeB02IndependentTreeReference$'
SELECTOR = PARENT + '/^revoked_persisted_public_receipt_identity_and_old_attachment$'
TOP = 'TestKnowledgeB02IndependentTreeReference'

PROBE = r'''package main

import (
    "context"
    "os"
    "reflect"
    "strings"
    "testing"
    "time"
)

func TestExplicitFixtureDiscoveryActualReceiptBinary(t *testing.T) {
    binary := os.Getenv("AGENTEAM_DISCOVERY_PROBE_BINARY")
    cwd := os.Getenv("AGENTEAM_DISCOVERY_PROBE_CWD")
    parent := "^TestKnowledgeB02IndependentTreeReference$"
    full := parent + "/^revoked_persisted_public_receipt_identity_and_old_attachment$"
    for _, tc := range []struct { name, filter, wantList string; want bool }{
        {"approved_parent", full, parent, true},
        {"wrong_parent", "^TestKnowledgeB02IndependentMissing$", "^TestKnowledgeB02IndependentMissing$", false},
        {"unapproved_child", parent + "/^wrong_child$", parent + "/^wrong_child$", false},
    } {
        t.Run(tc.name, func(t *testing.T) {
            target, err := selectedTestTarget(binary, cwd, tc.filter)
            if err != nil { t.Fatal(err) }
            ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
            defer cancel()
            cmd := target.command(ctx, true)
            if !reflect.DeepEqual(cmd.Args, []string{binary, "-test.list=" + tc.wantList}) || cmd.Dir != cwd {
                t.Fatalf("discovery command drift: %q", cmd.Args)
            }
            raw, err := cmd.Output()
            if err != nil { t.Fatalf("actual list/Wait: %v", err) }
            if cmd.ProcessState == nil || !cmd.ProcessState.Success() { t.Fatal("actual list not reaped successfully") }
            if strings.Contains(string(raw), "=== RUN") { t.Fatal("business test executed during discovery") }
            if got := target.matchesListing(string(raw)); got != tc.want {
                t.Fatalf("actual lower precheck=%v want=%v, listing=%q", got, tc.want, raw)
            }
            run := target.command(context.Background(), false)
            want := []string{binary, "-test.v", "-test.count=1", "-test.timeout=6m", "-test.run=" + tc.filter}
            if !reflect.DeepEqual(run.Args, want) { t.Fatalf("execution selector or budget changed: %q", run.Args) }
        })
    }
}
'''


def main():
    if not BINARY.is_file() or BINARY.stat().st_size != 36877449 or not os.access(BINARY, os.X_OK):
        raise RuntimeError('original 67286 receipt-fixed executable required')
    env = os.environ.copy()
    for name in ('AGENTEAM_PG_FIXTURE', 'AGENTEAM_PG_UNSUPPORTED_FIXTURE',
                 'AGENTEAM_OBJECT_FIXTURE', 'AGENTEAM_OUTBOUND_FIXTURE',
                 'AGENTEAM_FIXTURE_TEST_BINARY', 'AGENTEAM_FIXTURE_TEST_CWD',
                 'AGENTEAM_FIXTURE_OWNED_RECORD'):
        env.pop(name, None)
    env.update({
        'PATH': str(GO.parent) + os.pathsep + env.get('PATH', ''),
        'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off',
        'GOTELEMETRY': 'off', 'GOMAXPROCS': '2', 'GOFLAGS': '-mod=readonly',
        'GOMODCACHE': '/workspace/agenteam/output/ai/model-ui-recovery/go-mod',
        'GOCACHE': '/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache',
        'GOTMPDIR': str(ROOT / 'output/ai/knowledge-independent/tmp'),
        'AGENTEAM_DISCOVERY_PROBE_BINARY': str(BINARY),
        'AGENTEAM_DISCOVERY_PROBE_CWD': str(ROOT / 'tests/knowledge'),
    })
    # These invoke the actual candidate directly. Empty successful -list is
    # deliberately negative discovery evidence, never a successful business run.
    cases = (
        ('exact_parent', PARENT, [TOP]),
        ('full_slash_is_not_a_top', SELECTOR, []),
        ('wrong_parent', '^TestKnowledgeB02IndependentMissing$', []),
        ('unchanged_two_tops', '^TestKnowledgeB02Independent(Content|TreeReference)$',
         ['TestKnowledgeB02IndependentContent', TOP]),
    )
    for name, selector, expected in cases:
        result = subprocess.run([str(BINARY), '-test.list=' + selector],
                                cwd=ROOT / 'tests/knowledge', env=env,
                                capture_output=True, text=True, timeout=15)
        if result.returncode != 0 or result.stdout.splitlines() != expected or result.stderr:
            raise AssertionError((name, result.returncode, result.stdout, result.stderr))
        print(f'actual_candidate_list {name} exit=0 names={len(expected)}', flush=True)
    with tempfile.TemporaryDirectory(prefix='fixture-discovery-', dir=env['GOTMPDIR']) as directory:
        directory = Path(directory)
        probe = directory / 'discovery_probe_test.go'
        probe.write_text(PROBE)
        overlay = directory / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': {
            str(ROOT / 'tests/testsupport/postgres/cmd/fixture/discovery_probe_test.go'): str(probe),
        }}))
        result = subprocess.run([str(GO), 'test', '-race', '-count=1', '-p=1',
                                 '-overlay=' + str(overlay), '-timeout=45s', '-v',
                                 '-run=^TestExplicitFixture', './tests/testsupport/postgres/cmd/fixture'],
                                cwd=ROOT, env=env, timeout=60)
        if result.returncode != 0:
            raise RuntimeError(f'actual lower discovery controls failed: {result.returncode}')
    print('PASS actual candidate 4 list controls and lower fixture methods; no business/fixture resource run', flush=True)


if __name__ == '__main__':
    main()
