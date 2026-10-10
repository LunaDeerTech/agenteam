"""Read-only old-resource preflight for the existing Rename launch wrapper."""
from pathlib import Path
import importlib.util
import re
import subprocess

ROOT = Path(__file__).resolve().parents[2]


def missing_diagnostic(item, returncode, stderr):
    """Keep pg_only_supervisor.exact_absent's exact-ID predicate unchanged."""
    missing = re.compile(r'(?:No such (?:object|container|network):\s*'
                         + re.escape(item['id'])
                         + r'\b|network\s+' + re.escape(item['id'])
                         + r'\s+not found)', re.I)
    return returncode != 0 and missing.search(stderr) is not None


def check_previous(directory, launch_private, env):
    """Return safe observations on failure too; never create/remove resources."""
    source = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
    spec = importlib.util.spec_from_file_location('rename_original_supervisor', source)
    supervisor = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(supervisor)
    directory = Path(directory)
    record = supervisor.root_record(directory)
    if Path(launch_private) != Path(str(directory.parent) + '-private'):
        raise ValueError('exact previous launch-private path required')
    paths = [Path(p) for p in record['directories']]
    paths.extend((directory / 'runtime', Path(launch_private)))
    result = {'passed': False, 'observations': []}
    for number in (1, 2):
        observation = {'round': number, 'resources': [], 'private_absent': False}
        result['observations'].append(observation)
        for item in record['resources']:
            row = {'kind': item['kind'], 'id': item['id'], 'actual_wait': None,
                   'stderr_class': 'not_observed', 'exact_absent': False}
            observation['resources'].append(row)
            try:
                completed = subprocess.run(
                    ['docker', item['kind'], 'inspect', item['id']], env=env,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                    text=True, timeout=10)
            except subprocess.TimeoutExpired:
                row['stderr_class'] = 'inspect_timeout'
                return result
            except OSError:
                row['stderr_class'] = 'inspect_unavailable'
                return result
            row['actual_wait'] = completed.returncode
            row['exact_absent'] = missing_diagnostic(
                item, completed.returncode, completed.stderr)
            row['stderr_class'] = ('exact_target_missing' if row['exact_absent']
                                   else 'present' if completed.returncode == 0
                                   else 'other_error')
            if not row['exact_absent']:
                return result
        observation['private_absent'] = all(
            not path.exists() and not path.is_symlink() for path in paths)
        if not observation['private_absent']:
            return result
    result['passed'] = True
    return result
