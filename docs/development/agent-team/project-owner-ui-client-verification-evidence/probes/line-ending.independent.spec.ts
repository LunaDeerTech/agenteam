import { describe, expect, it, vi } from 'vitest'
import { captureProjectAddress, captureProjectUpdate, createProjectOwnerAPI } from './src/api/project-owner'
import { safeReturnTarget } from './src/router/auth'
import { project, projectID, ownerID } from './contract-vectors'
import type { Fetch } from './src/api/client'

const endings = [['LF', '\n'], ['CR', '\r'], ['LS', '\u2028'], ['PS', '\u2029']] as const
const response = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })

describe('canonical ASCII inputs must consume the complete string', () => {
  it.each(endings)('rejects raw return path ending in %s', (_name, ending) => {
    expect(safeReturnTarget('/alice/demo' + ending)).toBe('/')
  })
  it.each(endings)('rejects Project name ending in %s before network', (_name, ending) => {
    expect(() => captureProjectUpdate({ expected_version: '1', name: 'demo' + ending })).toThrow()
  })
  it.each(endings)('rejects decimal version ending in %s', async (_name, ending) => {
    const api = createProjectOwnerAPI(async () => response({ ...project(), version: '1' + ending }))
    await expect(api.get(projectID, ownerID, new AbortController().signal)).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each(endings)('rejects address username ending in %s', (_name, ending) => {
    expect(() => captureProjectAddress({ username: 'alice' + ending, project_name: 'demo' })).toThrow()
  })
  it.each(endings)('rejects key ending in %s before dispatch', async (_name, ending) => {
    const fetcher = vi.fn<Fetch>(async () => response(project()))
    const api = createProjectOwnerAPI(fetcher)
    await expect(api.update(projectID, ownerID, { expected_version: '1', name: project().name }, {
      csrfToken: 'Q'.repeat(43), key: 'independent' + ending,
    })).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetcher).not.toHaveBeenCalled()
  })
})
