#!/usr/bin/env python3
"""Delete only the exact stopped invocation's confirmed leftover Go temporary directory."""
from pathlib import Path
import datetime
import hashlib
import json
import os
import shutil
import stat
import time

ROOT = Path(__file__).resolve().parent
DRIVER = ROOT.parent / 'pg-driver-v02'
RUN = DRIVER / 'runs/embedselect01'
LAUNCH = DRIVER / 'launches/embedselect01'
RUNTIME = RUN / 'runtime'
TARGET = RUNTIME / 'go-build907913867'


def bound(path):
    path = Path(path)
    raw = path.read_bytes()
    return {'path': str(path), 'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}


def load(path):
    return json.loads(Path(path).read_bytes())


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def write(name, value):
    (ROOT / name).write_text(json.dumps(value, indent=2) + '\n')


assert bound(RUN / 'result.json')['sha256'] == '9e918dc5b18ace77f3f2ab7a03408fe2e15db1dc09d0341a063157ca18d1ed8d'
assert bound(LAUNCH / 'result.json')['sha256'] == 'dfb457aefce2da773c12aca17e46992639521389d5c0bf5b85e66c4d9f9d3ddb'
inner, outer = load(RUN / 'result.json'), load(LAUNCH / 'result.json')
cleanup = load(RUN / 'cleanup.json')
command = load(RUN / 'command.json')
adopted = load(RUN / 'adopted-waits.json')
tail = load(RUN / 'owned-tail-retirement.json')
assert inner['accepted'] is False and inner['driver_exit'] == 255 and inner['double_cleanup'] is False
assert inner['actual_wait_completed'] is True and inner['watchdog_thread_joined'] is True
assert outer['actual_direct_wait_completed'] is True and outer['exit_code'] == 1
assert command['actual_wait_completed'] is True and command['watchdog_thread_joined'] is True
assert len(adopted) == 1 and all(row['actual_wait'] is True for row in adopted)
assert tail['actual_owned_completion'] is True and tail['remaining'] == []
assert len(cleanup) == 2
for row in cleanup:
    assert row['baseline_unchanged'] is True and row['remaining_new'] == [] and row['owned_processes'] == []
    assert len(row['exact_absent']) == 7 and all(value['absent'] is True for value in row['exact_absent'].values())
    assert row['runtime_entries'] == [TARGET.name]
assert not RUNTIME.is_symlink() and not TARGET.is_symlink()
assert RUNTIME.resolve(strict=True) == RUNTIME and TARGET.resolve(strict=True) == TARGET
assert TARGET.parent == RUNTIME and TARGET.is_dir()
assert sorted(path.name for path in RUNTIME.iterdir()) == [TARGET.name]
target_stat = TARGET.lstat()
assert stat.S_ISDIR(target_stat.st_mode) and target_stat.st_uid == os.getuid()
assert shutil.rmtree.avoids_symlink_attacks
observed = load(RUN / 'observed-processes.json')
assert len(observed) == inner['observed_processes'] == 81


def remaining_owned():
    remaining = []
    for value in observed.values():
        path = Path('/proc') / str(value['pid']) / 'stat'
        try:
            fields = path.read_text().rsplit(') ', 1)[1].split()
        except FileNotFoundError:
            continue
        if fields[19] == str(value['starttime']):
            remaining.append({'pid': value['pid'], 'starttime': fields[19], 'state': fields[0]})
    return remaining


owned_checks = []
for _ in range(2):
    owned_checks.append({'time': now(), 'remaining_same_identity': remaining_owned()})
    assert owned_checks[-1]['remaining_same_identity'] == []
    time.sleep(0.2)
originals = {name: bound(RUN / name) for name in ['result.json', 'command.json', 'cleanup.json',
             'adopted-waits.json', 'owned-tail-retirement.json', 'observed-processes.json', 'tcp-tail-observation.json']}
originals['outer-result.json'] = bound(LAUNCH / 'result.json')
before = {'state': 'EXACT_OWNED_RUNTIME_RECOVERY_READY', 'target': str(TARGET), 'runtime': str(RUNTIME),
          'realpath': str(TARGET.resolve()), 'symlink': False,
          'target_lstat': {'inode': target_stat.st_ino, 'device': target_stat.st_dev,
                           'uid': target_stat.st_uid, 'mode': oct(stat.S_IMODE(target_stat.st_mode))},
          'originals': originals, 'original_owned_observations_empty': True,
          'current_owned_identity_checks': owned_checks,
          'runtime_before': [TARGET.name], 'historical_failure_not_changed': True}
write('before.json', before)
# Guard the exact directory identity again immediately before deletion.
current = TARGET.lstat()
assert (current.st_dev, current.st_ino) == (target_stat.st_dev, target_stat.st_ino)
assert not TARGET.is_symlink() and TARGET.resolve(strict=True) == TARGET
started = time.monotonic()
shutil.rmtree(TARGET)
after = []
for _ in range(2):
    row = {'time': now(), 'target_exists': TARGET.exists(),
           'runtime_entries': sorted(path.name for path in RUNTIME.iterdir()),
           'remaining_same_owned_identity': remaining_owned()}
    assert row['target_exists'] is False and row['runtime_entries'] == [] and row['remaining_same_owned_identity'] == []
    after.append(row)
    time.sleep(0.2)
for key, original in originals.items():
    assert bound(original['path']) == original
result = {'state': 'EXACT_RUNTIME_RECOVERY_PASS_ORIGINAL_TEST_FAIL_UNCHANGED',
          'operation': {'python_function': 'shutil.rmtree', 'argument': str(TARGET)},
          'elapsed_seconds': time.monotonic() - started, 'after_twice': after,
          'original_bytes_unchanged': True, 'inner_accepted_still_false': load(RUN / 'result.json')['accepted'] is False,
          'original_double_cleanup_still_false': load(RUN / 'result.json')['double_cleanup'] is False,
          'shared_cache_touched': False, 'unknown_path_deleted': False, 'resource_or_test_retried': False,
          'scope': 'Post-run filesystem retirement only. Direct/adopted actual waits reused from original records; current81 identity absence does not claim direct wait for each transitive process. Non-owned PID1 shims untouched.'}
write('result.json', result)
print(json.dumps(result))
