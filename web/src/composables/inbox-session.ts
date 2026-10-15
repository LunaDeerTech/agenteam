import { captureInboxProject, captureInboxQuery } from '../api/project-inbox'
import type { InboxQuery, ProjectInboxAPI } from '../api/inbox-types'
import type { PersonalIdentity } from './useSession'

// This facade borrows the original Session's request owner. It owns no Cookie,
// authentication material, request tail, source command or review decision.
export function createInboxSession(
  api: ProjectInboxAPI,
  ports: {
    identity(): PersonalIdentity
    run<T>(identity: PersonalIdentity, work: (signal: AbortSignal) => Promise<T>): Promise<T>
    cancelRead(): void
  },
) {
  return {
    list(projectID: string, query: InboxQuery) {
      const project = captureInboxProject(projectID),
        captured = captureInboxQuery(query)
      return ports.run(ports.identity(), (signal) => api.list(project, captured, signal))
    },
    abandonRead: ports.cancelRead,
  }
}
export type InboxSession = ReturnType<typeof createInboxSession>
