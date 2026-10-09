"""Inputs for the single Runner process-crash top's real command build.

No fixture, cleanup, command build or network operation lives in this module.
The fixed Go metadata process is actually waited within the supervisor's
existing pre-tail budget; terminal validation starts no process.
"""
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
MODULE = 'github.com/LunaDeerTech/agenteam'
COMMAND = MODULE + '/cmd/agenteam-runner'
SELECTOR = '^TestRunnerControlProcessCrashRecovery$'
OTHER_SOURCES = ('CgoFiles', 'CFiles', 'CXXFiles', 'MFiles', 'HFiles', 'FFiles',
                 'SFiles', 'SwigFiles', 'SwigCXXFiles', 'SysoFiles')


def regular(path):
    path = Path(path)
    if not path.is_absolute() or path.resolve(strict=True) != path or not stat.S_ISREG(path.stat().st_mode):
        raise ValueError('non-regular crash build input')
    return path


def digest(path):
    with regular(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def source_members(directory, recursive):
    # Top-level source additions can change a Go package without touching any
    # prior file. For go:embed, additions anywhere in its package may match an
    # original pattern, so record names under that package as well.
    if directory.resolve(strict=True) != directory or not directory.is_dir():
        raise ValueError('non-directory crash build input')
    entries = directory.rglob('*') if recursive else directory.iterdir()
    names = []
    for path in entries:
        if recursive or (not path.name.startswith(('.', '_')) and not path.name.endswith('_test.go')
                         and path.suffix in {'.go', '.c', '.cc', '.cpp', '.cxx', '.h', '.m', '.s', '.S', '.syso', '.swig', '.swigcxx', '.f', '.F'}):
            if path.is_symlink():
                raise ValueError('symlink in crash build input directory')
            names.append((str(path.relative_to(directory)), path.is_dir()))
    return tuple(sorted(names))


def decode_packages(raw):
    decoder = json.JSONDecoder()
    packages = []
    while raw.strip():
        raw = raw.lstrip()
        value, end = decoder.raw_decode(raw)
        if not isinstance(value, dict) or value.get('Error') or value.get('DepsErrors'):
            raise ValueError('incomplete Go dependency metadata')
        packages.append(value)
        raw = raw[end:]
    return packages


def project(packages, driver, binary, root=ROOT):
    local = {}
    for package in packages:
        module = package.get('Module') or {}
        if module.get('Replace'):
            raise ValueError('replacement modules require explicit build-input review')
        if not module.get('Main'):
            continue  # Fixed toolchain and readonly, checksum-pinned module cache.
        name = package.get('ImportPath')
        if (module.get('Path') != MODULE or module.get('Dir') != str(root)
                or module.get('GoMod') != str(root / 'go.mod') or name in local
                or not isinstance(name, str) or not name.startswith(MODULE + '/')):
            raise ValueError('foreign or duplicate local build package')
        local[name] = package
    if COMMAND not in local:
        raise ValueError('original Runner command missing from dependency closure')
    paths = {Path(driver), Path(binary), GO, Path(__file__).resolve(),
             root / '.agent-state/task-planning-recovery/pg_only_supervisor.py',
             root / 'tests/runnercontrol/process_crash_linux_test.go', root / 'go.mod', root / 'go.sum'}
    directories = {}
    for name, package in local.items():
        directory = Path(package.get('Dir', ''))
        if directory != root / name.removeprefix(MODULE + '/') or directory.resolve(strict=True) != directory:
            raise ValueError('local package directory disagrees with the actual module')
        for imported in package.get('Imports', []):
            if imported.startswith(MODULE + '/') and imported not in local:
                raise ValueError('local imported package omitted from closure')
        if any(package.get(field) for field in OTHER_SOURCES):
            raise ValueError('new native local build sources require explicit review')
        selected = package.get('GoFiles', [])
        ignored = package.get('IgnoredGoFiles', [])
        declared = {name for name in selected + ignored if not name.endswith('_test.go')}
        actual = {path.name for path in directory.iterdir()
                  if path.suffix == '.go' and not path.name.startswith(('.', '_')) and not path.name.endswith('_test.go')}
        if not selected or declared != actual:
            raise ValueError('local Go source omitted from metadata')
        for filename in declared:
            if Path(filename).name != filename:
                raise ValueError('non-local Go source name')
            paths.add(directory / filename)
        embedded = package.get('EmbedFiles', [])
        if bool(embedded) != bool(package.get('EmbedPatterns')):
            raise ValueError('incomplete embed metadata')
        for filename in embedded:
            path = directory / filename
            if not path.is_relative_to(directory) or path.resolve(strict=True) != path:
                raise ValueError('non-local embed input')
            paths.add(path)
        directories[str(directory)] = {'recursive': bool(embedded),
                                       'members': source_members(directory, bool(embedded))}
    return {'files': {str(path): digest(path) for path in sorted(paths)}, 'directories': directories}


def capture(driver, binary, deadline):
    if (os.environ.get('AGENTEAM_GO') != str(GO)
            or os.environ.get('GOFLAGS') != '-mod=readonly -p=1'):
        raise ValueError('crash metadata must match the fixed command build environment')
    env = dict(os.environ, GOTOOLCHAIN='local', GOENV='off', GOWORK='off',
               GOPROXY='off', GOSUMDB='off', GOTELEMETRY='off')
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise ValueError('original pre-tail budget exhausted')
    result = subprocess.run([str(GO), 'list', '-deps', '-json', '-race', '-mod=readonly', '-p=1',
                             './cmd/agenteam-runner'], cwd=ROOT, env=env,
                            capture_output=True, text=True, timeout=remaining)
    if result.returncode != 0 or time.monotonic() >= deadline:
        raise ValueError('actual Go metadata process failed or exceeded original budget')
    value = project(decode_packages(result.stdout), driver, binary)
    if time.monotonic() >= deadline:
        raise ValueError('original pre-tail budget exhausted during input snapshot')
    return value


def unchanged(value):
    try:
        return (all(digest(Path(path)) == expected for path, expected in value['files'].items())
                and all(source_members(Path(path), spec['recursive']) == spec['members']
                        for path, spec in value['directories'].items()))
    except (OSError, ValueError, TypeError, KeyError):
        return False
