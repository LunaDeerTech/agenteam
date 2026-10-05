#!/usr/bin/env python3
"""Check archived whitespace with exact raw-file exceptions; do not invoke Git."""
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def check():
    entries = json.loads((ROOT / 'whitespace-exceptions.json').read_text())['exceptions']
    exceptions = {entry['path']: entry for entry in entries}
    patches = set(json.loads((ROOT / 'raw-patch-paths.json').read_text()))
    assert len(patches) == 15 and len(exceptions) == 16
    assert patches == {name for name, entry in exceptions.items() if entry['kind'] == 'raw_patch'}
    for name, entry in exceptions.items():
        assert hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == entry['sha256']
    warnings = []
    checked = 0
    for path in sorted(ROOT.rglob('*')):
        if not path.is_file() or str(path.relative_to(ROOT)) in patches:
            continue
        name = str(path.relative_to(ROOT))
        lines = path.read_bytes().splitlines()
        checked += 1
        for number, line in enumerate(lines, 1):
            if line.endswith((b' ', b'\t')):
                warnings.append({'path': name, 'line': number, 'rule': 'trailing whitespace'})
            prefix = re.match(rb'[ \t]*', line).group()
            if b' \t' in prefix:
                warnings.append({'path': name, 'line': number, 'rule': 'space before tab in indent'})
        if lines and not lines[-1]:
            warnings.append({'path': name, 'line': len(lines), 'rule': 'blank line at EOF'})
    unexpected = [entry for entry in warnings if entry['path'] not in exceptions]
    assert not unexpected, unexpected
    raw = json.loads((ROOT / 'source-map.json').read_text())['raw_files']
    for name, entry in raw.items():
        assert hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == entry['sha256']
    return {'result': 'PASS with exact raw-file whitespace exceptions',
            'checked_non_patch_files': checked, 'exact_exceptions': len(exceptions),
            'preserved_raw_originals': len(raw), 'warnings_outside_raw_patches': warnings,
            'unexpected_warnings': unexpected,
            'scope': 'Python trailing whitespace, space-before-tab indent, blank-at-EOF and SHA checks; not a Git index check'}


if __name__ == '__main__':
    print(json.dumps(check(), ensure_ascii=False, indent=2))
