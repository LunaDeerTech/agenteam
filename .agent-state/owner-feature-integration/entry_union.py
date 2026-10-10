"""Offline exact-source union for six accepted, independently owned deltas.

Every baseline request first compares the real candidate with the complete
union. Unknown edits or overlapping replacement hunks fail; no gate is mocked.
Git is only read through show. This module never runs a driver or Go command.
"""
from difflib import SequenceMatcher
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = '280a6431'
SOURCES = {'skills_http': '1e260d55', 'd05': '52a42627',
           'skills_cleanup': '92cfb069', 'knowledge_content': '9b9d1e7c',
           'secret_storage': 'b724e397', 'secret_owner': 'f9cc11c6',
           'root_composition': None}
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
PATHS = {
    'skills_http': (SUP, '.agent-state/task-planning-recovery/pg_only_driver.go',
                    '.agent-state/work-owner-http/native_driver.go'),
    'd05': (SUP, DRIVER),
    'skills_cleanup': (SUP, DRIVER),
    'knowledge_content': (SUP, DRIVER, '.agent-state/work-owner-http/native_driver.go'),
    'secret_storage': (SUP, '.agent-state/task-planning-recovery/pg_only_driver.go'),
    'secret_owner': (SUP, '.agent-state/task-planning-recovery/pg_only_driver.go'),
    'root_composition': (SUP, DRIVER),
}


def source(ref, path):
    return subprocess.check_output(['git', 'show', ref + ':' + path],
                                   cwd=ROOT, text=True)


def apply_delta(base, changed, current):
    """Apply only changed lines, with unique unchanged anchors for insertions."""
    before, after = base.splitlines(keepends=True), changed.splitlines(keepends=True)
    edits = SequenceMatcher(None, before, after, autojunk=False).get_opcodes()
    for kind, start, end, other_start, other_end in reversed(edits):
        if kind == 'equal':
            continue
        old, new = ''.join(before[start:end]), ''.join(after[other_start:other_end])
        if old:
            candidates = [(old, new)]
            for size in range(8, 0, -1):
                prefix = ''.join(before[max(0, start - size):start])
                suffix = ''.join(before[end:end + size])
                candidates += [(prefix + old, prefix + new),
                               (old + suffix, new + suffix)]
            for match, replacement in candidates:
                if current.count(match) == 1:
                    current = current.replace(match, replacement, 1)
                    break
            else:
                raise AssertionError(('nonunique or overlapping replacement', old))
            continue
        anchors = []
        for size in range(8, 0, -1):
            next_anchor = ('after', ''.join(before[start:start + size]))
            prior_anchor = ('before', ''.join(before[max(0, start - size):start]))
            # Whole functions precede their next declaration. Statements stay
            # with the prior statement in their original function/block.
            anchors += ([next_anchor, prior_anchor] if new.lstrip().startswith('def ')
                        else [prior_anchor, next_anchor])
        for side, anchor in anchors:
            if anchor.strip() and current.count(anchor) == 1:
                current = current.replace(anchor, new + anchor if side == 'after'
                                          else anchor + new, 1)
                break
        else:
            raise AssertionError('no unique insertion anchor')
    return current


def apply_content_delta(path, base, changed, current):
    """Resolve only the known shared input/gate insertion points explicitly."""
    if path == DRIVER:
        return apply_delta(base, changed, current)
    if path.endswith('native_driver.go'):
        old_line = next(line for line in base.splitlines(True) if line.startswith('\tif opts.Parse('))
        new_line = next(line for line in changed.splitlines(True) if line.startswith('\tif opts.Parse('))
        gate = ' else if contentNative(*selector) {\n\t\tnativeGate = "AGENTEAM_KNOWLEDGE_CONTENT_HTTP_NATIVE"\n\t}'
        assert changed.count(gate) == 1
        remainder = changed.replace(new_line, old_line).replace(gate, '')
        current = apply_delta(base, remainder, current)
        actual_line = next(line for line in current.splitlines(True) if line.startswith('\tif opts.Parse('))
        skill_line = next(line for line in source(SOURCES['skills_http'], path).splitlines(True)
                          if line.startswith('\tif opts.Parse('))
        assert actual_line in (old_line, skill_line)
        current = current.replace(actual_line, actual_line.replace(
            ' && !variableNative(*selector)',
            ' && !variableNative(*selector) && !contentNative(*selector)'))
        start, end = current.index('\tnativeGate := '), current.index('\t// Copy inherited')
        original_gate = current[start:end]
        known_gates = []
        for text in (base, source(SOURCES['skills_http'], path)):
            known_gates.append(text[text.index('\tnativeGate := '):text.index('\t// Copy inherited')])
        assert original_gate in known_gates and original_gate.endswith('\t}\n')
        combined_gate = original_gate[:-3] + '\t}' + gate + '\n'
        return current[:start] + combined_gate + current[end:]
    assert path == SUP
    inputs = ('    if args.run in CONTENT_GROUPS:\n'
              '        inputs.update({str(p.resolve()): hashlib.sha256(p.read_bytes()).hexdigest() for p in content_inputs(args.run)})\n')
    old_same = ('            same = all((adapter.sha(p) if adapter is not None else hashlib.sha256(Path(p).read_bytes()).hexdigest()) == digest\n'
                '                       for p, digest in inputs.items())\n')
    new_same = ('            same = (content_same(inputs, args, adapter) if args.run in CONTENT_GROUPS else\n'
                '                    all((adapter.sha(p) if adapter is not None else hashlib.sha256(Path(p).read_bytes()).hexdigest()) == digest\n'
                '                        for p, digest in inputs.items()))\n')
    mapping = '        CONTENT_PG: set(CONTENT_GROUPS[CONTENT_PG]),\n'
    assert changed.count(inputs) == changed.count(new_same) == changed.count(mapping) == 1
    remainder = changed.replace(inputs, '').replace(new_same, old_same).replace(mapping, '')
    current = apply_delta(base, remainder, current)
    expected = current.index('    expected = {\n', current.index('def observe_root_chain('))
    at = expected + len('    expected = {\n')
    current = current[:at] + mapping + current[at:]
    anchor = ('    skill_selected = not args.root_chain and args.run in SKILL_HTTP_CASES\n'
              if '    skill_selected = ' in current else '    baseline = tcp()\n')
    assert current.count(anchor) == 1
    current = current.replace(anchor, inputs + anchor)
    # Preserve the already selected Skill input check. The original content
    # conditional belongs inside its else, with identical behavior otherwise.
    for indent in ('', '    '):
        prior = ''.join(indent + line for line in old_same.splitlines(True))
        if current.count(prior) == 1:
            replacement = ''.join(indent + line for line in new_same.splitlines(True))
            return current.replace(prior, replacement, 1)
    raise AssertionError('unrecognized shared input-consistency branch')


def apply_secret_delta(path, base, changed, current, domain):
    """Parallel domain deltas share only stat, selector line and same-input tail."""
    if path.endswith('pg_only_driver.go'):
        old = next(line for line in base.splitlines(True) if line.startswith('\tif opts.Parse('))
        new = next(line for line in changed.splitlines(True) if line.startswith('\tif opts.Parse('))
        assert new.startswith(old[:-5]) and old.endswith(') {\n')
        predicates = new[len(old) - 5:-5]
        current = apply_delta(base, changed.replace(new, old), current)
        actual = next(line for line in current.splitlines(True) if line.startswith('\tif opts.Parse('))
        assert actual.endswith(') {\n') and predicates not in actual
        return current.replace(actual, actual[:-5] + predicates + actual[-5:], 1)
    assert path == SUP
    flag = 'secret_storage' if domain == 'secret_storage' else 'secret_owner'
    original_same = base[base.index('            same = all('):base.index('            if not same:')]
    begin = changed.index('            if ' + flag + ':', changed.index('tail_deadline ='))
    end = changed.index('            if not same:', begin)
    branch = changed[begin:end]
    marker = '            else:\n'
    prefix, fallback = branch.split(marker, 1)
    assert fallback == ''.join('    ' + line for line in original_same.splitlines(True))
    inputs_start = changed.index('    if ' + flag + ':', changed.index('    inputs ='))
    inputs_end = changed.index('    baseline = tcp()', inputs_start)
    inputs = changed[inputs_start:inputs_end]
    observer = ('            if ' + flag + ' and not observe_' + flag + '(log_path, log, args.run):\n'
                '                code = 1\n')
    assert changed.count(observer) == 1
    remainder = changed.replace(branch, original_same).replace(inputs, '').replace(observer, '')
    if 'import stat\n' in current:
        remainder = remainder.replace('import stat\n', '')
    current = apply_delta(base, remainder, current)
    anchor = '    baseline = tcp()\n'
    assert current.count(anchor) == 1
    current = current.replace(anchor, inputs + anchor, 1)
    observer_anchor = '            # The tail is a host delta, not an assertion that every short\n'
    assert current.count(observer_anchor) == 1
    current = current.replace(observer_anchor, observer + observer_anchor, 1)
    # Existing domains keep their exact branches as the new domain's fallback.
    tail_end = current.index('            if not same:', current.index('tail_deadline ='))
    candidates = [current.find(text, current.index('tail_deadline ='), tail_end) for text in
                  ('            if secret_storage:', '            if skill_selected:', '            same = (', '            same = all(')]
    start = min(index for index in candidates if index >= 0)
    end = current.index('            if not same:', start)
    tail = current[start:end]
    return current[:start] + prefix + marker + ''.join('    ' + line for line in tail.splitlines(True)) + current[end:]


def apply_root_composition(path, current):
    """One reviewed local delta; inverse controls strip these exact additions."""
    selector = '^TestKnowledgeSkillsDefaultRootComposition$'
    def insert(anchor, addition, after=False):
        nonlocal current
        assert current.count(anchor) == 1, ('root composition anchor', anchor)
        current = current.replace(anchor, anchor + addition if after else addition + anchor, 1)
    if path == DRIVER:
        insert('TARGETS = {\n', "    '" + selector + "': 'internal/central/app',\n", after=True)
        insert('def metadata_cost_inputs():' if 'def metadata_cost_inputs():' in current else 'def configuration(', '''def root_composition_inputs():
    # Freeze the precompiled app package's same-package fixtures as provenance.
    return sorted((REPOSITORY / 'internal/central/app').glob('*.go'))


''')
        return current
    assert path == SUP
    insert('def budgets(root_chain):', '''def root_composition_results(output):
    selector = '^TestKnowledgeSkillsDefaultRootComposition$'
    wanted = 'TestKnowledgeSkillsDefaultRootComposition'
    runs = re.findall(r'^=== RUN   (\\S+)$', output, re.M)
    results = re.findall(r'^[ \\t]*--- (PASS|FAIL|SKIP): (\\S+) \\([^()\\r\\n]*\\)$', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector='
                       + re.escape(selector) + r'$', output, re.M)
    return (runs == [wanted] and results == [('PASS', wanted)]
            and len(waits) == 1 and waits[0][1] == '0'
            and re.search(r'^FAIL(?:\\s|$)', output, re.M) is None)


def root_composition_same(inputs, args, adapter):
    try:
        paths = set(adapter.input_paths(args.binary)) | set(adapter.root_composition_inputs())
        return (set(inputs) == {str(p) for p in paths}
                and all(p.is_file() and not p.is_symlink() and adapter.sha(p) == inputs[str(p)] for p in paths))
    except (OSError, ValueError, TypeError):
        return False


''')
    expected = current.index('    expected = {\n', current.index('def observe_root_chain(')) + len('    expected = {\n')
    current = current[:expected] + "        '" + selector + "': {'TestKnowledgeSkillsDefaultRootComposition'},\n" + current[expected:]
    insert("    log.write(f'ROOT exact_tops={actual == expected} actual_test_wait={waited}\\n')\n", '''    if selector == '^TestKnowledgeSkillsDefaultRootComposition$':
        complete = root_composition_results(output)
        log.write(f'ROOT composition_exact_run_pass_wait={complete}\\n')
        good = good and complete
''', after=True)
    insert('    args = parser.parse_args()\n', '''    if args.run == '^TestKnowledgeSkillsDefaultRootComposition$' and not args.root_chain:
        parser.error('default root composition requires the original root chain')
''', after=True)
    insert('    baseline = tcp()\n', '''    if args.run == '^TestKnowledgeSkillsDefaultRootComposition$':
        inputs.update({str(p): adapter.sha(p) for p in adapter.root_composition_inputs()})
''')
    insert('            if not same: code = 1\n', '''            if args.run == '^TestKnowledgeSkillsDefaultRootComposition$':
                same = same and root_composition_same(inputs, args, adapter)
''')
    return current


def combined(path, omit=None, overrides=None):
    base = source(BASE, path)
    current = base
    for domain, ref in SOURCES.items():
        if domain != omit and path in PATHS[domain]:
            if domain == 'root_composition':
                current = apply_root_composition(path, current)
                continue
            changed = source((overrides or {}).get(domain, ref), path)
            current = (apply_content_delta(path, base, changed, current)
                       if domain == 'knowledge_content' else
                       apply_secret_delta(path, base, changed, current, domain)
                       if domain in ('secret_storage', 'secret_owner') else apply_delta(base, changed, current))
    return current


def baseline_for(path, omit, replacement=None):
    path = str(Path(path).relative_to(ROOT)) if Path(path).is_absolute() else str(path)
    assert omit in SOURCES and path in PATHS[omit]
    assert (ROOT / path).read_text() == combined(path), 'unknown shared change: ' + path
    return combined(path, omit) if replacement is None else combined(path, overrides={omit: replacement})


def check_actual_union():
    for path in sorted({path for paths in PATHS.values() for path in paths}):
        assert (ROOT / path).read_text() == combined(path), 'unknown shared change: ' + path
