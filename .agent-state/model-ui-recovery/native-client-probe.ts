import { createProjectModelSettingsAPI, type ProjectConfigurationCommand } from '../../web/src/api/project-models'
import { AccountFailure, type Fetch } from '../../web/src/api/client'

// This module is bundled privately and installed before the production app.
// Request bodies, keys and CSRF values remain in this browser closure only.
type PrivateRequest = { path: string; query: string; method: string; body: string | null; headers: Headers }
type Observation = { request: PrivateRequest; token: string | null; status: number; headers: Headers; chunks: Uint8Array[]; eof: boolean; ended: boolean; cancelled: boolean; released: boolean }
type SafeResponse = { token: string; method: string; endpoint: string; query: string; status: number; content_type: string; content_length: string; request_id: string; raw: number[] }
const fail = () => { throw new Error('PROJECT_MODELS_NATIVE_BOUNDARY_FAILED') }
function need(value: unknown): asserts value { if (!value) fail() }
function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`
  if (value !== null && typeof value === 'object') return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonical((value as Record<string, unknown>)[key])}`).join(',')}}`
  return JSON.stringify(value)
}

// Diagnostic-only Session slot. It never retains bytes, parses a body, or
// contributes to the Model operation facts or acceptance gates.
function sessionDiagnostics(nativeFetch: typeof window.fetch) {
  const initial = () => ({ requests: 0, readers: 0, read_calls: 0, read_settled: 0, read_rejected: 0, bytes: 0,
    reader_cancel_calls: 0, reader_cancel_settled: 0, reader_cancel_rejected: 0,
    stream_cancel_calls: 0, stream_cancel_settled: 0, stream_cancel_rejected: 0,
    release_calls: 0, release_successes: 0, abort_events: 0, headers_seen: false,
    status_ok: false, read_done: false, cancel_before_eof: false, failure: 'none' });
  type Slot = { id: number; facts: ReturnType<typeof initial>; requestID: string | null; signal: AbortSignal | null; detach: () => void };
  let serial = 0, current: Slot | undefined;
  function close() { current?.detach(); current = undefined }
  function begin() { close(); current = { id: ++serial, facts: initial(), requestID: null, signal: null, detach: () => {} }; return serial }
  function end(id: number | null, expectedID: string | null) {
    if (!current || current.id !== id) return null;
    const slot = current;
    const result = { ...slot.facts, request_id_match: slot.facts.requests === 1 && !!slot.requestID && slot.requestID === expectedID,
      signal_aborted: slot.signal?.aborted === true };
    close(); return result;
  }
  function fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> | undefined {
    const slot = current;
    if (!slot) return;
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, location.origin);
    const method = init?.method ?? (input instanceof Request ? input.method : 'GET');
    if (url.origin !== location.origin || url.pathname !== '/api/v1/session' || url.search || method !== 'GET') return;
    slot.facts.requests++;
    const pending = nativeFetch(input, init);
    if (slot.facts.requests !== 1) return pending;
    slot.signal = init?.signal ?? (input instanceof Request ? input.signal : null);
    const abort = () => { slot.facts.abort_events++ };
    slot.signal?.addEventListener('abort', abort, { once: true });
    slot.detach = () => slot.signal?.removeEventListener('abort', abort);
    const failed = (code: string) => { if (slot.facts.failure === 'none') slot.facts.failure = code };
    function observe<T>(work: Promise<T>, done: (value: T) => void, rejected: () => void) {
      // The client receives the original Promise. Both branches and any
      // diagnostic callback failure are handled on this separate branch.
      void work.then(done, rejected).catch(() => failed('observer-error'));
    }
    observe(pending, (response) => {
      slot.facts.headers_seen = true; slot.facts.status_ok = response.status === 200;
      slot.requestID = response.headers.get('X-Request-ID');
      const stream = response.body;
      if (!stream) return;
      const streamCancel = stream.cancel.bind(stream), getReader = stream.getReader.bind(stream);
      stream.cancel = (...args) => {
        slot.facts.stream_cancel_calls++; slot.facts.cancel_before_eof ||= !slot.facts.read_done;
        const result = streamCancel(...args);
        observe(result, () => { slot.facts.stream_cancel_settled++ }, () => {
          slot.facts.stream_cancel_settled++; slot.facts.stream_cancel_rejected++; failed('stream-cancel-rejected');
        });
        return result;
      };
      Object.defineProperty(stream, 'getReader', { value: (...args: unknown[]) => {
        slot.facts.readers++;
        let reader: ReadableStreamDefaultReader<Uint8Array>;
        try { reader = Reflect.apply(getReader, stream, args) }
        catch (error) { failed('get-reader-threw'); throw error }
        const read = reader.read.bind(reader), cancel = reader.cancel.bind(reader), release = reader.releaseLock.bind(reader);
        reader.read = (...args: unknown[]) => {
          slot.facts.read_calls++;
          const result = Reflect.apply(read, reader, args) as ReturnType<typeof read>;
          observe(result, (value) => {
            slot.facts.read_settled++;
            if (value.done) slot.facts.read_done = true;
            else slot.facts.bytes += value.value.byteLength;
          }, () => { slot.facts.read_settled++; slot.facts.read_rejected++; failed('read-rejected') });
          return result;
        };
        reader.cancel = (...args) => {
          slot.facts.reader_cancel_calls++; slot.facts.cancel_before_eof ||= !slot.facts.read_done;
          const result = cancel(...args);
          observe(result, () => { slot.facts.reader_cancel_settled++ }, () => {
            slot.facts.reader_cancel_settled++; slot.facts.reader_cancel_rejected++; failed('reader-cancel-rejected');
          });
          return result;
        };
        reader.releaseLock = () => {
          slot.facts.release_calls++;
          try { release(); slot.facts.release_successes++ }
          catch (error) { failed('release-threw'); throw error }
        };
        return reader;
      } });
    }, () => failed('fetch-rejected'));
    return pending;
  }
  return { begin, end, close, fetch };
}

export function install() {
  const nativeFetch = window.fetch.bind(window)
  const session = sessionDiagnostics(nativeFetch)
  const observations: Observation[] = []
  let disposed = false
  const projectPath = /^\/api\/v1\/projects\/([0-9a-f-]{36})\/(model-providers|models|available-chat-models|model-credentials|model-commands\/lookup|model-credential-commands\/lookup)(?:\/([0-9a-f-]{36}))?$/
  const projectFetch: typeof window.fetch = async (input, init) => {
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, location.origin)
    if (url.origin !== location.origin || !projectPath.test(url.pathname)) return nativeFetch(input, init)
    need(!disposed && typeof input === 'string' && init && (!init.body || typeof init.body === 'string'))
    const entry: Observation = {
      request: { path: url.pathname, query: url.search.slice(1), method: init.method ?? 'GET', body: typeof init.body === 'string' ? init.body : null, headers: new Headers(init.headers) },
      token: null, status: 0, headers: new Headers(), chunks: [], eof: false, ended: false, cancelled: false, released: false,
    }
    observations.push(entry)
    try {
      const response = await nativeFetch(input, init)
      entry.status = response.status
      entry.headers = new Headers(response.headers)
      entry.token = response.headers.get('X-Project-Models-Request')
      need(response.body)
      const stream = response.body
      const getReader = stream.getReader.bind(stream)
      // Observe the actual reader used by accountTransport, without a clone,
      // tee, eager read, buffered replacement or change to cancellation order.
      Object.defineProperty(stream, 'getReader', { value: () => {
        const reader = getReader()
        const read = reader.read.bind(reader), cancel = reader.cancel.bind(reader), release = reader.releaseLock.bind(reader)
        reader.read = async () => {
          try {
            const value = await read()
            if (value.done) entry.eof = true
            else entry.chunks.push(value.value.slice())
            return value
          } catch (error) { entry.ended = true; throw error }
        }
        reader.cancel = async (reason) => {
          try { await cancel(reason) } finally { entry.cancelled = true }
        }
        reader.releaseLock = () => { release(); entry.released = true; entry.ended = true }
        return reader
      } })
      return response
    } catch (error) { entry.ended = true; throw error }
  }
  window.fetch = (input, init) => session.fetch(input, init) ?? projectFetch(input, init)

  function publicFacts() {
    return observations.map((o) => ({ token: o.token, method: o.request.method, path: o.request.path, query: o.request.query, status: o.status,
      eof: o.eof, ended: o.ended, cancelled: o.cancelled, released: o.released, bytes: o.chunks.reduce((n, c) => n + c.length, 0),
      chunks: o.chunks.map((c) => c.length), has_body: o.request.body !== null,
      mutation_headers: o.request.headers.has('X-CSRF-Token') || o.request.headers.has('Idempotency-Key') }))
  }
  function command(request: PrivateRequest): ProjectConfigurationCommand {
    const match = projectPath.exec(request.path); need(match)
    const kind = `${match[2] === 'model-providers' ? 'provider' : 'model'}.${request.method === 'POST' ? 'create' : request.method === 'PUT' ? 'update' : 'delete'}`
    const body = JSON.parse(request.body ?? '{}')
    if (kind === 'provider.create') return { kind, input: body.input }
    if (kind === 'provider.update') return { kind, id: match[3]!, expected_version: body.expected_version, input: body.input }
    if (kind === 'provider.delete') return { kind, id: match[3]!, expected_version: body.expected_version }
    if (kind === 'model.delete') return { kind, id: match[3]!, expected_version: body.expected_version, replacement: body.replacement }
    // Model HTTP omits immutable protocol/context. Obtain it only from a real
    // complete Provider/Model response already consumed in this browser.
    let providerID: string | undefined = body.provider_id
    if (!providerID) {
      for (const o of observations) {
        if (o.eof && o.status === 200 && o.request.path === request.path && o.request.method === 'GET') providerID = decoded(o).provider_id
      }
    }
    need(providerID)
    let protocol: 'openai-chat-completions' | 'anthropic-messages' | undefined
    for (const o of observations) {
      if (!o.eof || o.status !== 200 || !o.request.path.startsWith(`/api/v1/projects/${match[1]}/model-providers`)) continue
      const value = decoded(o)
      for (const provider of value.items ?? [value]) if (provider.id === providerID) protocol = provider.input.protocol
    }
    need(protocol)
    if (kind === 'model.create') return { kind, provider_id: providerID, protocol, input: body.input }
    need(kind === 'model.update')
    return { kind, provider_id: providerID, protocol, id: match[3]!, expected_version: body.expected_version, input: body.input }
  }
  function decoded(o: Observation): any {
    const raw = new Uint8Array(o.chunks.reduce((n, c) => n + c.length, 0)); let at = 0
    for (const chunk of o.chunks) { raw.set(chunk, at); at += chunk.length }
    try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(raw)) } finally { raw.fill(0) }
  }
  async function verify(value: SafeResponse) {
    const matches = observations.filter((o) => o.token === value.token); need(matches.length === 1)
    const observed = matches[0]!, original = observed.request
    need(observed.ended && observed.eof && observed.released && original.method === value.method && original.path === value.endpoint && original.query === value.query && observed.status === value.status)
    need(observed.headers.get('Content-Type') === value.content_type && (observed.headers.get('Content-Length') ?? '') === value.content_length && observed.headers.get('X-Request-ID') === value.request_id)
    let offset = 0
    for (const chunk of observed.chunks) for (const byte of chunk) need(value.raw[offset++] === byte)
    need(offset === value.raw.length)
    let invoked = 0, eof = false, released = false
    const fetcher: Fetch = async (url, init) => {
      invoked++
      need(invoked === 1 && url === original.path + (original.query ? `?${original.query}` : '') && init.method === original.method && (init.body ?? null) === original.body)
      const headers = new Headers(init.headers)
      for (const name of ['X-CSRF-Token', 'Idempotency-Key']) need(headers.get(name) === original.headers.get(name))
      let position = 0
      const stream = new ReadableStream<Uint8Array>({ pull(controller) {
        const chunk = observed.chunks[position++]
        if (chunk) controller.enqueue(chunk.slice()); else controller.close()
      } })
      const getReader = stream.getReader.bind(stream)
      Object.defineProperty(stream, 'getReader', { value: () => {
        const reader = getReader(), read = reader.read.bind(reader), release = reader.releaseLock.bind(reader)
        reader.read = async () => { const result = await read(); if (result.done) eof = true; return result }
        reader.releaseLock = () => { release(); released = true }
        return reader
      } })
      return new Response(stream, { status: value.status, headers: { 'Content-Type': value.content_type, 'Content-Length': value.content_length, 'X-Request-ID': value.request_id, 'Cache-Control': 'no-store' } })
    }
    const api = createProjectModelSettingsAPI(fetcher), match = projectPath.exec(original.path); need(match)
    const project = match[1]!, leaf = match[2]!, target = match[3]!, signal = new AbortController().signal
    const options = { key: original.headers.get('Idempotency-Key')!, csrfToken: original.headers.get('X-CSRF-Token')!, signal }
    const body = JSON.parse(original.body ?? '{}')
    const query = Object.fromEntries(new URLSearchParams(original.query))
    const page = { ...(query.cursor ? { cursor: query.cursor } : {}), ...(query.limit ? { limit: Number(query.limit) } : {}) }
    try {
      let result: unknown
      if (original.method === 'GET') {
        result = leaf === 'model-providers' ? target ? await api.getProvider(project, target, signal) : await api.listProviders(project, page, signal)
          : leaf === 'models' ? target ? await api.getModel(project, target, signal) : await api.listModels(project, page, signal)
          : leaf === 'available-chat-models' ? await api.listAvailableChatModels(project, page, signal)
          : await api.getCredentialMetadata(project, target, signal)
      } else if (leaf === 'model-commands/lookup') {
        const selected = observations.find((o) => o.request.path.startsWith(`/api/v1/projects/${project}/`) && o.request.headers.get('Idempotency-Key') === options.key && !o.request.path.endsWith('/lookup'))
        need(selected)
        const originalCommand = command(selected.request); need(originalCommand.kind === body.command)
        result = await api.lookupConfiguration(project, originalCommand, options)
      } else if (leaf === 'model-credential-commands/lookup') result = await api.lookupCredential(project, body, options)
      else if (leaf === 'model-credentials') {
        result = original.method === 'POST' ? await api.createCredential(project, body.value, options)
          : original.method === 'PUT' ? await api.updateCredential(project, target, body.expected_version, body.value, options)
          : await api.deleteCredential(project, target, body.expected_version, options)
      } else {
        const captured = command(original)
        switch (captured.kind) {
          case 'provider.create': result = await api.createProvider(project, captured.input, options); break
          case 'provider.update': result = await api.updateProvider(project, captured.id, captured.expected_version, captured.input, options); break
          case 'provider.delete': result = await api.deleteProvider(project, captured.id, captured.expected_version, options); break
          case 'model.create': result = await api.createModel(project, { provider_id: captured.provider_id, protocol: captured.protocol }, captured.input, options); break
          case 'model.update': result = await api.updateModel(project, { id: captured.id, provider_id: captured.provider_id, protocol: captured.protocol }, captured.expected_version, captured.input, options); break
          case 'model.delete': result = await api.deleteModel(project, captured.id, captured.expected_version, captured.replacement, options); break
        }
      }
      need(value.status === 200 && canonical(result) === canonical(JSON.parse(new TextDecoder().decode(Uint8Array.from(value.raw)))))
    } catch (error) { need(value.status !== 200 && error instanceof AccountFailure && error.kind === 'problem') }
    need(invoked === 1 && eof && released)
    return { native_eof: true, typed_client_ok: true }
  }
  Object.defineProperty(window, '__projectModelsProbe', { value: {
    facts: publicFacts, verify, sessionBegin: session.begin, sessionEnd: session.end,
    dispose() { session.close(); for (const o of observations) { o.request.body = null; o.request.headers = new Headers(); for (const chunk of o.chunks) chunk.fill(0) }; observations.length = 0; disposed = true },
  } })
}
