import { vi } from 'vitest'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createProjectArtifactsAPI, type ArtifactMetadata } from '../api/project-artifacts'
import { createSessionController } from '../composables/useSession'
import type { Fetch } from '../api/client'
import { usageID, usageJSON, usageTime, usageDeferred, usageProblem } from './project-usage.fixture'
export {
  usageID as artifactID,
  usageJSON as artifactJSON,
  usageDeferred as artifactDeferred,
  usageProblem as artifactProblem,
}
export const artifactProject = usageID(1),
  artifactPath = '/owner/artifact-demo/artifacts'
export const artifactBase = `/api/v1/projects/${artifactProject}/artifacts`
export const artifactText = '<script>literal</script> 中文\n'
export function artifactRecord(n = 100): ArtifactMetadata {
  return {
    artifact_ref: {
      project_id: artifactProject,
      artifact_id: usageID(n),
      file_id: usageID(n + 1000),
    },
    kind: 'generated',
    name: `report-${n}.txt`,
    description: '真实合同的受控前端夹具',
    object: {
      media_type: 'text/plain',
      byte_size: String(new TextEncoder().encode(artifactText).byteLength),
      sha256: 'sha256:' + 'a'.repeat(64),
    },
    created_by_user_id: usageID(2),
    created_at: usageTime,
    version: '9007199254740993',
  }
}
export const artifactPage = (items = [artifactRecord()], next?: string) => ({
  items,
  ...(next ? { next_cursor: next } : {}),
})
export const artifactContent = (
  artifact = artifactRecord(),
  value = artifactText,
  offset = '0',
  truncated = false,
) => ({
  artifact,
  projection: 'text',
  ...(value ? { text: value } : {}),
  offset,
  next_offset: String(BigInt(offset) + BigInt(new TextEncoder().encode(value).byteLength)),
  truncated,
})
export function artifactSessionFixture(commands?: (fetch: Fetch) => NonNullable<Parameters<typeof createSessionController>[19]>) {
  let session: SessionView = {
    user: {
      id: usageID(2),
      email: 'owner@example.test',
      username: 'owner',
      display_name: 'Owner',
      role: 'user',
      theme: 'system',
      version: '1',
      initial_password_suggestion: false,
    },
    session: {
      id: usageID(3),
      issued_at: usageTime,
      idle_expires_at: usageTime,
      absolute_expires_at: usageTime,
    },
    csrf_token: 'S'.repeat(43),
  }
  let project: Project = {
    id: artifactProject,
    owner_user_id: usageID(2),
    name: 'Artifact-Demo',
    normalized_name: 'artifact-demo',
    description: '',
    lifecycle: 'active',
    version: '1',
    current_sprint_id: null,
    created_at: usageTime,
    updated_at: usageTime,
    archived_at: null,
  }
  let intercept: Fetch | undefined
  const normal: Fetch = async (path) => {
    const url = new URL(path, 'https://owned.invalid')
    if (url.pathname === '/api/v1/session') return usageJSON(session)
    if (
      url.pathname === '/api/v1/projects/resolve' ||
      url.pathname === `/api/v1/projects/${project.id}`
    )
      return usageJSON(project)
    if (url.pathname === `/api/v1/projects/${project.id}/artifacts`)
      return usageJSON(
        artifactPage([
          {
            ...artifactRecord(),
            artifact_ref: { ...artifactRecord().artifact_ref, project_id: project.id },
          },
        ]),
      )
    if (url.pathname === artifactBase + `/${usageID(100)}/files/${usageID(1100)}/content`)
      return usageJSON(artifactContent())
    throw new Error('Unexpected Artifact fixture request')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[19] = { projectArtifacts: createProjectArtifactsAPI(fetch), ...commands?.(fetch) }
  return {
    auth: createSessionController(...args),
    fetch,
    normal,
    intercept(value?: Fetch) {
      intercept = value
    },
    session(value: SessionView) {
      session = value
    },
    sessionValue: () => session,
    project(value: Project) {
      project = value
    },
    projectValue: () => project,
  }
}
