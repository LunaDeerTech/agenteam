#!/usr/bin/env python3
"""Independent exact-child mapping checks through actual main; no resources."""
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
import contextlib
import hashlib
import importlib.util
import io
import json
import os
import sys
import tempfile

sys.dont_write_bytecode = True
root = Path('/workspace/agenteam-knowledge-independent')
source = root / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
driver = root / '.agent-state/work-owner-http/root_chain_driver.py'
spec = importlib.util.spec_from_file_location('review_sup', source)
s = importlib.util.module_from_spec(spec)
spec.loader.exec_module(s)
top = 'TestKnowledgeB02IndependentTreeReference'
child = top + '/revoked_persisted_public_receipt_identity_and_old_attachment'
selector = '^' + top + '$/^' + child.split('/')[1] + '$'
good = (f'=== RUN   {top}\n=== RUN   {child}\n--- PASS: {top} (1.0s)\n'
        f'    --- PASS: {child} (0.8s)\nD03 explicit test actual_wait pid=987 code=0 selector={selector}\n')
cases = [
    ('positive', good, False, True),
    ('unreadable bytes', good.encode() + b'\xff', False, False),
    ('read OSError', good, True, False),
    ('zero child', good.replace(f'=== RUN   {child}\n', '').replace(f'    --- PASS: {child} (0.8s)\n', ''), False, False),
    ('parent SKIP', good.replace(f'--- PASS: {top} ', f'--- SKIP: {top} '), False, False),
    ('child FAIL', good.replace(f'--- PASS: {child}', f'--- FAIL: {child}'), False, False),
    ('duplicate PASS', good + f'    --- PASS: {child} (0.1s)\n', False, False),
    ('extra child', good + f'=== RUN   {top}/other\n    --- PASS: {top}/other (0.1s)\n', False, False),
    ('missing Wait', good[:good.index('D03 explicit')], False, False),
]
here = Path(__file__).resolve().parents[2] / 'output/ai/work-owner-planning-ui/implementation/knowledge-reference-review'
here.mkdir(parents=True, exist_ok=True)
for name, payload, read_error, accept in cases:
    with tempfile.TemporaryDirectory(dir=here) as temporary:
        parent = Path(temporary)
        binary = parent / 'not-executed.test'
        binary.write_bytes(b'controlled identity only')
        waits = []
        class Child:
            pid, returncode = 765, None
            def __init__(self, args, stdout, stderr):
                assert args[args.index('--run') + 1] == selector
                run = Path(args[args.index('--directory') + 1])
                (run / 'runtime').mkdir(parents=True)
                resources = []
                for label, kinds in [('agenteam.d05.objectfixture', ['container','network']), ('agenteam.d04.networkfixture', ['container','network']), ('agenteam.d03.fixture', ['container','container','network'])]:
                    for kind in kinds:
                        resources.append({'kind':kind, 'id':f'{len(resources)+1:064x}', 'label':label, 'nonce':'1'*32})
                owned = run / 'owned.json'
                owned.write_text(json.dumps({'kind':'work-owner-root-chain', 'resources':resources, 'directories':[str(run/'runtime'/part) for part in ('object','outbound','pg')]}))
                owned.chmod(0o600)
                stdout.flush()
                os.write(stdout.fileno(), payload if isinstance(payload, bytes) else payload.encode())
            def wait(self, timeout):
                waits.append(timeout)
                self.returncode = 0
                return 0
        original_read = Path.read_text
        def read(path, *args, **kwargs):
            if read_error and path.suffix == '.log':
                raise OSError('controlled log read error')
            return original_read(path, *args, **kwargs)
        adapter = SimpleNamespace(TARGETS={selector:'tests/knowledge'}, input_paths=lambda _: [source, driver], sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest())
        argv = ['review','--root-chain','--driver',str(driver),'--binary',str(binary),'--run',selector,'--output',str(parent/'out')]
        with patch.object(sys,'argv',argv), patch.object(s,'root_adapter',return_value=adapter), \
             patch.object(s.ctypes,'CDLL',return_value=SimpleNamespace(prctl=lambda *_:0)), \
             patch.object(s.subprocess,'Popen',Child), patch.object(s,'descendants',return_value=set()) as desc, \
             patch.object(s.os,'waitpid',side_effect=ChildProcessError), patch.object(s,'tcp',return_value=set()) as tcp, \
             patch.object(s,'exact_absent',return_value=True) as absent, patch.object(s.time,'sleep',return_value=None), \
             patch.object(Path,'read_text',read), contextlib.redirect_stdout(io.StringIO()):
            result = s.main()
        log = next((parent/'out').glob('*.log')).read_bytes().decode('utf-8', errors='replace')
        if result != (0 if accept else 1):
            print(log)
        assert result == (0 if accept else 1), name
        assert waits == [540] and absent.call_count == 14 and desc.call_count == 3 and tcp.call_count == 3, name
        for round in (1,2):
            for line in (f'ROOT private_observation={round} absent=True', f'ROOT runtime_observation={round} empty=True', f'OWNED runtime_observation={round} descendants=[]', f'HOST_TCP delta_empty_observation={round}'):
                assert line in log, (name,line)
        assert f'SUPERVISOR inputs_unchanged=True terminal={result}' in log, name
        print('PASS', name, 'full controlled Wait/resource/private/runtime/desc/TCP/input tail')
print('9 independent actual-main cases; resources_started=False')
