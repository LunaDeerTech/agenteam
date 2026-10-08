#!/usr/bin/env python3
"""Import only the two stopped Model runner sources' standard-library imports."""
import argparse
import ctypes
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parent
PREFIX = Path('/opt/codex/runtimes/codex-primary-runtime/dependencies/python')
modules, builtin = {}, []
for name, module in list(sys.modules.items()):
    path = getattr(module, '__file__', None)
    if name == '__main__':
        continue
    if path is None:
        builtin.append(name)
        continue
    path = Path(path)
    assert path.is_relative_to(PREFIX), (name, str(path))
    assert 'site-packages' not in path.parts, (name, str(path))
    raw = path.read_bytes()
    modules[str(path)] = {'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}
executables = {}
for path in {Path(sys.executable), Path(sys.executable).resolve()}:
    with path.open('rb') as source:
        executables[str(path)] = {'sha256': hashlib.file_digest(source, 'sha256').hexdigest(),
                                  'bytes': path.stat().st_size}
value = {'state': 'ACTUAL_STANDARD_LIBRARY_IMPORT_FINGERPRINTS_ONLY',
         'python_version': sys.version, 'isolated': sys.flags.isolated,
         'dont_write_bytecode': sys.dont_write_bytecode,
         'modules': modules, 'module_count': len(modules), 'builtin_or_frozen_without_file': sorted(builtin),
         'executables': executables,
         'driver_imported_or_executed': False, 'subprocess_started': False,
         'scope': 'Literal standard-library import union of stopped driver/launcher under the same Python -I -B. No third-party, schema, Node, browser or provider SDK imported.'}
(ROOT / 'python-runtime.json').write_text(json.dumps(value, indent=2) + '\n')
print(json.dumps({key: value[key] for key in ['state', 'module_count', 'driver_imported_or_executed', 'subprocess_started']}))
