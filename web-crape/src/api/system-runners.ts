import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'
import { parseSystemInstant } from './system-account'

export type RunnerHello = Readonly<{
  runner_id: string
  runner_version: string
  protocol_version: Readonly<{ major: number; minor: number }>
  os: 'linux' | 'darwin'
  arch: 'amd64' | 'arm64'
  headless: boolean
  capabilities: readonly string[]
  feature_flags: readonly string[]
}>
export type SystemRunner = Readonly<{
  id: string
  name: string
  description: string
  tags: readonly string[]
  root_path: string
  version: string
  credential_generation: string
  status: 'online' | 'offline' | 'incompatible'
  public_key_fingerprint: string | null
  enrolled_at: string | null
  last_seen_at: string | null
  last_hello: RunnerHello | null
  created_at: string
  updated_at: string
}>
export type RunnerPage = Readonly<{ items: readonly SystemRunner[]; next: string | null }>
export type RunnerQuery = Readonly<{ limit?: number; after_id?: string }>
export type RunnerCommandName =
  'runner.create' | 'runner.update' | 'runner.enrollment.issue' | 'runner.revoke'
export type RunnerCreate = Readonly<{
  runner_id: string
  name: string
  description: string
  tags: readonly string[]
  root_path: string
}>
export type RunnerUpdate = Readonly<{
  expected_version: string
  name?: string
  description?: string
  tags?: readonly string[]
}>
export type RunnerCommand = Readonly<{
  command: RunnerCommandName
  target: string
  request: RunnerCreate | RunnerUpdate | Readonly<{ expected_version: string }>
}>
export type RunnerReceipt = Readonly<{
  command_id: string
  command: RunnerCommandName
  runner: SystemRunner
  changed: boolean
}>
// Material is never part of progress, receipt, original intent, storage or diagnostics.
export type RunnerMutation =
  | Readonly<{ receipt: RunnerReceipt; token_available: false }>
  | Readonly<{
      receipt: RunnerReceipt
      token_available: true
      enrollment_token: string
      expires_at: string
    }>
export type RunnerLookup =
  | Readonly<{ state: 'not_observed'; token_available: false }>
  | Readonly<{ state: 'committed'; receipt: RunnerReceipt; token_available: false }>
export interface SystemRunnersAPI {
  list(query: RunnerQuery, signal: AbortSignal): Promise<RunnerPage>
  get(id: string, signal: AbortSignal): Promise<SystemRunner>
  execute(command: RunnerCommand, options: WriteOptions): Promise<RunnerMutation>
  lookup(command: RunnerCommand, options: WriteOptions): Promise<RunnerLookup>
}
const encoder = new TextEncoder()
const commands: readonly RunnerCommandName[] = [
  'runner.create',
  'runner.update',
  'runner.enrollment.issue',
  'runner.revoke',
]
function valid(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function id(value: unknown) {
  const s = string(value, 36, 36)
  valid(uuid7.test(s))
  return s
}
function version(value: unknown) {
  const s = string(value, 1, 19)
  valid(/^[1-9]/.test(s) && !/[^0-9]/.test(s) && BigInt(s) <= 9223372036854775807n)
  return s
}
function text(
  value: unknown,
  min: number,
  max: number,
  controls = /[\u0000-\u001f\u007f-\u009f]/u,
) {
  const s = string(value, min, max)
  valid(
    encoder.encode(s).length <= max &&
      !controls.test(s) &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(s),
  )
  return s
}
export function runnerTextOrder(a: string, b: string) {
  const x = encoder.encode(a),
    y = encoder.encode(b)
  for (let n = 0; n < Math.min(x.length, y.length); n++) if (x[n] !== y[n]) return x[n]! - y[n]!
  return x.length - y.length
}
function tags(value: unknown) {
  valid(Array.isArray(value) && value.length <= 32)
  const out = Array.from(value, (v) => text(v, 1, 64))
  valid(out.every((s, n) => n === 0 || runnerTextOrder(out[n - 1]!, s) < 0))
  return Object.freeze(out)
}
function rootPath(value: unknown) {
  const s = string(value, 1, 4096)
  valid(
    encoder.encode(s).length <= 4096 &&
      s.startsWith('/') &&
      !/[\u0000\\]/.test(s) &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(s),
  )
  valid(
    s === '/' ||
      s
        .slice(1)
        .split('/')
        .every((p) => p.length > 0 && p !== '.' && p !== '..'),
  )
  return s
}
function instant(value: unknown) {
  const s = parseSystemInstant(value)
  valid(s !== '0001-01-01T00:00:00.000000Z')
  return s
}
function names(value: unknown) {
  valid(Array.isArray(value) && value.length <= 32)
  const out = Array.from(value, (v) => {
    const s = string(v, 1, 64)
    valid(/^[a-z]/.test(s) && !/[^a-z0-9_.-]/.test(s))
    return s
  })
  valid(new Set(out).size === out.length)
  return Object.freeze(out)
}
function hello(value: unknown, runner: string): RunnerHello {
  const v = shape(value, [
    'runner_id',
    'runner_version',
    'protocol_version',
    'os',
    'arch',
    'headless',
    'capabilities',
    'feature_flags',
  ])
  const runner_id = id(v.runner_id),
    // Device protocol text rejects C0/DEL, while management labels also reject C1.
    runner_version = text(v.runner_version, 1, 64, /[\u0000-\u001f\u007f]/u),
    pv = shape(v.protocol_version, ['major', 'minor'])
  valid(
    runner_id === runner &&
      [pv.major, pv.minor].every(
        (x) => typeof x === 'number' && Number.isInteger(x) && x >= 0 && x <= 65535,
      ),
  )
  const protocol_version = Object.freeze({ major: pv.major as number, minor: pv.minor as number })
  valid(
    (v.os === 'linux' || v.os === 'darwin') &&
      (v.arch === 'amd64' || v.arch === 'arm64') &&
      typeof v.headless === 'boolean',
  )
  return Object.freeze({
    runner_id,
    runner_version,
    protocol_version,
    os: v.os,
    arch: v.arch,
    headless: v.headless,
    capabilities: names(v.capabilities),
    feature_flags: names(v.feature_flags),
  })
}
export function parseSystemRunner(value: unknown, target?: string): SystemRunner {
  const v = shape(value, [
    'id',
    'name',
    'description',
    'tags',
    'root_path',
    'version',
    'credential_generation',
    'status',
    'public_key_fingerprint',
    'enrolled_at',
    'last_seen_at',
    'last_hello',
    'created_at',
    'updated_at',
  ])
  const runner = id(v.id),
    created_at = instant(v.created_at),
    updated_at = instant(v.updated_at)
  valid(
    (target === undefined || runner === target) &&
      updated_at >= created_at &&
      ['online', 'offline', 'incompatible'].includes(v.status as string),
  )
  const fingerprint =
    v.public_key_fingerprint === null ? null : string(v.public_key_fingerprint, 71, 71)
  valid(
    fingerprint === null ||
      (/^sha256:/.test(fingerprint) && !/[^a-f0-9]/.test(fingerprint.slice(7))),
  )
  const enrolled = v.enrolled_at === null ? null : instant(v.enrolled_at),
    seen = v.last_seen_at === null ? null : instant(v.last_seen_at)
  const last = v.last_hello === null ? null : hello(v.last_hello, runner)
  valid(
    (fingerprint === null) === (enrolled === null) &&
      (enrolled === null || enrolled >= created_at) &&
      (seen === null || seen >= created_at),
  )
  valid(
    v.status !== 'online' ||
      (fingerprint !== null && enrolled !== null && seen !== null && last !== null),
  )
  return Object.freeze({
    id: runner,
    name: text(v.name, 1, 128),
    description: text(v.description, 0, 4096),
    tags: tags(v.tags),
    root_path: rootPath(v.root_path),
    version: version(v.version),
    credential_generation: version(v.credential_generation),
    status: v.status as SystemRunner['status'],
    public_key_fingerprint: fingerprint,
    enrolled_at: enrolled,
    last_seen_at: seen,
    last_hello: last,
    created_at,
    updated_at,
  })
}
function input<T>(fn: () => T): T {
  try {
    return fn()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureRunnerID(value: unknown) {
  return input(() => id(value))
}
export function captureRunnerQuery(value: RunnerQuery) {
  return input(() => {
    const v = shape(value, [], ['limit', 'after_id']),
      limit = Object.hasOwn(v, 'limit') ? v.limit : 50
    valid(Number.isInteger(limit) && Number(limit) >= 1 && Number(limit) <= 200)
    return Object.freeze({
      limit: Number(limit),
      ...(Object.hasOwn(v, 'after_id') ? { after_id: id(v.after_id) } : {}),
    })
  })
}
export function captureRunnerCommand(value: RunnerCommand): RunnerCommand {
  return input(() => {
    const v = shape(value, ['command', 'target', 'request']),
      target = id(v.target)
    valid(commands.includes(v.command as RunnerCommandName))
    const command = v.command as RunnerCommandName
    let request: RunnerCommand['request']
    if (command === 'runner.create') {
      const r = shape(v.request, ['runner_id', 'name', 'description', 'tags', 'root_path'])
      valid(id(r.runner_id) === target)
      request = Object.freeze({
        runner_id: target,
        name: text(r.name, 1, 128),
        description: text(r.description, 0, 4096),
        tags: tags(r.tags),
        root_path: rootPath(r.root_path),
      })
    } else if (command === 'runner.update') {
      const r = shape(v.request, ['expected_version'], ['name', 'description', 'tags'])
      valid(['name', 'description', 'tags'].some((k) => Object.hasOwn(r, k)))
      request = Object.freeze({
        expected_version: version(r.expected_version),
        ...(Object.hasOwn(r, 'name') ? { name: text(r.name, 1, 128) } : {}),
        ...(Object.hasOwn(r, 'description') ? { description: text(r.description, 0, 4096) } : {}),
        ...(Object.hasOwn(r, 'tags') ? { tags: tags(r.tags) } : {}),
      })
    } else {
      const r = shape(v.request, ['expected_version'])
      request = Object.freeze({ expected_version: version(r.expected_version) })
    }
    valid(encoder.encode(JSON.stringify({ command, request })).length <= 32768)
    return Object.freeze({ command, target, request })
  })
}
function receipt(value: unknown, original: RunnerCommand): RunnerReceipt {
  const v = shape(value, ['command_id', 'command', 'runner', 'changed']),
    runner = parseSystemRunner(v.runner, original.target)
  valid(
    v.command === original.command &&
      typeof v.changed === 'boolean' &&
      (v.command === 'runner.update' || v.changed === true),
  )
  const r = original.request
  if (original.command === 'runner.create')
    valid(
      runner.version === '1' &&
        runner.name === (r as RunnerCreate).name &&
        runner.description === (r as RunnerCreate).description &&
        runner.root_path === (r as RunnerCreate).root_path &&
        JSON.stringify(runner.tags) === JSON.stringify((r as RunnerCreate).tags),
    )
  else {
    const expected = BigInt((r as RunnerUpdate).expected_version)
    valid(BigInt(runner.version) === expected + (v.changed ? 1n : 0n))
    if (original.command === 'runner.update')
      for (const k of ['name', 'description', 'tags'] as const)
        if (Object.hasOwn(r, k))
          valid(JSON.stringify(runner[k]) === JSON.stringify((r as RunnerUpdate)[k]))
  }
  return Object.freeze({
    command_id: id(v.command_id),
    command: original.command,
    runner,
    changed: v.changed,
  })
}
export function createSystemRunnersAPI(fetcher?: Fetch): SystemRunnersAPI {
  const request = accountTransport(fetcher)
  const options = (o: WriteOptions) => ({
    csrf: o.csrfToken,
    key: o.key,
    signal: o.signal ?? new AbortController().signal,
  })
  return {
    async list(query, signal) {
      const q = captureRunnerQuery(query)
      return request(
        'listSystemRunners',
        (value) => {
          const v = shape(value, ['items', 'next'])
          valid(Array.isArray(v.items) && v.items.length <= q.limit)
          const items = Array.from(v.items, (x) => parseSystemRunner(x)),
            next = v.next === null ? null : id(v.next)
          valid(
            items.every((x, n) =>
              n === 0 ? !q.after_id || x.id > q.after_id : items[n - 1]!.id < x.id,
            ),
          )
          valid(next === null || (items.length > 0 && items.at(-1)?.id === next))
          return Object.freeze({ items: Object.freeze(items), next })
        },
        { signal, runnerQuery: q },
      )
    },
    get(target, signal) {
      const t = captureRunnerID(target)
      return request('getSystemRunner', (v) => parseSystemRunner(v, t), { signal, target: t })
    },
    async execute(value, o) {
      const c = captureRunnerCommand(value)
      const endpoint = (
        {
          'runner.create': 'createSystemRunner',
          'runner.update': 'updateSystemRunner',
          'runner.enrollment.issue': 'issueSystemRunnerEnrollment',
          'runner.revoke': 'revokeSystemRunner',
        } as const
      )[c.command]
      return request<RunnerMutation>(
        endpoint,
        (v) => {
          const m = shape(v, ['receipt', 'token_available'], ['enrollment_token', 'expires_at']),
            rec = receipt(m.receipt, c)
          valid(typeof m.token_available === 'boolean')
          if (!m.token_available) {
            valid(!Object.hasOwn(m, 'enrollment_token') && !Object.hasOwn(m, 'expires_at'))
            return Object.freeze({ receipt: rec, token_available: false as const })
          }
          valid(c.command === 'runner.create' || c.command === 'runner.enrollment.issue')
          const token = string(m.enrollment_token, 43, 43)
          valid(!/[^A-Za-z0-9_-]/.test(token) && 'AEIMQUYcgkosw048'.includes(token.at(-1)!))
          return Object.freeze({
            receipt: rec,
            token_available: true as const,
            enrollment_token: token,
            expires_at: instant(m.expires_at),
          })
        },
        {
          ...options(o),
          ...(c.command === 'runner.create' ? {} : { target: c.target }),
          body: c.request,
        },
      )
    },
    async lookup(value, o) {
      const c = captureRunnerCommand(value)
      return request<RunnerLookup>(
        'lookupSystemRunner',
        (value) => {
          const v = shape(value, ['state', 'token_available'], ['receipt'])
          valid(v.token_available === false)
          if (v.state === 'not_observed') {
            valid(!Object.hasOwn(v, 'receipt'))
            return Object.freeze({
              state: 'not_observed' as const,
              token_available: false as const,
            })
          }
          valid(v.state === 'committed')
          return Object.freeze({
            state: 'committed' as const,
            receipt: receipt(v.receipt, c),
            token_available: false as const,
          })
        },
        { ...options(o), target: c.target, body: { command: c.command, request: c.request } },
      )
    },
  }
}
