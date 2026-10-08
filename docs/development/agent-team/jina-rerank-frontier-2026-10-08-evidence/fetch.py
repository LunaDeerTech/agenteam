import hashlib
import json
import pathlib
import subprocess
import sys
import time

root = pathlib.Path(__file__).resolve().parent
name, url = sys.argv[1:]
assert name.replace('-', '').isalnum()
target = root / 'sources' / (name + '.raw')
headers = root / 'evidence' / (name + '.headers')
stderr = root / 'evidence' / (name + '.stderr')
meta_path = root / 'evidence' / (name + '.meta.json')
assert not target.exists() and not meta_path.exists()
argv = ['curl', '--silent', '--show-error', '--fail-with-body', '--location', '--max-redirs', '3', '--connect-timeout', '10', '--max-time', '45', '--max-filesize', '2097152', '--proto', '=https', '--proto-redir', '=https', '--dump-header', str(headers), '--output', str(target), '--write-out', '%{http_code}\t%{url_effective}\t%{content_type}', url]
started = time.time_ns()
with stderr.open('wb') as error_file:
    process = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=error_file)
    output, _ = process.communicate()
record = {'argv': argv, 'cwd': str(pathlib.Path.cwd()), 'url': url, 'started_ns': started, 'ended_ns': time.time_ns(), 'pid': process.pid, 'exit_code': process.returncode, 'http_observation': output.decode(), 'request_method': 'GET', 'authentication': 'none; no API key, cookie or model request supplied', 'license': 'To be determined from official source; public accessibility is not a redistribution license.'}
if target.exists():
    record.update({'bytes': target.stat().st_size, 'sha256': hashlib.sha256(target.read_bytes()).hexdigest(), 'path': str(target.relative_to(root))})
record['stderr_sha256'] = hashlib.sha256(stderr.read_bytes()).hexdigest()
meta_path.write_text(json.dumps(record, indent=2) + '\n')
print(json.dumps(record))
raise SystemExit(process.returncode)
