import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  captureArtifactID,
  decodeArtifactMetadata,
  type ArtifactMetadata,
} from './project-artifacts'

export type ArtifactUploadIntent = Readonly<{
  name: string
  description?: string
  media_type: string
  byte_size: string
  sha256?: string
}>
export type ArtifactUploadResult =
  | Readonly<{
      state: 'not_observed' | 'pending' | 'uploaded' | 'cancel_requested' | 'cancelled'
      artifact: null
    }>
  | Readonly<{ state: 'committed'; artifact: ArtifactMetadata }>
export type ArtifactUploadCancellation =
  | Readonly<{ state: 'not_observed'; cleanup: null }>
  | Readonly<{ state: 'cancel_requested'; cleanup: 'pending' }>
  | Readonly<{ state: 'cancelled'; cleanup: 'completed' }>
export interface ArtifactUploadAPI {
  begin(
    project: string,
    user: string,
    intent: ArtifactUploadIntent,
    options: WriteOptions,
  ): Promise<ArtifactUploadResult>
  lookup(
    project: string,
    user: string,
    intent: ArtifactUploadIntent,
    options: WriteOptions,
  ): Promise<ArtifactUploadResult>
  payload(
    project: string,
    user: string,
    intent: ArtifactUploadIntent,
    file: Blob,
    options: WriteOptions,
  ): Promise<ArtifactUploadResult>
  complete(
    project: string,
    user: string,
    intent: ArtifactUploadIntent,
    options: WriteOptions,
  ): Promise<Readonly<{ artifact: ArtifactMetadata }>>
  cancel(
    project: string,
    user: string,
    intent: ArtifactUploadIntent,
    options: WriteOptions,
  ): Promise<ArtifactUploadCancellation>
}
const encoder = new TextEncoder()
function requireValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function text(value: unknown, maximum: number, minimum = 0) {
  const result = string(value, minimum, maximum)
  requireValue(
    !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result) &&
      encoder.encode(result).byteLength <= maximum,
  )
  return result
}
export function captureArtifactUpload(value: unknown): ArtifactUploadIntent {
  try {
    const raw = shape(value, ['name', 'media_type', 'byte_size'], ['description', 'sha256'])
    const name = text(raw.name, 255, 1),
      media = text(raw.media_type, 256, 1),
      size = string(raw.byte_size, 1, 10)
    requireValue(
      !/[\p{Cc}/\\]/u.test(name) && /^(0|[1-9][0-9]*)$/.test(size) && BigInt(size) <= 1073741824n,
    )
    // File.type normally supplies a parameterless MIME type. A caller may also
    // supply an already canonical parameter list; the server checks its exact
    // canonical form and the receipt must match this original intent.
    requireValue(
      !/[^\x20-\x7e]/.test(media) &&
        /^[!#$%&'*+.^_`|~0-9a-z-]+\/[!#$%&'*+.^_`|~0-9a-z-]+(?:; [^\r\n]+)?$/.test(media),
    )
    const description = Object.hasOwn(raw, 'description') ? text(raw.description, 4096) : undefined
    requireValue(description === undefined || !description.includes('\0'))
    const sha = Object.hasOwn(raw, 'sha256') ? string(raw.sha256, 71, 71) : undefined
    requireValue(sha === undefined || /^sha256:[0-9a-f]{64}$/.test(sha))
    const result = Object.freeze({
      name,
      media_type: media,
      byte_size: size,
      ...(description === undefined ? {} : { description }),
      ...(sha === undefined ? {} : { sha256: sha }),
    })
    requireValue(encoder.encode(JSON.stringify(result)).byteLength <= 32768)
    return result
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureArtifactUploadFile(file: File): ArtifactUploadIntent {
  if (!(file instanceof File)) throw new AccountFailure('invalid-input')
  // Browser-reported MIME is advisory. The original Object pipeline measures
  // the stream; absent/noncanonical browser types use the ordinary binary type.
  const media = /^[!#$%&'*+.^_`|~0-9a-z-]+\/[!#$%&'*+.^_`|~0-9a-z-]+$/.test(file.type)
    ? file.type
    : 'application/octet-stream'
  return captureArtifactUpload({ name: file.name, media_type: media, byte_size: String(file.size) })
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
export function createArtifactUploadAPI(fetcher?: Fetch): ArtifactUploadAPI {
  const transport = accountTransport(fetcher)
  async function request(
    action: 'begin' | 'lookup' | 'payload' | 'complete' | 'cancel',
    projectID: string,
    userID: string,
    value: ArtifactUploadIntent,
    write: WriteOptions,
    file?: Blob,
  ) {
    const project = captureArtifactID(projectID),
      user = captureArtifactID(userID),
      input = captureArtifactUpload(value),
      options = captureWrite(write)
    if (action === 'payload' && (!(file instanceof Blob) || file.size !== Number(input.byte_size)))
      throw new AccountFailure('invalid-input')
    const metadata = (raw: unknown) => {
      const artifact = decodeArtifactMetadata(raw)
      requireValue(
        artifact.artifact_ref.project_id === project &&
          artifact.kind === 'user_upload' &&
          artifact.created_by_user_id === user &&
          artifact.created_by_agent_id === undefined &&
          artifact.source_execution_id === undefined &&
          artifact.source_operation_id === undefined &&
          artifact.name === input.name &&
          (artifact.description ?? '') === (input.description ?? '') &&
          artifact.object.media_type === input.media_type &&
          artifact.object.byte_size === input.byte_size &&
          (!input.sha256 || artifact.object.sha256 === input.sha256),
      )
      return artifact
    }
    const decode = (
      raw: unknown,
    ):
      | ArtifactUploadResult
      | ArtifactUploadCancellation
      | Readonly<{ artifact: ArtifactMetadata }> => {
      if (action === 'complete')
        return Object.freeze({ artifact: metadata(shape(raw, ['artifact']).artifact) })
      if (action === 'cancel') {
        const result = shape(raw, ['state', 'cleanup'])
        if (result.state === 'not_observed' && result.cleanup === null)
          return Object.freeze({ state: 'not_observed', cleanup: null })
        if (result.state === 'cancel_requested' && result.cleanup === 'pending')
          return Object.freeze({ state: 'cancel_requested', cleanup: 'pending' })
        requireValue(result.state === 'cancelled' && result.cleanup === 'completed')
        return Object.freeze({ state: 'cancelled', cleanup: 'completed' })
      }
      const result = shape(raw, ['state', 'artifact'])
      if (result.state === 'committed')
        return Object.freeze({ state: 'committed', artifact: metadata(result.artifact) })
      requireValue(
        result.artifact === null &&
          (action === 'payload'
            ? result.state === 'uploaded'
            : ['not_observed', 'pending', 'uploaded', 'cancel_requested', 'cancelled'].includes(
                String(result.state),
              )),
      )
      return Object.freeze({
        state: result.state as Exclude<ArtifactUploadResult['state'], 'committed'>,
        artifact: null,
      })
    }
    if (action === 'payload')
      return transport('writeArtifactUploadPayload', decode, {
        ...options,
        projectID: project,
        artifactPayload: { file: file!, mediaType: input.media_type },
      })
    const endpoints = {
      begin: 'beginArtifactUpload',
      lookup: 'lookupArtifactUpload',
      complete: 'completeArtifactUpload',
      cancel: 'cancelArtifactUpload',
    } as const
    return transport(endpoints[action], decode, { ...options, projectID: project, body: input })
  }
  return {
    begin: (project, user, input, options) =>
      request('begin', project, user, input, options) as Promise<ArtifactUploadResult>,
    lookup: (project, user, input, options) =>
      request('lookup', project, user, input, options) as Promise<ArtifactUploadResult>,
    payload: (project, user, input, file, options) =>
      request('payload', project, user, input, options, file) as Promise<ArtifactUploadResult>,
    complete: (project, user, input, options) =>
      request('complete', project, user, input, options) as Promise<
        Readonly<{ artifact: ArtifactMetadata }>
      >,
    cancel: (project, user, input, options) =>
      request('cancel', project, user, input, options) as Promise<ArtifactUploadCancellation>,
  }
}
