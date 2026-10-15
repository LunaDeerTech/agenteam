import { createProjectInboxAPI } from '../api/project-inbox'
import type { InboxItem, InboxStatus } from '../api/inbox-types'
import { artifactSessionFixture } from './project-artifacts.fixture'
import { usageID, usageJSON, usageTime } from './project-usage.fixture'
export {
  usageID as inboxID,
  usageJSON as inboxJSON,
  usageDeferred as inboxDeferred,
  usageProblem as inboxProblem,
} from './project-usage.fixture'
export const inboxProject = usageID(1),
  inboxPath = '/owner/artifact-demo/inbox'
export const inboxBase = `/api/v1/projects/${inboxProject}/inbox`
export function inboxItem(n = 100, status: InboxStatus = 'open'): InboxItem {
  return {
    id: usageID(n),
    project_id: inboxProject,
    source_type: 'meeting',
    source_context: {},
    source_id: usageID(n + 1000),
    action_type: 'meeting.review_proposal',
    status,
    blocking: false,
    priority: 0,
    dismissible: false,
    title: 'Calibration meeting',
    summary: '<script>literal proposal</script> 讨论时间安排。',
    source_version: '9007199254740993',
    source_occurred_at: usageTime,
    created_at: usageTime,
    updated_at: usageTime,
    resolved_at: status === 'resolved' ? usageTime : null,
  }
}
export const inboxPage = (items = [inboxItem()], next?: string) => ({
  items,
  ...(next ? { next_before: next } : {}),
})
export function inboxFixture() {
  const f = artifactSessionFixture((fetch) => ({ inbox: createProjectInboxAPI(fetch) }))
  const normal: typeof f.normal = (path, init) => {
    const url = new URL(path, 'https://owned.invalid')
    return url.pathname === inboxBase
      ? Promise.resolve(
          usageJSON(
            inboxPage([
              inboxItem(100, url.searchParams.get('status') === 'resolved' ? 'resolved' : 'open'),
            ]),
          ),
        )
      : f.normal(path, init)
  }
  f.intercept(normal)
  return { ...f, normal }
}

export function inboxDecision(n = 200, status: InboxStatus = 'open'): InboxItem {
  return {
    ...inboxItem(n, status),
    source_type: 'decision_request',
    action_type: 'decision.answer',
    source_context: { meeting_id: usageID(n + 2000) },
    blocking: true,
    priority: 1,
    title: '待答决议',
    summary: '选择本次讨论采用的方案。',
  }
}
