import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  captureArtifactID,
  decodeArtifactMetadata,
  type ArtifactMetadata,
} from './project-artifacts'

export type ArtifactContentInput = Readonly<{
  name: string
  media_type: 'text/plain' | 'text/markdown'
  content: string
  description?: string
}>
export type ArtifactCreationResult = Readonly<{ artifact: ArtifactMetadata }>
export type ArtifactCreationLookup =
  | Readonly<{
      state: 'not_observed' | 'pending' | 'cancel_requested' | 'cancelled'
      artifact: null
    }>
  | Readonly<{ state: 'committed'; artifact: ArtifactMetadata }>
export type ArtifactCancellation =
  | Readonly<{ state: 'not_observed'; cleanup: null }>
  | Readonly<{ state: 'cancel_requested'; cleanup: 'pending' }>
  | Readonly<{ state: 'cancelled'; cleanup: 'completed' }>
export interface ArtifactCreationAPI {
  create(
    projectID: string,
    userID: string,
    input: ArtifactContentInput,
    options: WriteOptions,
  ): Promise<ArtifactCreationResult>
  lookup(
    projectID: string,
    userID: string,
    input: ArtifactContentInput,
    options: WriteOptions,
  ): Promise<ArtifactCreationLookup>
  cancel(
    projectID: string,
    userID: string,
    input: ArtifactContentInput,
    options: WriteOptions,
  ): Promise<ArtifactCancellation>
}
const encoder = new TextEncoder()
const invalidSurrogate = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u
function requireValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function text(value: unknown, maximum: number, minimum = 0) {
  const result = string(value, minimum, maximum)
  requireValue(!invalidSurrogate.test(result) && encoder.encode(result).byteLength <= maximum)
  return result
}
export function captureArtifactContent(value: unknown): ArtifactContentInput {
  try {
    const raw = shape(value, ['name', 'media_type', 'content'], ['description'])
    const name = text(raw.name, 255, 1),
      content = text(raw.content, 1048576)
    requireValue(
      !/[\p{Cc}/\\]/u.test(name) &&
        (raw.media_type === 'text/plain' || raw.media_type === 'text/markdown'),
    )
    const description = Object.hasOwn(raw, 'description') ? text(raw.description, 4096) : undefined
    requireValue(description === undefined || !description.includes('\0'))
    const input = Object.freeze({
      name,
      media_type: raw.media_type,
      content,
      ...(description === undefined ? {} : { description }),
    })
    requireValue(encoder.encode(JSON.stringify(input)).byteLength <= 7340032)
    return input
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function captureWrite(value: WriteOptions) {
  try {
    const raw = shape(value, ['csrfToken', 'key', 'signal'])
    const csrf = string(raw.csrfToken, 43, 43),
      key = string(raw.key, 1, 128)
    requireValue(
      /^[A-Za-z0-9_-]{43}$/.test(csrf) &&
        /^[A-Za-z0-9._:/-]{1,128}$/.test(key) &&
        raw.signal instanceof AbortSignal,
    )
    return { csrf, key, signal: raw.signal as AbortSignal }
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function createArtifactCreationAPI(fetcher?: Fetch): ArtifactCreationAPI {
  const transport = accountTransport(fetcher)
  async function request(
    action: 'create' | 'lookup' | 'cancel',
    projectID: string,
    userID: string,
    value: ArtifactContentInput,
    write: WriteOptions,
  ) {
    const project = captureArtifactID(projectID),
      user = captureArtifactID(userID),
      input = captureArtifactContent(value),
      options = captureWrite(write)
    const digest = await crypto.subtle.digest('SHA-256', encoder.encode(input.content))
    if (options.signal.aborted) throw new AccountFailure('cancelled')
    const sha =
      'sha256:' +
      Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('')
    const metadata = (raw: unknown) => {
      const artifact = decodeArtifactMetadata(raw)
      requireValue(
        artifact.artifact_ref.project_id === project &&
          artifact.kind === 'generated' &&
          artifact.created_by_user_id === user &&
          artifact.created_by_agent_id === undefined &&
          artifact.source_execution_id === undefined &&
          artifact.source_operation_id === undefined &&
          artifact.name === input.name &&
          (artifact.description ?? '') === (input.description ?? '') &&
          artifact.object.media_type === input.media_type &&
          artifact.object.byte_size === String(encoder.encode(input.content).byteLength) &&
          artifact.object.sha256 === sha,
      )
      return artifact
    }
    return transport(
      action === 'create'
        ? 'createArtifactContent'
        : action === 'lookup'
          ? 'lookupArtifactCreation'
          : 'cancelArtifactCreation',
      (raw): ArtifactCreationResult | ArtifactCreationLookup | ArtifactCancellation => {
        if (action === 'create')
          return Object.freeze({ artifact: metadata(shape(raw, ['artifact']).artifact) })
        if (action === 'lookup') {
          const result = shape(raw, ['state', 'artifact'])
          if (result.state === 'committed')
            return Object.freeze({ state: 'committed', artifact: metadata(result.artifact) })
          requireValue(
            (result.state === 'not_observed' ||
              result.state === 'pending' ||
              result.state === 'cancel_requested' ||
              result.state === 'cancelled') &&
              result.artifact === null,
          )
          return Object.freeze<ArtifactCreationLookup>({ state: result.state, artifact: null })
        }
        const result = shape(raw, ['state', 'cleanup'])
        if (result.state === 'not_observed' && result.cleanup === null)
          return Object.freeze<ArtifactCancellation>({ state: 'not_observed', cleanup: null })
        if (result.state === 'cancel_requested' && result.cleanup === 'pending')
          return Object.freeze<ArtifactCancellation>({
            state: 'cancel_requested',
            cleanup: 'pending',
          })
        requireValue(result.state === 'cancelled' && result.cleanup === 'completed')
        return Object.freeze<ArtifactCancellation>({ state: 'cancelled', cleanup: 'completed' })
      },
      { ...options, projectID: project, body: input },
    )
  }
  return {
    create: (project, user, input, options) =>
      request('create', project, user, input, options) as Promise<ArtifactCreationResult>,
    lookup: (project, user, input, options) =>
      request('lookup', project, user, input, options) as Promise<ArtifactCreationLookup>,
    cancel: (project, user, input, options) =>
      request('cancel', project, user, input, options) as Promise<ArtifactCancellation>,
  }
}
