#!/usr/bin/env python3
"""Offline exact-entry controls. No driver, Go, proc scan or socket is started."""
import ast
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SUP = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
PG = ROOT / '.agent-state/task-planning-recovery/pg_only_driver.go'
NATIVE = ROOT / '.agent-state/work-owner-http/native_driver.go'
BASE = '784ecf02'
namespace = {'__file__': str(SUP), '__name__': 'skills_entry_control'}
exec(compile(SUP.read_text(), str(SUP), 'exec'), namespace)
checks = 0


def check(value, label):
    global checks
    assert value, label
    checks += 1


def baseline(path):
    return subprocess.check_output(['git', 'show', BASE + ':' + str(path.relative_to(ROOT))], cwd=ROOT, text=True)


# Remove just the new exact branches from the real AST, then compare the entire
# prior supervisor (including every timeout/Wait/reaper/TCP/default-input node).
class Previous(ast.NodeTransformer):
    def visit_FunctionDef(self, node):
        if node.name in ('skill_http_inputs', 'skill_http_spawn', 'observe_skill_http'):
            return None
        return self.generic_visit(node)

    def visit_Assign(self, node):
        if any(isinstance(t, ast.Name) and (t.id.startswith('SKILL_HTTP_') or t.id == 'skill_selected') for t in node.targets):
            return None
        return self.generic_visit(node)

    def visit_If(self, node):
        if isinstance(node.test, ast.Name) and node.test.id == 'skill_selected':
            return [self.visit(x) for x in node.orelse]
        if isinstance(node.test, ast.BoolOp) and isinstance(node.test.values[0], ast.Name) and node.test.values[0].id == 'skill_selected':
            return None
        return self.generic_visit(node)


check(ast.dump(Previous().visit(ast.parse(SUP.read_text())), include_attributes=False)
      == ast.dump(ast.parse(baseline(SUP)), include_attributes=False), 'entire old supervisor AST preserved')
pg = PG.read_text()
pg_inverse = re.sub(r'^const skillOwnerHTTPSelector = .*\n\n', '', pg, flags=re.M)
pg_inverse = pg_inverse.replace(' && *selector != skillOwnerHTTPSelector', '')
pg_inverse = re.sub(r'\tif \*selector == skillOwnerHTTPSelector \{\n(?:\t[^\n]*\n)*?\t\}\n', '', pg_inverse)
check(pg_inverse == baseline(PG), 'entire old PG driver bytes preserved')
native = NATIVE.read_text()
native_inverse = re.sub(r'^const skillOwnerHTTPNativeSelector = .*\n\n', '', native, flags=re.M).replace(' && *selector != skillOwnerHTTPNativeSelector', '').replace('\t} else if *selector == skillOwnerHTTPNativeSelector {\n\t\tnativeGate = "AGENTEAM_SKILL_HTTP_NATIVE"\n\t}', '\t}')
check(native_inverse == baseline(NATIVE), 'entire old native driver bytes preserved')
check(namespace['budgets'](False) == (123, 3) and namespace['budgets'](True) == (540, 60), 'original budgets')
selectors = (namespace['SKILL_HTTP_PG'], namespace['SKILL_HTTP_NATIVE'])
for selector, source, name in zip(selectors, (pg, native), ('skillOwnerHTTPSelector', 'skillOwnerHTTPNativeSelector')):
    declared = re.search(r'const ' + name + r' = `([^`]+)`', source).group(1)
    check(declared == selector, 'same exact literal on both sides')
    for bad in (selector[1:], selector[:-1], selector + '|TestOther', selector.replace('HTTP', 'Http'), selector.replace('(', '(?:'), '^TestSkill.*$'):
        check(bad not in namespace['SKILL_HTTP_CASES'], 'no widened exact supervisor gate')
    check('105*time.Second' in source.replace(' ', '') and '-test.count=1' in source and 'cmd.Wait()' in source, 'unchanged driver timing/wait')


for selector in selectors:
    with patch('subprocess.Popen', return_value=object()) as popen:
        namespace['skill_http_spawn'](Path('/fixed/driver'), Path('/fixed/binary'), selector, Path('/fixed/runtime'), io.StringIO())
    args, options = popen.call_args
    check(args[0] == ['/fixed/driver', '--test-binary', '/fixed/binary', '--run', selector, '--directory', '/fixed/runtime'], 'actual spawn exact command')
    expected_env = dict(os.environ)
    if selector == selectors[0]: expected_env['AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON'] = str(Path(sys.executable).resolve())
    check(options['env'] == expected_env, 'actual spawn only explicit Schema interpreter change')


def manifest_and_log(directory, selector):
    directory.mkdir()
    cases = namespace['SKILL_HTTP_CASES'][selector]
    names = [name for top, subs in cases.items() for name in (top, *(top + '/' + sub for sub in subs))]
    text = ''.join('=== RUN   ' + x + '\n' for x in names)
    text += ''.join('--- PASS: ' + x + ' (0.01s)\n' for x in reversed(names))
    if selector == selectors[0]:
        record = {'nonce': 'a'*32, 'container_id': 'b'*64, 'network_id': 'c'*64}
        text += 'OWNED nonce=' + record['nonce'] + ' container=' + record['container_id'] + ' network=' + record['network_id'] + ' port=12345 PostgreSQL=170006 vector=0.8.1\n'
        text += 'CHILD pid=2345 selector=' + selector + '\nCHILD actual_wait pid=2345 state=exit status 0\n'
        for n in (1, 2):
            text += f'RETIRE observation={n} exact_container={record["container_id"]} exact_network={record["network_id"]} clean=true\n'
        text += 'DRIVER terminal exit=0 elapsed=1s child_started=true actual_child_wait=true cleanup=true\n'
    else:
        record = {'kind': 'work-http-native', 'child_pid': 2345}
        text += 'CHILD pid=2345 selector=' + selector + ' kind=native-http\nCHILD actual_wait pid=2345 state=exit status 0\n'
        text += 'NATIVE runtime_empty=true actual_child_wait=true\nDRIVER terminal exit=0 elapsed=1.000s child_started=true actual_child_wait=true private_removed=true\n'
    path = directory / 'owned.json'
    path.write_text(json.dumps(record)); path.chmod(0o600)
    return text, names


with tempfile.TemporaryDirectory(prefix='skills-entry-') as temporary:
    temp = Path(temporary)
    for index, selector in enumerate(selectors):
        directory = temp / ('observer-' + str(index))
        valid, names = manifest_and_log(directory, selector)
        log_path = temp / ('observer-' + str(index) + '.log')
        def observe(text):
            log_path.write_bytes(text if isinstance(text, bytes) else text.encode())
            return namespace['observe_skill_http'](directory, io.StringIO(), log_path, selector)
        check(observe(valid), 'positive complete case set/wait/private')
        for name in names:
            check(not observe(valid.replace('=== RUN   ' + name + '\n', '')), 'every missing original case rejected')
        for bad in (
            valid + '=== RUN   ' + names[0] + '\n',
            valid + '--- PASS: ' + names[0] + ' (1s)\n',
            valid + '=== RUN   TestOther\n',
            valid.replace('--- PASS:', '--- SKIP:', 1),
            valid.replace('actual_wait pid=2345', 'actual_wait pid=2346'),
            valid + 'CHILD actual_wait pid=2345 state=exit status 1\n',
            valid + 'CHILD pid=5555 selector=^TestOther$\n',
            valid.replace('DRIVER terminal exit=0', 'DRIVER terminal exit=1'),
            valid.encode() + b'\xff',
        ):
            check(not observe(bad), 'closed negative log')
        log_path.unlink()
        check(not namespace['observe_skill_http'](directory, io.StringIO(), log_path, selector), 'unreadable log safely fails')
        (directory / 'private-left').symlink_to(directory / 'absent')
        check(not observe(valid), 'dangling private residue rejected')
        (directory / 'private-left').unlink()
        if selector == selectors[0]:
            check(not observe(valid.replace('RETIRE observation=2', 'RETIRE observation=1')), 'two distinct retire observations')
            check(not observe(valid.replace('clean=true', 'clean=false', 1)), 'resource retirement false remains false')
        else:
            check(not observe(valid.replace('runtime_empty=true', 'runtime_empty=false')), 'native runtime must retire')
        (directory / 'owned.json').chmod(0o644)
        check(not observe(valid), 'private manifest mode')

    # The actual input function observes additions and deletions, not only the
    # hashes of a one-time list. Writes below are exclusively this temporary tree.
    root = temp / 'inputs'
    root.mkdir()
    fixed = ['go.mod','go.sum','.agent-state/task-planning-recovery/pg_only_supervisor.py', '.agent-state/task-planning-recovery/pg_only_driver.go', '.agent-state/work-owner-http/native_driver.go',
             'internal/central/skill/http/testdata/schema.py', 'api/openapi/skill-owner.json', 'api/openapi/common.json']
    fixed += ['internal/central/skill/http/' + x for x in ('handler.go', 'wire.go', 'io.go', 'native_test.go')]
    fixed += ['tests/skills/' + x for x in ('owner_http_fixture_test.go', 'owner_http_test.go', 'owner_http_transactions_test.go')]
    for name in fixed:
        p = root / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text(name)
    driver, binary = root / 'driver', root / 'binary'
    driver.write_text('driver'); binary.write_text('binary')
    old_file = namespace['__file__']
    namespace['__file__'] = str(root / '.agent-state/task-planning-recovery/pg_only_supervisor.py')
    try:
        first = namespace['skill_http_inputs'](driver, binary, selectors[0])
        for name in ('api/openapi/common.json','api/openapi/skill-owner.json','internal/central/skill/http/testdata/schema.py'):
            p = root/name; original = p.read_text(); p.write_text(original+'changed')
            check(namespace['skill_http_inputs'](driver,binary,selectors[0]) != first, 'runtime byte change detected')
            p.write_text(original)
        added = root/'tests/skills/new_test.go'; added.write_text('new')
        check(namespace['skill_http_inputs'](driver,binary,selectors[0]) != first, 'new package source detected')
        added.unlink()
        missing = root/'tests/skills/owner_http_test.go'; saved = missing.read_text(); missing.unlink()
        try:
            namespace['skill_http_inputs'](driver,binary,selectors[0])
            check(False, 'missing required source must fail')
        except FileNotFoundError:
            check(True, 'missing required source fails')
        missing.write_text(saved)
    finally:
        namespace['__file__'] = old_file

    # Exercise the actual main with explicit OS/child/TCP doubles. The only
    # launched process in this script is the read-only git-show above.
    original_inputs = namespace['skill_http_inputs']
    for mode in ('pass', 'native-pass', 'bad-log', 'bad-utf8', 'addition', 'removal', 'unreadable', 'driver-fail'):
        out = temp / ('main-' + mode)
        calls = {'input': 0, 'tcp': 0, 'desc': 0, 'wait': 0}
        class Child:
            pid, returncode = 4567, None
            def wait(self, timeout):
                check(timeout == 123, 'actual main original wait budget')
                calls['wait'] += 1
                self.returncode = 1 if mode == 'driver-fail' else 0
                return self.returncode
        def spawn(d, b, selector, directory, log):
            body, _ = manifest_and_log(directory, selector)
            if mode == 'bad-log': body = body.replace('=== RUN', 'IGNORED', 1)
            log.write(body)
            if mode == 'bad-utf8':
                log.flush()
                with open(log.name, 'ab') as invalid: invalid.write(b'\xff')
                log.seek(0, os.SEEK_END)
            return Child()
        def inputs(*args):
            calls['input'] += 1
            value = {'unchanged': 'bytes'}
            if calls['input'] == 2:
                if mode == 'addition': value['new'] = 'bytes'
                if mode == 'removal': value = {}
                if mode == 'unreadable': raise OSError('PRIVATE must not escape')
            return value
        def tcp(): calls['tcp'] += 1; return set()
        def descendants(pid): calls['desc'] += 1; return set()
        def waited(*args): raise ChildProcessError()
        replacements = {'skill_http_spawn': spawn, 'skill_http_inputs': inputs, 'tcp': tcp, 'descendants': descendants}
        originals = {k: namespace[k] for k in replacements}; namespace.update(replacements)
        try:
            with patch.object(sys, 'argv', ['supervisor', '--driver', str(driver), '--binary', str(binary), '--run', selectors[1] if mode == 'native-pass' else selectors[0], '--output', str(out)]), patch('ctypes.CDLL', return_value=types.SimpleNamespace(prctl=lambda *a: 0)), patch('signal.signal', return_value=None), patch('os.waitpid', side_effect=waited), patch('time.sleep', return_value=None), patch('sys.stdout', new=io.StringIO()):
                code = namespace['main']()
            check(code == (0 if mode in ('pass', 'native-pass') else 1), 'actual main code ' + mode)
            check(calls == {'input': 2, 'tcp': 3, 'desc': 3, 'wait': 1}, 'original full tails remain reachable ' + mode)
        finally:
            namespace.update(originals)

print(json.dumps({'checks': checks, 'exit': 0, 'scope': 'offline exact entry; actual main OS/child/TCP doubles; no Go or resources'}))
