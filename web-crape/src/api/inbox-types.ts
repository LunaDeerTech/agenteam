export type InboxStatus = 'open' | 'resolved'
export type InboxSource = Readonly<
  | {
      source_type: 'meeting'
      source_id: string
      action_type: 'meeting.review_proposal'
      source_context: Readonly<Record<string, never>>
    }
  | {
      source_type: 'decision_request'
      source_id: string
      action_type: 'decision.answer'
      source_context: Readonly<{ meeting_id: string }>
    }
>
export type InboxItem = InboxSource &
  Readonly<{
    id: string
    project_id: string
    status: InboxStatus
    blocking: boolean
    priority: number
    dismissible: false
    title: string
    summary: string
    source_version: string
    source_occurred_at: string
    created_at: string
    updated_at: string
    resolved_at: string | null
  }>
export type InboxQuery = Readonly<{ status?: InboxStatus; limit?: number; before?: string }>
export type CapturedInboxQuery = Readonly<{ status: InboxStatus; limit: number; before?: string }>
export type InboxPage = Readonly<{ items: readonly InboxItem[]; next_before: string | null }>
export interface ProjectInboxAPI {
  list(projectID: string, query: InboxQuery, signal: AbortSignal): Promise<InboxPage>
}
