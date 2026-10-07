"""Validate frozen *actual response bytes* only, after the authorized PG run."""
import hashlib
import json
from pathlib import Path
import sys

from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012


def sha(data):
    return hashlib.sha256(data).hexdigest()


def bound(row):
    data = Path(row['path']).read_bytes()
    if sha(data) != row['sha256']:
        raise ValueError('frozen safe input changed')
    return data


def main():
    manifest = json.loads(bound({'path': sys.argv[1], 'sha256': sys.argv[2]}))
    registry = Registry()
    for row in manifest['schemas']:
        registry = registry.with_resource(row['uri'], Resource.from_contents(json.loads(bound(row)), default_specification=DRAFT202012))
    result = []
    for row in manifest['responses']:
        body = bound(row['body'])
        source = json.loads(bound(row['source']))
        if source['body_sha256'] != sha(body) or source['status'] != 200 or source['content_type'] != 'application/json':
            raise ValueError('actual HTTP metadata mismatch')
        if source['content_length'] != str(len(body)) or len(body) > 1024:
            raise ValueError('actual safe response representation mismatch')
        if source['candidate'] != manifest['candidate_sha256'] or source['run'] != manifest['source_run']:
            raise ValueError('HTTP source not from the frozen completed run')
        if source['schema_sha256'] != manifest['project_schema_sha256'] or source['method'] != row['method'] or source['path'] != row['http_path']:
            raise ValueError('HTTP target/schema provenance mismatch')
        schema = {'$ref': manifest['project_schema_uri'] + '#/components/schemas/' + row['schema']}
        Draft202012Validator(schema, registry=registry, format_checker=FormatChecker()).validate(json.loads(body))
        result.append({'name': row['name'], 'body_sha256': sha(body), 'bytes': len(body), 'schema': row['schema'], 'passed': True})
    if len(result) != 4:
        raise ValueError('expected four independent original safe bodies')
    print(json.dumps({'passed': True, 'responses': result, 'source_run': manifest['source_run'], 'original_bytes_reencoded': False}))


if __name__ == '__main__':
    main()
