// Independent specification vectors. No candidate modules are imported here.
export const ownerID = '019a0400-0000-7000-8000-000000000001'
export const projectID = '019a0400-0000-7000-8000-000000000002'
export const otherID = '019a0400-0000-7000-8000-000000000003'
export const operationID = '019a0400-0000-7000-8000-000000000004'
export const maxVersion = '9223372036854775807'

export const project = () => ({
  id: projectID,
  owner_user_id: ownerID,
  name: 'My.Project-1',
  normalized_name: 'my.project-1',
  description: '  preserve whitespace\tand\ntext <script>  ',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: '2026-10-08T00:00:00.000001Z',
  updated_at: '2026-10-08T00:00:00.000002Z',
  archived_at: null,
})

export const invalidVersions: unknown[] = [
  '0', '-1', '+1', '01', '1.0', '1e0', ' 1', '1 ', '9223372036854775808', 1, null,
]

export const invalidInstants = [
  '2026-02-29T00:00:00.000001Z',
  '2026-04-31T00:00:00.000001Z',
  '2026-10-08T24:00:00.000001Z',
  '2026-10-08T00:60:00.000001Z',
  '2026-10-08T00:00:60.000001Z',
  '2026-10-08T00:00:00Z',
  '2026-10-08T00:00:00.00001Z',
  '2026-10-08T00:00:00.0000001Z',
  '2026-10-08T00:00:00.000001+00:00',
]

export const invalidDescriptions = [
  '\ud800', '\udfff', 'a\ud800z', '\u0000', '\r', '\u001f', '\u007f',
  'x'.repeat(8193), '界'.repeat(2731),
]

export const validDescriptions = [
  '', '\t\n', '  original  ', 'x'.repeat(8192),
  '界'.repeat(2730) + 'xx', '😀'.repeat(2048), '\u2028\u2029',
]

export const validReturnTargets: Array<[string, string]> = [
  ['/', '/'], ['/projects', '/projects'],
  ['/Alice/My.Project-1', '/alice/my.project-1'],
  ['/a1b/demo', '/a1b/demo'],
  ['/a-b/demo/settings', '/a-b/demo/settings'],
  ['/a-b/demo/settings/general', '/a-b/demo/settings/general'],
  ['/ali--ce/demo', '/ali--ce/demo'],
  ['/admin/demo', '/admin/demo'], ['/root/demo', '/root/demo'],
  ['/support/demo', '/support/demo'], ['/projects/demo', '/projects/demo'],
  ...['forgot-password', 'reset-password'].flatMap((name): Array<[string, string]> =>
    ['', '/settings', '/settings/general'].map((suffix) => [
      `/${name}/demo${suffix}`, `/${name}/demo${suffix}`,
    ]),
  ),
  ...[
    '/settings/profile', '/settings/appearance', '/settings/password',
    '/system/users', '/system/invitations', '/system/providers', '/system/models',
    '/system/model-selection', '/system/account-security', '/system/smtp',
    '/system/outbound-policy', '/system/audit', '/system/runtime-information',
  ].map((path): [string, string] => [path, path]),
  [`/${'a'.repeat(32)}/${'Z'.repeat(64)}`, `/${'a'.repeat(32)}/${'z'.repeat(64)}`],
]

export const invalidReturnTargets: unknown[] = [
  undefined, null, 7, {}, [], ['/alice/demo'], ['/', '/alice/demo'],
  'alice/demo', 'https://example.test/alice/demo', '//alice/demo',
  '/alice//demo', '/alice/demo/', '/alice/./demo', '/alice/../demo',
  '/alice/.', '/alice/..', '/alice/demo/extra',
  '/alice/demo/settings/other', '/alice/demo/settings/general/extra',
  '/alice/demo?x=1', '/alice/demo#x', '/alice/demo?token=private',
  '/%61lice/demo', '/alice/%64emo', '/alice/%2f', '/alice/%252f',
  '/alice\\demo', '/alice/demo\\x', '/alice/é',
  '/ab/demo', '/-alice/demo', '/alice-/demo',
  `/${'a'.repeat(33)}/demo`, `/alice/${'a'.repeat(65)}`,
  '/forgot-password', '/reset-password', '/login', '/settings', '/system',
  '/forgot-password/demo?token=private', '/reset-password/demo#token',
  '/forgot-password/demo/extra', '/reset-password/%64emo',
  ...[
    'api', 'assets', 'auth', 'login', 'logout', 'invite', 'reset', 'settings',
    'system', 'personal', 'diagnostics', 'livez', 'readyz', 'debug',
  ].flatMap((name) => [`/${name}/demo`, `/${name.toUpperCase()}/demo`]),
  '/api/v1/projects', '/system/users/extra', '/settings/profile/extra',
]

// Build 100 exact, unique list rows. '<' occupies one input UTF-8 byte but the
// accepted Go encoder escapes it to six wire bytes, exercising the actual cap.
export function largePage() {
  return {
    items: Array.from({ length: 100 }, (_, i) => ({
      id: `019a0400-0000-7000-8000-${String(i + 100).padStart(12, '0')}`,
      name: `p${i}`, lifecycle: 'active', version: '1', description: '<'.repeat(8192),
    })),
    next_cursor: 'X'.repeat(8192),
  }
}

export function goEscapedJSON(value: unknown) {
  return JSON.stringify(value)
    .replace(/</g, '\\u003c')
    .replace(/>/g, '\\u003e')
    .replace(/&/g, '\\u0026')
    .replace(/\u2028/g, '\\u2028')
    .replace(/\u2029/g, '\\u2029')
}
