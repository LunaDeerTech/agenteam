#!/usr/bin/env python3
"""Resolve local response references: no HEAD response may declare a body."""
import json
from pathlib import Path

schema = json.loads(Path('/workspace/agenteam-knowledge-http/api/openapi/knowledge-owner.json').read_text())


def response(value):
    seen = set()
    while '$ref' in value:
        ref = value['$ref']
        assert ref.startswith('#/') and ref not in seen, 'nonlocal/cyclic response ref'
        seen.add(ref)
        value = schema
        for key in ref[2:].split('/'):
            value = value[key.replace('~1', '/').replace('~0', '~')]
    return value


bad = [(path, code) for path, item in schema['paths'].items()
       for code, value in item['head']['responses'].items()
       if response(value).get('content')]
print(json.dumps({'head_operations': len(schema['paths']),
                  'head_responses_declaring_body': len(bad), 'violations': bad}))
assert not bad, 'HEAD response declares content although actual Account/handler writes no body'
