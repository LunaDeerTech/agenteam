"""Offline exact-source union for the three accepted, independently owned deltas.

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
           'skills_cleanup': '92cfb069'}
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
PATHS = {
    'skills_http': (SUP, '.agent-state/task-planning-recovery/pg_only_driver.go',
                    '.agent-state/work-owner-http/native_driver.go'),
    'd05': (SUP, DRIVER),
    'skills_cleanup': (SUP, DRIVER),
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


def combined(path, omit=None):
    base = source(BASE, path)
    current = base
    for domain, ref in SOURCES.items():
        if domain != omit and path in PATHS[domain]:
            current = apply_delta(base, source(ref, path), current)
    return current


def baseline_for(path, omit):
    path = str(Path(path).relative_to(ROOT)) if Path(path).is_absolute() else str(path)
    assert omit in SOURCES and path in PATHS[omit]
    assert (ROOT / path).read_text() == combined(path), 'unknown shared change: ' + path
    return combined(path, omit)


def check_actual_union():
    for path in sorted({path for paths in PATHS.values() for path in paths}):
        assert (ROOT / path).read_text() == combined(path), 'unknown shared change: ' + path
