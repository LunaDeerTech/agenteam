import { AccountFailure, accountTransport, type Fetch, type SkillPackageProgress } from './client'
import { captureSkillID } from './skill-owner'

export type { SkillPackageProgress } from './client'
export interface SkillPackageAPI {
  get(
    projectID: string,
    skillID: string,
    revision: string,
    signal: AbortSignal,
    progress?: SkillPackageProgress,
  ): Promise<Blob>
}
export function captureSkillRevision(value: unknown): string {
  if (
    typeof value !== 'string' ||
    !/^[1-9][0-9]{0,18}$/.test(value) ||
    /[^0-9]/.test(value) ||
    BigInt(value) > 9223372036854775807n
  )
    throw new AccountFailure('invalid-input')
  return value
}
export function createSkillPackageAPI(fetcher?: Fetch): SkillPackageAPI {
  const request = accountTransport(fetcher)
  return {
    get(projectID, skillID, revision, signal, progress) {
      return request(
        'getSkillPackage',
        (value) => {
          if (!(value instanceof Blob)) throw new AccountFailure('invalid-response')
          return value
        },
        {
          signal,
          projectID: captureSkillID(projectID),
          target: captureSkillID(skillID),
          revision: captureSkillRevision(revision),
          ...(progress ? { progress } : {}),
        },
      )
    },
  }
}
