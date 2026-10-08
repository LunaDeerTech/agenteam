from pathlib import Path
import hashlib
import json
import warnings
import jsonschema

root = Path(__file__).parent
owner_path = root / 'api/openapi/project-owner.json'
common_path = root / 'api/openapi/common.json'
owner = json.loads(owner_path.read_text())
common = json.loads(common_path.read_text())
page = {
    'items': [
        {'id': f'019a0400-0000-7000-8000-{i + 100:012d}', 'name': f'p{i}',
         'lifecycle': 'active', 'version': '1', 'description': '<' * 8192}
        for i in range(100)
    ],
    'next_cursor': 'X' * 8192,
}
with warnings.catch_warnings():
    warnings.simplefilter('ignore', DeprecationWarning)
    resolver = jsonschema.RefResolver(
        base_uri=owner_path.as_uri(), referrer=owner,
        store={owner_path.as_uri(): owner, common_path.as_uri(): common},
    )
    jsonschema.Draft202012Validator(
        owner['components']['schemas']['ProjectPage'], resolver=resolver,
        format_checker=jsonschema.FormatChecker(),
    ).validate(page)
raw = json.dumps(page, ensure_ascii=False, separators=(',', ':')).replace('<', '\\u003c').encode()
assert 600000 < len(raw) < 5242880
(root / 'large-page.json').write_bytes(raw)
result = {
    'status': 'PASS_CONTROLLED_SYNTHETIC_SCHEMA_ONLY', 'bytes': len(raw),
    'sha256': hashlib.sha256(raw).hexdigest(), 'rows': 100,
    'description_utf8_bytes_per_row': 8192, 'cursor_utf8_bytes': 8192,
    'schema': 'ProjectPage', 'schema_revision': 'ff396a4e',
    'same_bytes_consumed_by': 'client-contract.independent.spec.ts',
    'real_backend_response': False,
}
(root / 'large-page-result.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps(result, indent=2))
