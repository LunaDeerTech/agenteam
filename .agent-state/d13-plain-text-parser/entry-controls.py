#!/usr/bin/env python3
"""Offline controls for the exact D13 root-chain extension.

No Go test, socket, Docker command or real resource observer is started. The
resource-observation controls call the real supervisor function with explicitly
controlled ownership/absence functions. A pending shared-source binding cannot
produce a successful result.
"""
import hashlib
import importlib.util
import io
from pathlib import Path
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUPERVISOR = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
BASE = '04455194'
SELECTOR = '^TestKnowledgePlainTextParserIntegration$'
TOP = 'TestKnowledgePlainTextParserIntegration'
CASES = (TOP, *(TOP + '/' + name for name in (
    'full_current_bytes_after_actual_close',
    'partial_and_nonplain_rejected',
    'foreign_owner_produces_no_parser_input')))
BINARY = ROOT / 'output/ai/d13-plain-text-parser/parser-integration-race-01.test'

# Frozen additions from the sole shared writer: two driver and seven
# supervisor hunks. No run-time diff or new source can extend this allowlist.
BASE_SHA = {'.agent-state/work-owner-http/root_chain_driver.py': 'fa8502149c5b782ac461dd1254af44b0ca862c8819f23cf10ec2d1e9db1c01b6',
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': '26e03db9fd5c37590846a9392d2c87e783971779aa1278ff6ea4649e9b567d2b'}
SOURCE_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': (('',
                                                        '    '
                                                        "'^TestKnowledgePlainTextParserIntegration$': "
                                                        "'tests/knowledge',\n"),
                                                       ('',
                                                        '    return sorted(paths)\n'
                                                        '\n'
                                                        '\n'
                                                        'def parser_inputs(binary):\n'
                                                        '    # The fixed integration binary uses '
                                                        'every same-package Knowledge helper.\n'
                                                        '    paths = set(input_paths(binary)) | '
                                                        'set((REPOSITORY / '
                                                        "'tests/knowledge').glob('*.go'))\n"
                                                        '    if any(not p.is_file() or '
                                                        'p.is_symlink() or p.resolve(strict=True) '
                                                        '!= p for p in paths):\n'
                                                        "        raise ValueError('regular "
                                                        "original parser inputs required')\n")),
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': (('',
                                                                '\n'
                                                                '\n'
                                                                'PARSER_PG = '
                                                                "'^TestKnowledgePlainTextParserIntegration$'\n"
                                                                'PARSER_CASES = frozenset({\n'
                                                                '    '
                                                                "'TestKnowledgePlainTextParserIntegration',\n"
                                                                '    '
                                                                "'TestKnowledgePlainTextParserIntegration/full_current_bytes_after_actual_close',\n"
                                                                '    '
                                                                "'TestKnowledgePlainTextParserIntegration/partial_and_nonplain_rejected',\n"
                                                                '    '
                                                                "'TestKnowledgePlainTextParserIntegration/foreign_owner_produces_no_parser_input',\n"
                                                                '})\n'
                                                                '\n'
                                                                '\n'
                                                                'def parser_results(output):\n'
                                                                "    runs = re.findall(r'^=== "
                                                                "RUN   (\\S+)$', output, re.M)\n"
                                                                "    results = re.findall(r'^[ "
                                                                '\\t]*--- (PASS|FAIL|SKIP): (\\S+) '
                                                                "\\([^()\\r\\n]*\\)$', output, "
                                                                're.M)\n'
                                                                "    waits = re.findall(r'^D03 "
                                                                'explicit test actual_wait '
                                                                'pid=([1-9][0-9]*) code=(-?[0-9]+) '
                                                                "selector=(\\S+)$', output, re.M)\n"
                                                                '    return (len(runs) == '
                                                                'len(PARSER_CASES) and set(runs) '
                                                                '== PARSER_CASES\n'
                                                                '            and len(results) == '
                                                                'len(PARSER_CASES)\n'
                                                                '            and all(state == '
                                                                "'PASS' for state, _ in results)\n"
                                                                '            and {name for _, name '
                                                                'in results} == PARSER_CASES\n'
                                                                '            and len(waits) == 1 '
                                                                "and waits[0][1:] == ('0', "
                                                                'PARSER_PG)\n'
                                                                '            and '
                                                                "re.search(r'^FAIL(?:\\s|$)', "
                                                                'output, re.M) is None)\n'
                                                                '\n'
                                                                '\n'
                                                                'def parser_same(inputs, args, '
                                                                'adapter):\n'
                                                                '    try:\n'
                                                                '        paths = '
                                                                'set(adapter.parser_inputs(args.binary))\n'
                                                                '        return (set(inputs) == '
                                                                '{str(p) for p in paths}\n'
                                                                '                and '
                                                                'all(p.is_file() and not '
                                                                'p.is_symlink()\n'
                                                                '                        and '
                                                                'p.resolve(strict=True) == p\n'
                                                                '                        and '
                                                                'adapter.sha(p) == inputs[str(p)] '
                                                                'for p in paths))\n'
                                                                '    except (OSError, ValueError, '
                                                                'TypeError):\n'
                                                                '        return False\n'),
                                                               ('    if selector in '
                                                                "('^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$',\n",
                                                                '    if selector in (PARSER_PG, '
                                                                "'^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$',\n"),
                                                               ('',
                                                                '        PARSER_PG: '
                                                                "{'TestKnowledgePlainTextParserIntegration'},\n"),
                                                               ('',
                                                                '    if selector == PARSER_PG:\n'
                                                                '        complete = '
                                                                'parser_results(output)\n'
                                                                "        log.write(f'ROOT "
                                                                "parser_exact_run_pass_wait={complete}\\n')\n"
                                                                '        good = good and '
                                                                'complete\n'),
                                                               ('',
                                                                "    if 'KnowledgePlainTextParser' "
                                                                'in args.run and (args.run != '
                                                                'PARSER_PG or not '
                                                                'args.root_chain):\n'
                                                                "        parser.error('plain text "
                                                                'parser requires its exact '
                                                                "original root-chain entry')\n"),
                                                               ('',
                                                                '    if args.run == PARSER_PG:\n'
                                                                '        inputs = {str(p): '
                                                                'adapter.sha(p) for p in '
                                                                'adapter.parser_inputs(args.binary)}\n'),
                                                               ('',
                                                                '            if args.run == '
                                                                'PARSER_PG:\n'
                                                                '                same = same and '
                                                                'parser_same(inputs, args, '
                                                                'adapter)\n'))}


def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def check(value, label):
    if not value:
        raise AssertionError(label)


def original_log():
    return '\n'.join(
        [f'=== RUN   {name}' for name in CASES]
        + [f'--- PASS: {name} (0.01s)' for name in reversed(CASES)]
        + [f'D03 explicit test actual_wait pid=123 code=0 selector={SELECTOR}', ''])


def reverse_source(current, hunks):
    for old, new in reversed(hunks):
        if not new or current.count(new) != 1:
            raise ValueError('unknown or ambiguous shared-source hunk')
        current = current.replace(new, old, 1)
    return current


def inverse(name, source: str) -> str:
    """Remove only the frozen D13 additions; reject any other source change."""
    if name not in SOURCE_HUNKS or not isinstance(source, str):
        raise ValueError('unknown shared source')
    restored = reverse_source(source, SOURCE_HUNKS[name])
    if hashlib.sha256(restored.encode()).hexdigest() != BASE_SHA[name]:
        raise ValueError('unrecognized main baseline change')
    return restored


def reject_inverse(name, source):
    try:
        inverse(name, source)
    except ValueError:
        return
    raise AssertionError('unrecognized or weakened source accepted')


def source_binding():
    check(set(SOURCE_HUNKS) == {DRIVER, SUPERVISOR}, 'closed shared-source set')
    check(tuple(map(len, SOURCE_HUNKS.values())) == (2, 7), 'fixed two/seven hunk scope')
    for relative, hunks in SOURCE_HUNKS.items():
        baseline = subprocess.check_output(['git', 'show', BASE + ':' + relative], cwd=ROOT)
        current = (ROOT / relative).read_text()
        check(inverse(relative, current).encode() == baseline, 'exact original source restoration')
        reject_inverse(relative, current + '\n# unknown delta\n')
        for _, added in hunks:
            reject_inverse(relative, current.replace(added, '', 1))
            reject_inverse(relative, current + added)
    reject_inverse('unknown.py', (ROOT / DRIVER).read_text())
    source = (ROOT / SUPERVISOR).read_text()
    for old, new in (
            ("PARSER_PG: {'TestKnowledgePlainTextParserIntegration'}", 'PARSER_PG: set()'),
            ("('0', PARSER_PG)", "('1', PARSER_PG)"),
            ("('0', SECRET_ROOT)", "('1', SECRET_ROOT)"),
            ("KNOWLEDGE_UI: {'TestKnowledgeOwnerReadWeb'}", 'KNOWLEDGE_UI: set()'),
            ("('0', KNOWLEDGE_UI)", "('1', KNOWLEDGE_UI)"),
            ('(540, 60) if root_chain', '(541, 60) if root_chain'),
            ('same = same and parser_same(inputs, args, adapter)', 'same = True'),
            ('same = same and root_composition_same(inputs, args, adapter)', 'same = True'),
            ('same = same and knowledge_ui_same(inputs, args, adapter)', 'same = True')):
        check(source.count(old) == 1, 'known original gate for mutation control')
        reject_inverse(SUPERVISOR, source.replace(old, new, 1))
    driver_source = (ROOT / DRIVER).read_text()
    original_target = "'^TestProjectSecretVariablesDefaultRoot$': 'internal/central/app'"
    check(driver_source.count(original_target) == 1, 'original cross-domain target')
    reject_inverse(DRIVER, driver_source.replace(original_target, original_target.replace('internal/central/app', 'tests/knowledge'), 1))
    return True


def result_controls(supervisor):
    good = original_log()
    check(supervisor.PARSER_PG == SELECTOR, 'exact selector constant')
    check(set(supervisor.PARSER_CASES) == set(CASES), 'closed four-node set')
    check(supervisor.parser_results(good), 'complete RUN/PASS/Wait positive')
    for name in CASES:
        run = f'=== RUN   {name}\n'
        passed = f'--- PASS: {name} (0.01s)\n'
        for bad in (good.replace(run, ''), good.replace(passed, ''), good + run,
                    good + passed, good.replace(passed, passed.replace('PASS', 'FAIL')),
                    good.replace(passed, passed.replace('PASS', 'SKIP'))):
            check(not supervisor.parser_results(bad), 'missing/duplicate/failing/skipped node')
    wait = f'D03 explicit test actual_wait pid=123 code=0 selector={SELECTOR}\n'
    for bad in (good.replace(wait, ''), good + wait, good.replace('code=0', 'code=1'),
                good.replace('code=0', 'code=-1'), good.replace('pid=123', 'pid=0'),
                good.replace(wait, wait.replace(SELECTOR, '^Other$')),
                good + '=== RUN   TestExtra\n--- PASS: TestExtra (0.01s)\n',
                good + 'FAIL\n'):
        check(not supervisor.parser_results(bad), 'original Wait/extra node/final failure')


def configuration_controls(driver, supervisor, directory):
    plan = driver.configuration(BINARY, SELECTOR, directory / 'fresh-root')
    check(plan['cwd'] == str(ROOT / 'tests/knowledge'), 'actual test package')
    check(plan['test_timeout'] == '6m' and plan['resources'] == 7, 'original Go/resource budget')
    check(supervisor.budgets(True) == (540, 60), 'original root supervisor budget')
    for selector in (SELECTOR[1:], SELECTOR[:-1], SELECTOR + '/.*',
                     '^TestKnowledgePlainTextParser.*$', '^TestKnowledgePlainTextParserIntegration$/foreign_owner_produces_no_parser_input'):
        try:
            driver.configuration(BINARY, selector, directory / 'fresh-root')
        except (ValueError, OSError):
            continue
        raise AssertionError('non-exact selector accepted')
    paths = set(driver.parser_inputs(BINARY))
    check(paths == set(driver.input_paths(BINARY)) | set((ROOT / 'tests/knowledge').glob('*.go')), 'exact additional source closure')
    for relative in ('tests/knowledge/plain_text_parser_integration_test.go',
                     'internal/central/retrieval/parser/types.go',
                     'internal/central/retrieval/parser/plain_text.go',
                     'internal/central/retrieval/parser/bounded_content.go'):
        check(ROOT / relative in paths, 'actual D12-to-D13 source present')


def main_guard_controls(supervisor, directory):
    # Call the real CLI parsing/guard path. Sentinels prove it stops before
    # adapter setup, filesystem creation, process setup or TCP observation.
    for selector, root_chain in (
            (SELECTOR, False), (SELECTOR + 'x', True),
            (SELECTOR[1:], True), (SELECTOR[:-1], True),
            ('^TestKnowledgePlainTextParser.*$', True),
            (SELECTOR + '/foreign_owner_produces_no_parser_input', True)):
        output = directory / 'guard-must-not-create'
        argv = ['supervisor', '--driver', str(ROOT / DRIVER), '--binary', str(BINARY),
                '--output', str(output), '--run', selector]
        if root_chain:
            argv.append('--root-chain')
        with patch.object(sys, 'argv', argv), patch('sys.stderr', io.StringIO()), \
                patch.object(supervisor, 'root_adapter', side_effect=AssertionError('adapter setup reached')), \
                patch.object(supervisor, 'tcp', side_effect=AssertionError('TCP observation reached')), \
                patch.object(supervisor.ctypes, 'CDLL', side_effect=AssertionError('process setup reached')), \
                patch.object(Path, 'mkdir', side_effect=AssertionError('directory creation reached')):
            try:
                supervisor.main()
            except SystemExit as stopped:
                check(stopped.code == 2, 'real main rejects invalid mode/selector')
            else:
                raise AssertionError('main returned without rejecting selector')
        check(not output.exists(), 'rejected CLI creates no output directory')


def input_controls(driver, supervisor, directory):
    first, second = directory / 'input-a', directory / 'input-b'
    first.write_bytes(b'original')
    second.write_bytes(b'second')
    selected = [first, second]
    adapter = types.SimpleNamespace(parser_inputs=lambda _: list(selected), sha=driver.sha)
    args = types.SimpleNamespace(binary=BINARY)
    inputs = {str(path): driver.sha(path) for path in selected}
    check(supervisor.parser_same(inputs, args, adapter), 'same original inputs')
    first.write_bytes(b'changed')
    check(not supervisor.parser_same(inputs, args, adapter), 'changed input')
    first.write_bytes(b'original')
    selected.pop()
    check(not supervisor.parser_same(inputs, args, adapter), 'removed input path')
    selected.append(second)
    extra = directory / 'input-extra'
    extra.write_bytes(b'new')
    selected.append(extra)
    check(not supervisor.parser_same(inputs, args, adapter), 'new input path')
    selected.pop()
    second.unlink()
    check(not supervisor.parser_same(inputs, args, adapter), 'missing original input')
    second.symlink_to(first)
    same_bytes = dict(inputs)
    same_bytes[str(second)] = driver.sha(first)
    check(not supervisor.parser_same(same_bytes, args, adapter), 'symlink substitution even with matching bytes')


def observer_controls(supervisor, directory):
    owned = directory / 'controlled-observer'
    owned.mkdir()
    (owned / 'runtime').mkdir()
    path = directory / 'controlled-go.log'
    record = {'resources': [dict(kind='container' if n < 4 else 'network', id=str(n), nonce='controlled') for n in range(7)],
              'directories': [str(directory / ('absent-private-' + str(n))) for n in range(3)]}
    old_record, old_absent = supervisor.root_record, supervisor.exact_absent
    seen = []
    supervisor.root_record = lambda _: record
    supervisor.exact_absent = lambda item, _: seen.append(item['id']) is None
    try:
        path.write_text(original_log())
        check(supervisor.observe_root_chain(owned, io.StringIO(), path, SELECTOR), 'real observer controlled-positive')
        check(len(seen) == 14, 'both original seven-resource observations')
        seen.clear()
        path.write_text(original_log().replace('code=0', 'code=1'))
        check(not supervisor.observe_root_chain(owned, io.StringIO(), path, SELECTOR), 'real observer rejects Wait1')
        check(len(seen) == 14, 'failed result still observes both resource tails')
        path.write_text(original_log())
        supervisor.exact_absent = lambda item, _: item['id'] != '0'
        check(not supervisor.observe_root_chain(owned, io.StringIO(), path, SELECTOR), 'resource residue rejects')
        supervisor.exact_absent = lambda *_: True
        (owned / 'runtime' / 'residue').write_text('controlled')
        check(not supervisor.observe_root_chain(owned, io.StringIO(), path, SELECTOR), 'runtime residue rejects')
    finally:
        supervisor.root_record, supervisor.exact_absent = old_record, old_absent


def main():
    driver, supervisor = load('d13_driver', DRIVER), load('d13_supervisor', SUPERVISOR)
    if not all(hasattr(supervisor, name) for name in ('PARSER_PG', 'PARSER_CASES', 'parser_results', 'parser_same')) or not hasattr(driver, 'parser_inputs'):
        print('PENDING shared D13 entry not yet installed; no readiness claim')
        return 2
    out = ROOT / 'output/ai/d13-plain-text-parser'
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='entry-controls-', dir=out) as temporary:
        directory = Path(temporary)
        result_controls(supervisor)
        configuration_controls(driver, supervisor, directory)
        main_guard_controls(supervisor, directory)
        input_controls(driver, supervisor, directory)
        observer_controls(supervisor, directory)
    if not source_binding():
        print('PENDING exact shared-source inverse binding; method results are not readiness')
        return 2
    print('PASS exact D13 entry/source/Wait/input and controlled resource-tail methods')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
