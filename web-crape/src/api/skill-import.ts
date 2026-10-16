import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import { captureSkillID } from './skill-owner'
import type { WriteOptions } from './account'

export type SkillImportInput = Readonly<{
  request: Readonly<{
    skill_id: string
    mode: 'create'
    source: Readonly<{
      kind: 'text_files'
      files: readonly [Readonly<{ path: 'SKILL.md'; utf8_text: string }>]
    }>
  }>
}>
export type SkillImportReceipt = Readonly<{ skill_id: string; revision: '1'; version: '1' }>
export type SkillImportFile = Readonly<{ name: 'SKILL.md'; size: number; text: string }>
export interface SkillImportAPI {
  install(
    projectID: string,
    input: SkillImportInput,
    write: WriteOptions,
  ): Promise<SkillImportReceipt>
  lookup(
    projectID: string,
    input: SkillImportInput,
    write: WriteOptions,
  ): Promise<SkillImportReceipt>
}
const encoder = new TextEncoder()
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-input')
}
function sourceText(value: unknown): string {
  requireValue(typeof value === 'string')
  requireValue(value.length > 0 && !/[\u0000\r]/u.test(value) && !value.startsWith('\ufeff'))
  requireValue(
    !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value),
  )
  requireValue(encoder.encode(value).byteLength <= 256 * 1024)
  // Frontmatter and metadata are validated by the service, never rewritten here.
  return value
}
export async function readSkillImportFile(file: File): Promise<SkillImportFile> {
  try {
    requireValue(
      file instanceof File && file.name === 'SKILL.md' && file.size > 0 && file.size <= 256 * 1024,
    )
    const bytes = new Uint8Array(await file.arrayBuffer())
    requireValue(bytes.byteLength === file.size)
    const text = sourceText(
      new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes),
    )
    const encoded = encoder.encode(text)
    requireValue(
      encoded.length === bytes.length && encoded.every((value, index) => value === bytes[index]),
    )
    return Object.freeze({ name: 'SKILL.md', size: bytes.length, text })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureSkillImport(value: unknown): SkillImportInput {
  try {
    const outer = shape(value, ['request']),
      request = shape(outer.request, ['skill_id', 'mode', 'source'])
    const source = shape(request.source, ['kind', 'files'])
    requireValue(request.mode === 'create' && source.kind === 'text_files')
    requireValue(Array.isArray(source.files) && source.files.length === 1)
    const file = shape(source.files[0], ['path', 'utf8_text'])
    requireValue(file.path === 'SKILL.md')
    const result: SkillImportInput = Object.freeze({
      request: Object.freeze({
        skill_id: captureSkillID(request.skill_id),
        mode: 'create',
        source: Object.freeze({
          kind: 'text_files',
          files: Object.freeze([
            Object.freeze({ path: 'SKILL.md', utf8_text: sourceText(file.utf8_text) }),
          ] as const),
        }),
      }),
    })
    requireValue(encoder.encode(JSON.stringify(result)).byteLength <= 1024 * 1024)
    return result
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function createSkillImportAPI(fetcher?: Fetch): SkillImportAPI {
  const request = accountTransport(fetcher)
  function send(
    endpoint: 'installProjectSkill' | 'lookupSkillInstallation',
    projectID: string,
    input: SkillImportInput,
    write: WriteOptions,
  ) {
    const project = captureSkillID(projectID),
      captured = captureSkillImport(input)
    return request(
      endpoint,
      (value) => {
        const result = shape(value, ['skill_id', 'revision', 'version'])
        if (
          string(result.skill_id, 36, 36) !== captured.request.skill_id ||
          result.revision !== '1' ||
          result.version !== '1'
        )
          throw new AccountFailure('invalid-response')
        return Object.freeze({
          skill_id: captured.request.skill_id,
          revision: '1',
          version: '1',
        } as const)
      },
      {
        signal: write.signal ?? new AbortController().signal,
        projectID: project,
        body: captured,
        csrf: write.csrfToken,
        key: write.key,
      },
    )
  }
  return {
    install: (project, input, write) => send('installProjectSkill', project, input, write),
    lookup: (project, input, write) => send('lookupSkillInstallation', project, input, write),
  }
}
