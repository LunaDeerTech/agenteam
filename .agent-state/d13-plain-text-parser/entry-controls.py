#!/usr/bin/env python3
"""Offline controls for the exact D13 root-chain extension.

No Go test, socket, Docker command or real resource observer is started. The
resource-observation controls call the real supervisor function with explicitly
controlled ownership/absence functions. A pending shared-source binding cannot
produce a successful result.
"""
import importlib.util
import io
from pathlib import Path
import subprocess
import sys
import tempfile
import types

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUPERVISOR = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
BASE = '3a7a3fb5'
SELECTOR = '^TestKnowledgePlainTextParserIntegration$'
TOP = 'TestKnowledgePlainTextParserIntegration'
CASES = (TOP, *(TOP + '/' + name for name in (
    'full_current_bytes_after_actual_close',
    'partial_and_nonplain_rejected',
    'foreign_owner_produces_no_parser_input')))
BINARY = ROOT / 'output/ai/d13-plain-text-parser/parser-integration-race-01.test'

# Filled only after the sole shared writer freezes the reviewed exact hunks.
# Each tuple will be (old_bytes, new_bytes), independently scoped per file;
# reverse application must recover the complete fixed main baseline bytes.
SOURCE_HUNKS = None


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


def source_binding():
    if SOURCE_HUNKS is None:
        return False
    check(set(SOURCE_HUNKS) == {DRIVER, SUPERVISOR}, 'closed shared-source set')
    for relative, hunks in SOURCE_HUNKS.items():
        baseline = subprocess.check_output(['git', 'show', BASE + ':' + relative], cwd=ROOT)
        current = (ROOT / relative).read_bytes()
        check(reverse_source(current, hunks) == baseline, 'exact original source restoration')
        try:
            changed = reverse_source(current + b'\n# unknown delta\n', hunks)
        except ValueError:
            changed = None
        check(changed != baseline, 'unknown extra source must fail whole-byte comparison')
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
        input_controls(driver, supervisor, directory)
        observer_controls(supervisor, directory)
    if not source_binding():
        print('PENDING exact shared-source inverse binding; method results are not readiness')
        return 2
    print('PASS exact D13 entry/source/Wait/input and controlled resource-tail methods')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
