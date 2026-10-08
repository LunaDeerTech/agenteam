from pathlib import Path
import collections
import datetime
import hashlib
import json
import os
import re
import selectors
import subprocess
import sys
import time

ROOT = Path('/workspace/scratch/image-pinned-source-proxy01')
LIMIT = 2097152
os.umask(0o077)


class Rejected(Exception):
    pass


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def identity(pid):
    try:
        raw = Path('/proc/' + str(pid) + '/stat').read_text()
        parts = raw[raw.rfind(')') + 2:].split()
        return {'pid': pid, 'starttime': parts[19], 'state': parts[0]}
    except FileNotFoundError:
        return None


def no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise Rejected('duplicate_json_member')
        result[key] = value
    return result


def git_object(kind, body):
    return hashlib.sha1(kind.encode() + b' ' + str(len(body)).encode() + b'\0' + body).hexdigest()


def bind_tree(body, binding):
    tree = json.loads(body.decode('utf-8'), object_pairs_hook=no_duplicates)
    if tree.get('sha') != binding['root_tree'] or tree.get('truncated') is not False:
        raise Rejected('tree_identity_or_completeness')
    entries = tree.get('tree')
    if not isinstance(entries, list):
        raise Rejected('tree_shape')
    indexed = {}
    children = collections.defaultdict(list)
    for entry in entries:
        path = entry['path']
        if not isinstance(path, str) or path.startswith('/') or any(p in ('', '.', '..') for p in path.split('/')) or '\0' in path:
            raise Rejected('tree_path')
        if path in indexed or not re.fullmatch('[0-9a-f]{40}', entry['sha']):
            raise Rejected('tree_entry_identity')
        if (entry['mode'], entry['type']) not in [('040000', 'tree'), ('100644', 'blob'), ('100755', 'blob'), ('120000', 'blob'), ('160000', 'commit')]:
            raise Rejected('tree_entry_mode_type')
        indexed[path] = entry
        parent, _, name = path.rpartition('/')
        children[parent].append((name, entry))
    wanted = [r['source_path'] for r in binding['urls'] if r['source_path']]
    ancestry = {''}
    for path in wanted:
        segments = path.split('/')
        ancestry.update('/'.join(segments[:n]) for n in range(1, len(segments)))
    proofs = []
    for parent in sorted(ancestry, key=lambda p: (p.count('/'), p)):
        immediate = sorted(children[parent], key=lambda item: item[0].encode() + (b'/' if item[1]['type'] == 'tree' else b''))
        raw = b''.join(format(int(e['mode'], 8), 'o').encode() + b' ' + name.encode() + b'\0' + bytes.fromhex(e['sha']) for name, e in immediate)
        expected = binding['root_tree'] if not parent else indexed[parent]['sha']
        actual = git_object('tree', raw)
        if actual != expected:
            raise Rejected('tree_ancestry_hash_mismatch')
        proofs.append({'path': parent or '.', 'entry_count': len(immediate), 'tree_bytes': len(raw), 'sha1': actual})
    sources = []
    for path in wanted:
        e = indexed[path]
        if e['type'] != 'blob' or e['mode'] != '100644' or not isinstance(e.get('size'), int) or not 0 < e['size'] < LIMIT:
            raise Rejected('source_entry_not_regular_bounded_blob')
        sources.append({'path': path, 'mode': e['mode'], 'bytes': e['size'], 'git_blob_sha1': e['sha']})
    if indexed['LICENSE']['sha'] != binding['license']['git_blob']:
        raise Rejected('existing_license_blob_mismatch')
    result = {'commit': binding['official_commit'], 'tree': binding['root_tree'], 'truncated': False, 'entries': len(entries), 'verified_ancestry': proofs, 'sources': sources, 'old_license_tree_entry_matches': True, 'signature_verified': False}
    write_json(ROOT / 'tree-binding.json', result)
    return result


def main():
    slug = sys.argv[1]
    binding = json.loads((ROOT / 'local-binding.json').read_text())
    ledger_path = ROOT / 'requests.json'
    ledger = json.loads(ledger_path.read_text())
    if ledger['stopped_on_failure'] or len(ledger['attempts']) >= 5:
        raise Rejected('closed_plan_stopped')
    plan = binding['urls'][len(ledger['attempts'])]
    if slug != plan['slug']:
        raise Rejected('wrong_order_or_retry')
    curl = Path(binding['curl']['path'])
    if hashlib.sha256(curl.read_bytes()).hexdigest() != binding['curl']['sha256']:
        raise Rejected('curl_changed_before_request')
    headers_path = ROOT / (slug + '.headers.raw')
    body_path = ROOT / (slug + '.body')
    stderr_path = ROOT / (slug + '.stderr.raw')
    argv = [str(curl), '--disable', '--silent', '--show-error', '--proto', '=https', '--proto-redir', '=https', '--max-redirs', '0', '--retry', '0', '--max-time', '29', '--max-filesize', str(LIMIT), '--request', 'GET', '--header', 'Accept: application/vnd.github+json' if slug == 'tree' else 'Accept: text/plain', '--header', 'Accept-Encoding: identity', '--user-agent', 'agenteam-pinned-public-source/1', '--dump-header', str(headers_path), '--output', '-', plan['url']]
    entry = {**plan, 'ordinal': len(ledger['attempts']) + 1, 'started_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'argv': argv, 'state': 'STARTED', 'environment_policy': 'inherit standard environment without reading or changing proxy/credentials/DNS/trust/no_proxy; curl config disabled; no netrc', 'body_limit_bytes': LIMIT, 'request_total_seconds_max': 30}
    ledger['attempts'].append(entry)
    write_json(ledger_path, ledger)
    process = None
    before_identity = None
    count = 0
    stdout_eof = False
    exit_code = None
    failure = None
    forced_kill = False
    status = None
    headers = []
    start = time.monotonic()
    try:
        with body_path.open('xb') as output, stderr_path.open('xb') as stderr:
            process = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=stderr, start_new_session=True)
            before_identity = identity(process.pid)
            entry['direct_process'] = before_identity
            write_json(ROOT / (slug + '.spawn.json'), entry)
            poller = selectors.DefaultSelector()
            poller.register(process.stdout, selectors.EVENT_READ)
            try:
                while not stdout_eof:
                    if time.monotonic() - start >= 29.5:
                        raise Rejected('outer_request_deadline')
                    for key, _ in poller.select(0.1):
                        chunk = os.read(key.fd, min(65536, LIMIT - count))
                        if not chunk:
                            stdout_eof = True
                            break
                        output.write(chunk)
                        count += len(chunk)
                        if count >= LIMIT:
                            raise Rejected('body_cap_reached_without_confirmed_EOF')
                exit_code = process.wait(timeout=max(0.05, 29.5 - (time.monotonic() - start)))
            finally:
                poller.close()
                process.stdout.close()
        if exit_code != 0:
            raise Rejected('curl_nonzero_exit')
    except Exception as error:
        failure = {'kind': type(error).__name__, 'code': str(error) if isinstance(error, Rejected) else 'local_exception_details_not_logged'}
    finally:
        if process is not None:
            if process.poll() is None:
                process.kill()
                forced_kill = True
            exit_code = process.wait()
        elapsed = time.monotonic() - start
        if not body_path.exists():
            body_path.write_bytes(b'')
        if not stderr_path.exists():
            stderr_path.write_bytes(b'')
        if not headers_path.exists():
            headers_path.write_bytes(b'')
    body = body_path.read_bytes()
    statuses = []
    current = {}
    for line in headers_path.read_bytes().splitlines():
        match = re.match(rb'HTTP/\S+ ([0-9]{3})(?: (.*))?$', line)
        if match:
            status = int(match[1])
            statuses.append({'status': status, 'reason': (match[2] or b'').decode('latin-1')})
            current = {}
        elif b':' in line:
            name, value = line.split(b':', 1)
            current.setdefault(name.decode('latin-1').lower(), []).append(value.strip().decode('latin-1'))
    selected_headers = {k: current[k] for k in ['content-type', 'content-length', 'content-encoding', 'date', 'etag', 'x-github-request-id', 'location'] if k in current}
    proof = None
    if failure is None:
        try:
            if elapsed > 30:
                raise Rejected('actual_elapsed_above_total_limit')
            if status != 200:
                raise Rejected('HTTP_status_not_200')
            if current.get('content-encoding', ['identity']) != ['identity']:
                raise Rejected('unexpected_content_encoding')
            lengths = current.get('content-length')
            if lengths is not None and (len(lengths) != 1 or not lengths[0].isdecimal() or int(lengths[0]) != len(body)):
                raise Rejected('declared_length_not_complete')
            if slug == 'tree':
                proof = bind_tree(body, binding)
            else:
                trees = json.loads((ROOT / 'tree-binding.json').read_text())
                wanted = next(s for s in trees['sources'] if s['path'] == plan['source_path'])
                actual = git_object('blob', body)
                if len(body) != wanted['bytes'] or actual != wanted['git_blob_sha1']:
                    raise Rejected('source_git_blob_mismatch')
                body.decode('utf-8')
                proof = {'path': plan['source_path'], 'bytes': len(body), 'git_blob_sha1': actual, 'matches_bound_tree_entry': True}
        except Exception as error:
            failure = {'kind': type(error).__name__, 'code': str(error) if isinstance(error, Rejected) else 'representation_or_binding_exception'}
    observations = [identity(process.pid), identity(process.pid)] if process is not None else []
    own_absent = process is not None and all(p is None or p['starttime'] != before_identity['starttime'] for p in observations)
    if process is not None and not own_absent and failure is None:
        failure = {'kind': 'Rejected', 'code': 'direct_identity_not_absent'}
    receipt = {**entry, 'state': 'PASS' if failure is None else 'FAIL_STOP', 'status': status, 'observed_status_lines': statuses, 'selected_headers': selected_headers, 'stdout_EOF': stdout_eof, 'body_bytes': len(body), 'body_sha256': hashlib.sha256(body).hexdigest(), 'curl_exit_code': exit_code, 'actual_wait_completed': process is not None and exit_code is not None, 'direct_identity_observations': observations, 'direct_identity_absent_twice': own_absent, 'whole_process_tree_claimed': False, 'forced_kill': forced_kill, 'elapsed_seconds': elapsed, 'failure': failure, 'binding': proof, 'redirect_followed': False, 'retry_count': 0, 'stderr_bytes': stderr_path.stat().st_size, 'stderr_sha256': hashlib.sha256(stderr_path.read_bytes()).hexdigest(), 'stderr_content_not_inspected_or_printed': True, 'proxy_values_not_inspected_or_recorded': True}
    write_json(ROOT / (slug + '.result.json'), receipt)
    ledger['attempts'][-1] = receipt
    ledger['stopped_on_failure'] = failure is not None
    write_json(ledger_path, ledger)
    print(json.dumps({k: receipt[k] for k in ['slug', 'state', 'status', 'body_bytes', 'body_sha256', 'curl_exit_code', 'actual_wait_completed', 'direct_identity_absent_twice', 'forced_kill', 'elapsed_seconds', 'failure']}, ensure_ascii=False))
    return 0 if failure is None else 1


if __name__ == '__main__':
    sys.exit(main())
