#!/usr/bin/env python3
"""Pure original response-observer controls; no PG/socket or Runner process."""
from pathlib import Path
import os
import json
import subprocess

here = Path(__file__).resolve().parent
subject = Path('/workspace/agenteam-runner-control')
output = here.parents[1] / 'output/ai/work-owner-planning-ui/implementation/runner-business-review'
output.mkdir(parents=True, exist_ok=True)
source = (subject / 'tests/runnercontrol/process_crash_linux_test.go').read_text()
start = source.index('\tproxy.ModifyResponse = func(')
end = source.index('\n\tproxy.ErrorHandler = ', start)
callback = source[start:end]
identity_start = source.index('func runnerCrashIdentity(')
identity_end = source.index('\nvar errRunnerCrashHeld', identity_start)
template = (here / 'response_test.go.txt').read_text()
generated = output / 'response_test.go'
generated.write_text(template.replace('// ORIGINAL_CALLBACK', callback).replace('// ORIGINAL_IDENTITY', source[identity_start:identity_end]))
virtual = subject / '.agent-state/runner_business_independent_test.go'
overlay = output / 'overlay.json'
overlay.write_text(json.dumps({'Replace': {str(virtual): str(generated)}}))
env = dict(os.environ, GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOPROXY='off', GOSUMDB='off', GOTELEMETRY='off', GOMAXPROCS='2',
           GOCACHE='/workspace/agenteam-work-ui/output/ai/work-owner-planning-ui/implementation/gocache',
           GOMODCACHE='/workspace/agenteam/output/ai/model-ui-recovery/go-mod', REVIEW_PRIVATE=str(output))
result = subprocess.run(['/workspace/toolchains/go1.27.1/bin/go', 'test', '-overlay', str(overlay), '-mod=readonly', '-p=1', '-race', '-count=1', '-timeout=15s', '-v', str(virtual)], cwd=subject, env=env)
raise SystemExit(result.returncode)
