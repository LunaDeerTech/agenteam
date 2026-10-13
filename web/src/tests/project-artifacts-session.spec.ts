import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import {
  artifactBase,
  artifactContent,
  artifactDeferred,
  artifactID,
  artifactJSON,
  artifactPage,
  artifactProject,
  artifactRecord,
  artifactSessionFixture,
} from './project-artifacts.fixture'
const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanups.splice(0)) close()
  await flushPromises()
})
describe('Artifact reads through the original Session owner', () => {
  it('captures list filters and full references before dispatch, preserving read-only command isolation', async () => {
    const f = artifactSessionFixture()
    cleanups.push(() => f.auth.leave())
    await f.auth.restore()
    const query = { name_query: 'report-', limit: 50 },
      target = { ...artifactRecord().artifact_ref },
      readQuery = { offset: '0', limit: 8192 }
    const first = f.auth.projectArtifacts.list(artifactProject, query)
    query.name_query = 'mutated'
    await first
    expect(
      new URL(f.fetch.mock.calls.at(-1)![0], 'https://owned.invalid').searchParams.get(
        'name_query',
      ),
    ).toBe('report-')
    const second = f.auth.projectArtifacts.read(target, readQuery)
    target.file_id = artifactID(500)
    readQuery.offset = '9'
    await second
    expect(f.fetch.mock.calls.at(-1)![0]).toBe(
      `${artifactBase}/${artifactID(100)}/files/${artifactID(1100)}/content?offset=0&limit=8192`,
    )
  })
  it('retires visible reads while retaining the real Session busy tail until the transport returns', async () => {
    const f = artifactSessionFixture(),
      held = artifactDeferred<Response>()
    cleanups.push(() => f.auth.leave())
    await f.auth.restore()
    f.intercept((path, init) =>
      path.startsWith(artifactBase) ? held.promise : f.normal(path, init),
    )
    const original = f.auth.projectArtifacts
      .read(artifactRecord().artifact_ref, {})
      .catch((error) => error)
    await flushPromises()
    f.auth.projectArtifacts.abandon()
    expect(await original).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.projectArtifacts.list(artifactProject, {})).rejects.toMatchObject({
      kind: 'busy',
    })
    const count = f.fetch.mock.calls.length
    held.resolve(artifactJSON(artifactContent()))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.fetch.mock.calls).toHaveLength(count)
    f.intercept()
    expect((await f.auth.projectArtifacts.list(artifactProject, {})).items).toHaveLength(1)
  })
  it('invalidates a late result after the original identity leaves without publishing it', async () => {
    const f = artifactSessionFixture(),
      held = artifactDeferred<Response>()
    await f.auth.restore()
    f.intercept((path, init) =>
      path.startsWith(artifactBase) ? held.promise : f.normal(path, init),
    )
    const pending = f.auth.projectArtifacts.list(artifactProject, {}).catch((error) => error)
    await flushPromises()
    f.auth.leave()
    held.resolve(artifactJSON(artifactPage()))
    await flushPromises()
    expect(await pending).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.personalContext.identity).toBeNull()
  })
})
