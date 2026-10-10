"""Offline exact-source union for four accepted, independently owned deltas.

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
           'skills_cleanup': '92cfb069', 'knowledge_content': '9b9d1e7c'}
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
PATHS = {
    'skills_http': (SUP, '.agent-state/task-planning-recovery/pg_only_driver.go',
                    '.agent-state/work-owner-http/native_driver.go'),
    'd05': (SUP, DRIVER),
    'skills_cleanup': (SUP, DRIVER),
    'knowledge_content': (SUP, DRIVER, '.agent-state/work-owner-http/native_driver.go'),
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


def combined(path, omit=None):
    base = source(BASE, path)
    current = base
    for domain, ref in SOURCES.items():
        if domain != omit and path in PATHS[domain]:
            changed = source(ref, path)
            current = (apply_content_delta(path, base, changed, current)
                       if domain == 'knowledge_content' else apply_delta(base, changed, current))
    return current


def baseline_for(path, omit):
    path = str(Path(path).relative_to(ROOT)) if Path(path).is_absolute() else str(path)
    assert omit in SOURCES and path in PATHS[omit]
    assert (ROOT / path).read_text() == combined(path), 'unknown shared change: ' + path
    return combined(path, omit)


def check_actual_union():
    for path in sorted({path for paths in PATHS.values() for path in paths}):
        assert (ROOT / path).read_text() == combined(path), 'unknown shared change: ' + path
