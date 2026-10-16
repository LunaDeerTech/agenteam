import { describe, expect, it, vi } from 'vitest'
import { createAccountAPI, type ProfileInput, type ProfileView } from '../api/account'
import { AccountFailure } from '../api/client'

const id = '01900000-0000-7000-8000-000000000001'
const profile: ProfileView = {
  user: {
    id,
    email: 'person@example.com',
    username: 'admin',
    display_name: ' 显示 ',
    role: 'admin',
    theme: 'system',
    version: '9007199254740993',
    initial_password_suggestion: true,
  },
  avatar: { media_type: 'image/png', byte_size: '12', sha256: 'sha256:' + 'a'.repeat(64) },
}
const options = { csrfToken: 'S'.repeat(43), key: 'original-key' }
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status < 400 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id,
    },
  })
const capture = (response: () => Response) => {
  const calls: { url: string; init: RequestInit }[] = []
  const api = createAccountAPI(async (url, init) => {
    calls.push({ url, init })
    return response()
  })
  return { api, calls }
}

describe('personal Account wire', () => {
  it('uses the eight exact methods and preserves explicit empty, optional absence and original File', async () => {
    const response = vi.fn(() => json(profile))
    const { api, calls } = capture(response)
    expect(await api.getProfile()).toEqual(profile)
    const update = { version: profile.user.version, display_name: '' }
    await api.updateProfile(update, options)
    expect(JSON.parse(calls[1]!.init.body as string)).toEqual(update)
    expect(Object.hasOwn(JSON.parse(calls[1]!.init.body as string), 'username')).toBe(false)
    response.mockImplementation(() => json({ version: profile.user.version, theme: 'dark' }))
    await api.getPreferences()
    await api.setPreferences({ version: profile.user.version, theme: 'dark' }, options)
    response.mockImplementation(
      () =>
        new Response(new Uint8Array([137, 80, 78, 71]), {
          status: 200,
          headers: {
            'Content-Type': 'image/png',
            'Content-Length': '4',
            ETag: '"sha256:' + 'b'.repeat(64) + '"',
          },
        }),
    )
    const image = await api.readAvatar()
    expect(image.blob.size).toBe(4)
    expect(image.metadata.byte_size).toBe('4')
    response.mockImplementation(() => json(profile))
    const file = new File([new Uint8Array([137, 80, 78, 71])], 'selected.png', {
      type: 'image/png',
    })
    await api.putAvatar({ version: profile.user.version, file, mediaType: 'image/png' }, options)
    expect(calls[5]!.init.body === file).toBe(true)
    expect((calls[5]!.init.headers as Record<string, string>)['If-Match']).toBe(
      '"9007199254740993"',
    )
    response.mockImplementation(() => new Response(null, { status: 204 }))
    expect(await api.deleteAvatar({ version: profile.user.version }, options)).toBeUndefined()
    response.mockImplementation(() => json({ completed: true, next_path: '/' }))
    const password = '  原始 字符 123456789  '
    await api.changePassword(
      {
        version: profile.user.version,
        current_password: 'x',
        new_password: password,
        confirmation: password,
      },
      options,
    )
    const sent = JSON.parse(calls[7]!.init.body as string)
    expect(
      sent.new_password === password &&
        sent.confirmation === password &&
        sent.current_password === 'x',
    ).toBe(true)
    expect(calls.map((c) => [c.init.method, c.url])).toEqual([
      ['GET', '/api/v1/me'],
      ['PATCH', '/api/v1/me'],
      ['GET', '/api/v1/me/preferences'],
      ['PUT', '/api/v1/me/preferences'],
      ['GET', '/api/v1/me/avatar'],
      ['PUT', '/api/v1/me/avatar'],
      ['DELETE', '/api/v1/me/avatar'],
      ['POST', '/api/v1/me/change-password'],
    ])
    for (const { init } of calls) {
      expect(init.credentials).toBe('same-origin')
      expect(init.cache).toBe('no-store')
      expect(init.redirect).toBe('error')
      const headers = init.headers as Record<string, string>
      expect(
        Object.keys(headers).some((key) =>
          ['Cookie', 'Host', 'Origin', 'Content-Length', 'Range'].includes(key),
        ),
      ).toBe(false)
      if (init.method === 'GET') expect(init.body).toBeUndefined()
      else
        expect(
          headers['X-CSRF-Token'] === options.csrfToken &&
            headers['Idempotency-Key'] === options.key,
        ).toBe(true)
    }
  })
  it('rejects missing/null fields, invalid complete User and malformed scalar forms', async () => {
    const malformed = [
      { user: profile.user },
      { user: { ...profile.user, role: 'owner' }, avatar: null },
      { user: { ...profile.user, initial_password_suggestion: 1 }, avatar: null },
      { user: { ...profile.user, version: 9007199254740992 }, avatar: null },
      { user: { ...profile.user, version: '9223372036854775808' }, avatar: null },
      { user: profile.user, avatar: { ...profile.avatar, byte_size: '-1' } },
      { user: profile.user, avatar: { ...profile.avatar, byte_size: '01' } },
      { user: profile.user, avatar: { ...profile.avatar, sha256: 'sha256:' + 'A'.repeat(64) } },
    ]
    for (const value of malformed)
      await expect(capture(() => json(value)).api.getProfile()).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    const { api, calls } = capture(() => json(profile))
    for (const value of [
      { version: '1' },
      { version: '1', username: null },
      { version: '1', display_name: null },
      { version: '1', display_name: '', role: 'admin' },
      { version: '0', display_name: '' },
    ])
      await expect(
        Promise.resolve().then(() => api.updateProfile(value as ProfileInput, options)),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(calls.length).toBe(0)
    expect(await capture(() => json({ ...profile, avatar: null })).api.getProfile()).toEqual({
      ...profile,
      avatar: null,
    })
    expect(
      (
        await capture(() =>
          json({ ...profile, avatar: { ...profile.avatar, byte_size: '9223372036854775807' } }),
        ).api.getProfile()
      ).avatar?.byte_size,
    ).toBe('9223372036854775807')
  })
  it.each([
    'length-zero',
    'length-large',
    'truncated',
    'overflow',
    'media',
    'etag',
    'partial',
    'html',
  ])('rejects unsafe avatar responses and closes their stream: %s', async (mode) => {
    let cancelled = false
    const stream = new ReadableStream<Uint8Array>({
      start(s) {
        s.enqueue(new Uint8Array(mode === 'overflow' ? 5 : 3))
        s.close()
      },
      cancel() {
        cancelled = true
      },
    })
    const headers = {
      'Content-Type':
        mode === 'media' ? 'image/svg+xml' : mode === 'html' ? 'text/html' : 'image/png',
      'Content-Length': mode === 'length-zero' ? '0' : mode === 'length-large' ? '5242881' : '4',
      ETag: mode === 'etag' ? 'sha256:' + 'b'.repeat(64) : '"sha256:' + 'b'.repeat(64) + '"',
    }
    const response = new Response(stream, { status: mode === 'partial' ? 206 : 200, headers })
    await expect(createAccountAPI(async () => response).readAvatar()).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(response.body?.locked).toBe(false)
    // The pre-read branches cancel queued bytes; consumed/truncated EOF is already terminal.
    if (['length-zero', 'length-large', 'media', 'etag', 'partial', 'html'].includes(mode))
      expect(cancelled).toBe(true)
  })
  it('preserves safe Problems and read/mutation commit states, and never parses a DELETE 204', async () => {
    const problem = {
      type: 'urn:agenteam:problem:dependency-unavailable',
      title: 'Unavailable',
      status: 503,
      detail: 'Safe failure',
      instance: '/api/v1/me',
      code: 'DEPENDENCY_UNAVAILABLE',
      request_id: id,
      commit_state: 'unknown',
      retry_hint: 'lookup',
      field_errors: [{ path: '/password', code: 'INVALID' }],
    }
    const { api } = capture(() => json(problem, 503))
    for (const call of [
      () => api.getProfile(),
      () => api.updateProfile({ version: '1', display_name: '' }, options),
      () => api.readAvatar(),
    ]) {
      const e = await call().catch((e: unknown) => e)
      expect(e).toBeInstanceOf(AccountFailure)
      expect((e as AccountFailure).problem).toEqual(problem)
    }
    await expect(
      capture(() => json({ ...problem, commit_state: ['unknown'] }, 503)).api.getPreferences(),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(
      capture(() => json({ completed: true, next_path: '//example.com' })).api.changePassword(
        {
          version: '1',
          current_password: 'x',
          new_password: 'password 123456789',
          confirmation: 'password 123456789',
        },
        options,
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
