import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, accountTransport, type Fetch } from '../api/client'
import {
  captureProjectSecretCreate,
  captureProjectSecretUpdate,
  captureProjectSecretValue,
  createProjectSecretCommandsAPI,
} from '../api/project-secret-commands'
import {
  secretBase,
  secretID,
  secretJSON,
  secretProject,
  secretReceipt,
} from './project-secret-write.fixture'
import { secretMetadata } from './project-secrets.fixture'
const csrf = 'S'.repeat(43),
  key = 'secret-write-original-key',
  signal = new AbortController().signal
const targetID = secretID(10)
describe('Project Secret writes: closed material transport and safe receipts', () => {
  it('preserves exact scalar UTF8 material including controls, whitespace and CRLF at the formal byte limit', () => {
    for (const value of [
      ' \t\r\n ',
      '\b\f\u0001\u0085',
      '中文🙂\r\nsecond\rthird',
      '🙂'.repeat(16384),
    ]) {
      expect(captureProjectSecretValue(value) === value).toBe(true)
    }
    for (const value of ['', '\0', '\ud800', '\udfff', '🙂'.repeat(16384) + 'a', null, undefined]) {
      expect(() => captureProjectSecretValue(value)).toThrow(AccountFailure)
    }
    expect(
      captureProjectSecretCreate({
        variable_id: targetID,
        name: 'VALID',
        description: '',
        value: ' ',
      }).value === ' ',
    ).toBe(true)
  })
  it('distinguishes omitted fields from explicit replacement and rejects null, reserved names and extra material metadata', () => {
    expect(captureProjectSecretUpdate({ description: '' })).toEqual({ description: '' })
    for (const value of [
      {},
      { value: '' },
      { value: null },
      { value: undefined },
      { name: 'AGENTEAM_X' },
      { name: 'A\n' },
      { description: '\u0085' },
      { digest: 'not-a-wire-field' },
    ])
      expect(() => captureProjectSecretUpdate(value)).toThrow(AccountFailure)
  })
  it('sends the actual create and metadata-only/replace PATCH bodies, original version and safe closed receipt', async () => {
    let response: unknown = secretReceipt('create', { ...secretMetadata(), version: '1' })
    const fetch = vi.fn<Fetch>(async () => secretJSON(response)),
      api = createProjectSecretCommandsAPI(fetch)
    const material = 'test-only\r\nsecond\t ' // Synthetic fixture, never serialize requests to evidence.
    await api.create(
      secretProject,
      {
        variable_id: targetID,
        name: 'SERVICE_TOKEN',
        description: secretMetadata().description,
        value: material,
      },
      csrf,
      key,
      signal,
    )
    expect(fetch.mock.calls[0]![0]).toBe(secretBase)
    const body = JSON.parse(String(fetch.mock.calls[0]![1].body))
    expect(body.request.value === material).toBe(true)
    response = secretReceipt('update', { ...secretMetadata(), description: '', version: '2' })
    await api.update(secretProject, targetID, '1', { description: '' }, csrf, key, signal)
    expect(JSON.parse(String(fetch.mock.calls[1]![1].body))).toEqual({
      expected_version: '1',
      request: { description: '' },
    })
    response = secretReceipt('update', { ...secretMetadata(), version: '3' })
    const result = await api.update(
      secretProject,
      targetID,
      '2',
      { value: material },
      csrf,
      key,
      signal,
    )
    expect(Object.hasOwn(result.variable, 'value')).toBe(false)
    expect(fetch.mock.calls[2]![1].method).toBe('PATCH')
    expect(new Headers(fetch.mock.calls[2]![1].headers).get('Idempotency-Key')).toBe(key)
  })
  it('Lookup sends only original command identity and cannot prove or resubmit material', async () => {
    let response: unknown = {
      status: 'committed',
      receipt: secretReceipt('update', { ...secretMetadata(), version: '2' }),
    }
    const fetch = vi.fn<Fetch>(async () => secretJSON(response)),
      api = createProjectSecretCommandsAPI(fetch)
    await api.lookup(
      { kind: 'update', projectID: secretProject, targetID, expectedVersion: '1' },
      csrf,
      key,
      signal,
    )
    expect(JSON.parse(String(fetch.mock.calls[0]![1].body))).toEqual({
      command: 'project.secret_variable.update',
      target_id: targetID,
      expected_version: '1',
    })
    expect(fetch.mock.calls[0]![0]).toBe(secretBase + '/commands/lookup')
    response = { status: 'not_observed', receipt: null }
    expect(
      await api.lookup({ kind: 'create', projectID: secretProject, targetID }, csrf, key, signal),
    ).toEqual(response)
    expect(Object.keys(JSON.parse(String(fetch.mock.calls[1]![1].body))).sort()).toEqual([
      'command',
      'target_id',
    ])
  })
  it('rejects unsafe/foreign receipts, replacement no-op, duplicate JSON and transport option widening', async () => {
    let response: unknown = secretReceipt('update', { ...secretMetadata(), version: '2' })
    const fetch = vi.fn<Fetch>(async () =>
        typeof response === 'string'
          ? new Response(response, { headers: { 'Content-Type': 'application/json' } })
          : secretJSON(response),
      ),
      api = createProjectSecretCommandsAPI(fetch)
    for (const bad of [
      { ...secretReceipt('update', { ...secretMetadata(), version: '2' }), value: 'forbidden' },
      secretReceipt('update', { ...secretMetadata(), version: '2', project_id: secretID(88) }),
      secretReceipt('update', { ...secretMetadata(), version: '1' }, false),
      {
        ...secretReceipt('update', { ...secretMetadata(), version: '2' }),
        variable: { ...secretMetadata(), version: '2', credential_ref: secretID(42) },
      },
      JSON.stringify(secretReceipt('update', { ...secretMetadata(), version: '2' })).replace(
        '"changed":true',
        '"changed":true,"changed":true',
      ),
    ]) {
      response = bad
      await expect(
        api.update(secretProject, targetID, '1', { value: 'synthetic' }, csrf, key, signal),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    const count = fetch.mock.calls.length
    await expect(
      accountTransport(fetch)('lookupProjectSecret', (v) => v, {
        signal,
        projectID: secretProject,
        target: targetID,
        csrf,
        key,
        body: {},
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch.mock.calls).toHaveLength(count)
  })
})
