from pathlib import Path
import calendar, hashlib, json, re, sys
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

body_dir, output = map(Path, sys.argv[1:3])
schema = Path('/workspace/scratch/project-owner-audit-http-author/wire02/src/api/openapi/project-audit.json')
common = Path('/workspace/scratch/project-owner-audit-http-spec-verification/rev1/fixed/api/openapi/common.json')
sha = lambda b: hashlib.sha256(b).hexdigest()
doc = json.loads(schema.read_bytes())
base = schema.as_uri()
registry = Registry().with_resource(base, Resource.from_contents(doc, default_specification=DRAFT202012)).with_resource((schema.parent / 'common.json').as_uri(), Resource.from_contents(json.loads(common.read_bytes()), default_specification=DRAFT202012))
checker = FormatChecker()

@checker.checks('date-time')
def instant(value):
    if not isinstance(value, str):
        return True
    match = re.fullmatch(r'(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?Z', value)
    if not match:
        return False
    y, mo, day, hour, minute, second = map(int, match.groups())
    return 1 <= mo <= 12 and 1 <= day <= calendar.monthrange(y, mo)[1] and hour < 24 and minute < 60 and second < 60

results = []
for name, method, component in [('independent-root-list', 'GET', 'ProjectAuditPage'), ('independent-root-detail', 'GET', 'ProjectAuditRecord'), ('independent-root-head', 'HEAD', None)]:
    path = body_dir / (name + '.json')
    raw = path.read_bytes()
    source = json.loads((body_dir / (name + '-source.json')).read_bytes())
    assert source['method'] == method and source['status'] == 200
    assert source['body_sha256'] == sha(raw) and source['schema_sha256'] == sha(schema.read_bytes())
    assert source['content_type'] == 'application/json'
    assert source['request_id'] and source['source_run'] and source['candidate'] and source['input'] and source['project_id']
    if component:
        validator = Draft202012Validator({'$ref': base + '#/components/schemas/' + component}, registry=registry, format_checker=checker)
        assert validator.is_valid(json.loads(raw)), 'ORIGINAL_AUDIT_BODY_SCHEMA_FAILED'
        assert int(source['content_length']) == len(raw)
    else:
        assert raw == b''
        detail = json.loads((body_dir / 'independent-root-detail-source.json').read_bytes())
        assert source['path'] == detail['path'] and source['content_length'] == detail['content_length']
    results.append({'name': name, 'method': method, 'bytes': len(raw), 'body_sha256': sha(raw), 'source_sha256': sha((body_dir / (name + '-source.json')).read_bytes()), 'schema_component': component, 'status': 'PASS'})
output.write_text(json.dumps({'status': 'PASS_ORIGINAL_BYTES_STANDARD_SCHEMA_AND_HEAD', 'schema_sha256': sha(schema.read_bytes()), 'common_sha256': sha(common.read_bytes()), 'parser_sha256': sha(Path(__file__).read_bytes()), 'results': results}, indent=2) + '\n')
print(json.dumps({'status': 'PASS', 'GET_standard_schema': 2, 'HEAD_zero_body': 1, 'byte_counts': [r['bytes'] for r in results]}))
