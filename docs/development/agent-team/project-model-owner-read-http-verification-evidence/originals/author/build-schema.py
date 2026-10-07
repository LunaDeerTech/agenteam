from pathlib import Path
import copy
import json

repo = Path('/workspace/agenteam')
target = repo / 'api/openapi/project-models.json'
assert not target.exists()
system = json.loads((repo / 'api/openapi/model-system.json').read_text())

def common(name): return {'$ref': './common.json#/components/schemas/' + name}
def ref(name): return {'$ref': '#/components/schemas/' + name}
def obj(properties): return {'type': 'object', 'additionalProperties': False, 'required': list(properties), 'properties': properties}
def nullable(schema): return {'oneOf': [schema, {'type': 'null'}]}
def array_enum(values): return {'type': 'array', 'items': {'enum': values}, 'uniqueItems': True, 'maxItems': len(values)}
name = {'type': 'string', 'minLength': 1, 'maxLength': 128, 'not': {'pattern': '\u0000'}, 'description': 'UTF-8, at most 128 code points and 512 bytes; NUL forbidden.'}
schemas = {}
schemas['ProjectScope'] = obj({'kind': {'const': 'project'}, 'project_id': common('ID')})
schemas['SystemScope'] = obj({'kind': {'const': 'system'}})
schemas['AvailableScope'] = {'oneOf': [ref('ProjectScope'), ref('SystemScope')], 'description': 'A Project scope must equal the requested Project. This equality is checked by the HTTP adapter.'}
schemas['Capabilities'] = obj({
    'tool_calls': {'type': 'boolean'}, 'parallel_tool_calls': {'type': 'boolean'},
    'streaming': {'type': 'boolean'}, 'reasoning': {'type': 'boolean'},
    'input_modalities': array_enum(['text', 'image', 'file', 'vector']),
    'output_modalities': array_enum(['text', 'image', 'file', 'vector']),
    'reasoning_efforts': {'type': 'array', 'uniqueItems': True, 'items': {
        'type': 'string', 'minLength': 1, 'maxLength': 32, 'pattern': '^[A-Za-z0-9_.:-]+$',
        'not': {'pattern': '[^A-Za-z0-9_.:-]'}}},
    'structured_output_modes': array_enum(['text', 'json_schema']),
    'context_length': nullable(common('PositiveInt64String')),
    'max_output': nullable(common('PositiveInt64String')),
})
schemas['Capabilities']['allOf'] = [
    {'if': {'properties': {'parallel_tool_calls': {'const': True}}, 'required': ['parallel_tool_calls']}, 'then': {'properties': {'tool_calls': {'const': True}}}},
    {'if': {'properties': {'reasoning': {'const': False}}, 'required': ['reasoning']}, 'then': {'properties': {'reasoning_efforts': {'maxItems': 0}}}},
]
schemas['Capabilities']['description'] = 'When both counts are present, max_output <= context_length is additionally enforced by the typed validator. reasoning_efforts has no business total-count limit or fixed value enumeration. The HTTP representation budget applies before copying/validation/encoding.'
dynamic = {'type': 'object', 'additionalProperties': True, 'description': 'Original typed configuration permits this dynamic JSON object. UTF-8 byte limits and overwrite restrictions are checked by the typed validator; schema acceptance alone does not establish these conditions.'}
schemas['ProviderInput'] = obj({
    'name': copy.deepcopy(name), 'protocol': {'enum': ['openai-chat-completions', 'anthropic-messages']},
    'base_url': {'type': 'string', 'minLength': 1, 'maxLength': 8192, 'format': 'uri', 'description': 'HTTP(S) endpoint, at most 8192 UTF-8 bytes; no userinfo, query, fragment or invalid port. The typed validator enforces these conditions.'},
    'enabled': {'type': 'boolean'}, 'credential_ref': nullable(common('ID')), 'options': copy.deepcopy(dynamic),
})
schemas['ProviderInput']['description'] = 'Current Owner configuration only. The nullable credential ID refers to the same Project; no Secret material or metadata is exposed. Options occupy at most 64 KiB before encoding.'
schemas['ModelInput'] = obj({
    'name': copy.deepcopy(name),
    'provider_model_id': {'type': 'string', 'minLength': 1, 'maxLength': 256, 'not': {'pattern': '\u0000'}, 'description': 'At most 256 UTF-8 bytes, not merely characters.'},
    'type': {'const': 'chat'}, 'enabled': {'type': 'boolean'},
    'parameters': copy.deepcopy(dynamic), 'request_overwrite': copy.deepcopy(dynamic),
    'header_overwrite': {'type': 'object', 'propertyNames': {'type': 'string', 'minLength': 1, 'pattern': '^[A-Za-z0-9-]+$', 'not': {'pattern': '[^A-Za-z0-9-]'}}, 'additionalProperties': {'type': 'string', 'not': {'pattern': '[\r\n\u0000]'}}, 'description': 'Case-insensitive duplicate names, protected headers, 16 KiB combined name/value bytes and 64 KiB request-overwrite plus header bytes are enforced by the typed validator.'},
    'capabilities': ref('Capabilities'),
})
schemas['Provider'] = obj({'id': common('ID'), 'scope': ref('ProjectScope'), 'input': ref('ProviderInput'), 'version': common('PositiveInt64String'), 'created_at': common('Instant'), 'updated_at': common('Instant')})
schemas['Model'] = obj({'id': common('ID'), 'provider_id': common('ID'), 'scope': ref('ProjectScope'), 'input': ref('ModelInput'), 'version': common('PositiveInt64String'), 'created_at': common('Instant'), 'updated_at': common('Instant')})
schemas['AvailableChatModel'] = obj({'id': common('ID'), 'provider_id': common('ID'), 'scope': ref('AvailableScope'), 'name': copy.deepcopy(name), 'provider_name': copy.deepcopy(name), 'version': common('PositiveInt64String'), 'capabilities': ref('Capabilities')})
schemas['AvailableChatModel']['description'] = 'Exactly seven safe fields from the one-statement enabled Provider + enabled chat Model union of System and the requested Project. This is configured availability, not a claim that endpoint/model invocation succeeded.'
for page, row in [('ProviderPage', 'Provider'), ('ModelPage', 'Model'), ('AvailableChatModelPage', 'AvailableChatModel')]:
    schemas[page] = obj({'items': {'type': 'array', 'maxItems': 100, 'items': ref(row)}, 'next_cursor': nullable({'type': 'string', 'minLength': 1, 'maxLength': 8192})})
    schemas[page]['description'] = 'Empty items is []; no continuation is null. Item count does not exceed requested limit. Original cursor watermarks and binding apply; cursor has no TTL. Current authority is checked on every page.'

headers = {'Cache-Control': {'schema': {'const': 'no-store'}}, 'X-Content-Type-Options': {'schema': {'const': 'nosniff'}}, 'Referrer-Policy': {'schema': {'const': 'no-referrer'}}, 'X-Request-ID': {'schema': common('ID')}, 'Content-Length': {'schema': {'type': 'string', 'pattern': '^[0-9]+$'}}}
query = [
    {'in': 'query', 'name': 'cursor', 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 8192}, 'description': 'Single nonempty opaque cursor, decoded size <=8192 bytes. No TTL; a retired/unavailable signing kid fails verification. Same current Owner/query binding required.'},
    {'in': 'query', 'name': 'limit', 'schema': {'type': 'string', 'pattern': '^(?:[1-9]|[1-9][0-9]|100)$', 'not': {'pattern': '[^0-9]'}, 'default': '50'}, 'description': 'One canonical decimal value from 1 to 100. No sign, spaces, leading zero, empty value or repetition.'},
]
paths = {}
base = '/api/v1/projects/{project_id}/'
for suffix, schema, target_id, list_op in [
    ('model-providers', 'ProviderPage', None, True), ('model-providers/{provider_id}', 'Provider', 'provider_id', False),
    ('models', 'ModelPage', None, True), ('models/{model_id}', 'Model', 'model_id', False),
    ('available-chat-models', 'AvailableChatModelPage', None, True),
]:
    params = [{'in': 'path', 'name': 'project_id', 'required': True, 'schema': common('ID')}]
    if target_id: params.append({'in': 'path', 'name': target_id, 'required': True, 'schema': common('ID')})
    if list_op: params += copy.deepcopy(query)
    ops = {}
    for method in ['get', 'head']:
        responses = {'200': {'description': 'Complete validated representation within the 8 MiB admission/encoding bound. HEAD performs the same query and encoding with no body.', 'headers': copy.deepcopy(headers)}}
        if method == 'get': responses['200']['content'] = {'application/json': {'schema': ref(schema)}}
        for code in ['400', '401', '403', '404', '405', '409', '500', '503']:
            item = {'description': 'Original public Problem. Read COMMIT_UNKNOWN remains unknown with zero success representation. Output budget failure is DEPENDENCY_UNAVAILABLE/NotStarted, not a database rollback claim.', 'headers': copy.deepcopy(headers)}
            if code == '405': item['headers']['Allow'] = {'schema': {'const': 'GET, HEAD'}}
            if method == 'get': item['content'] = {'application/problem+json': {'schema': common('Problem')}}
            responses[code] = item
        ops[method] = {'operationId': method + '_project_' + schema.lower(), 'security': [{'Session': []}, {'LocalSession': []}],
            'description': 'Current Human Owner only; administrator has no cross-Owner exemption. Account boundary and same-transaction Session/Owner/gate apply. Exactly one query; no write/lookup/Provider invocation. One two-second publication/I/O budget begins before authentication and includes EOF, service, complete admission/validation/encoding, Body.Close, Write/Flush and callback retirement. No request body; all undocumented, repeated, empty or malformed query parameters and empty ? are rejected; details accept no query. Raw query <=32768 bytes. Timeout/partial I/O aborts rather than publishing a late Problem. Model/Provider configuration includes disabled entries; available-chat-models includes only enabled Provider + Model chat union.',
            'parameters': copy.deepcopy(params), 'responses': responses}
    paths[base + suffix] = ops
document = {'openapi': '3.1.0', 'info': {'title': 'Project Model configuration and safe available chat read API', 'version': '1.0.0',
    'description': 'Five Owner GET/HEAD resources only. The new HTTP representation limit is 8 MiB; it does not bound prior accepted database/library allocations or process RSS. Legal oversized results produce zero success projection; no array/item truncation or automatic smaller-limit retry. Existing System routes are unchanged.'},
    'paths': paths, 'components': {'securitySchemes': copy.deepcopy(system['components']['securitySchemes']), 'schemas': schemas}}
target.write_text(json.dumps(document, ensure_ascii=False, indent=2) + '\n')
print(str(target))
