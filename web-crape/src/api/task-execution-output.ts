import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import { parseSystemInstant } from './system-account'
import { parseTaskExecution, type TaskExecution } from './task-executions'
import { workID } from './work-review'

export type TaskExecutionText = Readonly<{
  id: string
  execution_id: string
  seq: '1'
  type: 'text'
  status: 'completed'
  revision: '1'
  started_at: string
  updated_at: string
  completed_at: string
  source_reference: Readonly<{ kind: 'execution_round'; id: string }>
  data: Readonly<{ content: string }>
}>
export type TaskExecutionOutput = Readonly<{
  execution: TaskExecution
  availability: 'pending' | 'available' | 'not_recorded'
  snapshot: Readonly<{
    schema: 'durable_final_text_v1' | 'durable_final_text_v2'
    generation: string
    included_update_progress: '1'
    execution_version: string
    items: readonly TaskExecutionText[]
  }> | null
}>
export interface TaskExecutionOutputAPI {
  get(
    project: string,
    task: string,
    execution: string,
    signal: AbortSignal,
  ): Promise<TaskExecutionOutput>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function inputID(value: unknown) {
  try {
    return workID(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function parseTaskExecutionOutput(
  value: unknown,
  project: string,
  task: string,
  id: string,
): TaskExecutionOutput {
  const v = shape(value, ['execution', 'availability', 'snapshot'])
  const execution = parseTaskExecution(v.execution, project, task, id)
  const terminal = ['succeeded', 'failed', 'cancelled'].includes(execution.status)
  const availability = v.availability
  requireValue(
    availability === 'pending' || availability === 'available' || availability === 'not_recorded',
  )
  if (availability !== 'available') {
    requireValue(v.snapshot === null && (availability === 'pending' ? !terminal : terminal))
    return Object.freeze({ execution, availability, snapshot: null })
  }
  requireValue(
    terminal &&
      execution.snapshot_id !== null &&
      execution.started_at !== null &&
      execution.reason !== null,
  )
  const executionStarted = execution.started_at
  const snapshot = shape(v.snapshot, [
    'schema',
    'generation',
    'included_update_progress',
    'execution_version',
    'items',
  ])
  const schema = snapshot.schema
  requireValue(
    (schema === 'durable_final_text_v1' || schema === 'durable_final_text_v2') &&
      snapshot.generation === execution.snapshot_id &&
      snapshot.included_update_progress === '1' &&
      snapshot.execution_version === execution.version &&
      Array.isArray(snapshot.items) &&
      snapshot.items.length === (execution.status === 'succeeded' ? 1 : 0),
  )
  const items: TaskExecutionText[] = snapshot.items.map((value) => {
    const item = shape(value, [
      'id',
      'execution_id',
      'seq',
      'type',
      'status',
      'revision',
      'started_at',
      'updated_at',
      'completed_at',
      'source_reference',
      'data',
    ])
    const round = workID(item.id),
      reference = shape(item.source_reference, ['kind', 'id'])
    const data = shape(item.data, ['content']),
      content = data.content
    // Avoid expanding a potentially 16 MiB string into an array of code points.
    requireValue(typeof content === 'string' && content.length <= 16 * 1024 * 1024)
    const started = parseSystemInstant(item.started_at),
      updated = parseSystemInstant(item.updated_at),
      completed = parseSystemInstant(item.completed_at)
    requireValue(
      item.execution_id === execution.id &&
        item.seq === '1' &&
        item.type === 'text' &&
        item.status === 'completed' &&
        item.revision === '1' &&
        reference.kind === 'execution_round' &&
        reference.id === round &&
        (schema === 'durable_final_text_v1'
          ? started === executionStarted
          : started >= executionStarted) &&
        started <= completed &&
        updated === execution.completed_at &&
        completed === execution.completed_at &&
        new TextEncoder().encode(content).byteLength <= 16 * 1024 * 1024 &&
        !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(content),
    )
    const parsed: TaskExecutionText = {
      id: round,
      execution_id: execution.id,
      seq: '1',
      type: 'text',
      status: 'completed',
      revision: '1',
      started_at: started,
      updated_at: updated,
      completed_at: completed,
      source_reference: Object.freeze({ kind: 'execution_round', id: round }),
      data: Object.freeze({ content }),
    }
    return Object.freeze(parsed)
  })
  return Object.freeze({
    execution,
    availability,
    snapshot: Object.freeze({
      schema,
      generation: execution.snapshot_id,
      included_update_progress: '1',
      execution_version: execution.version,
      items: Object.freeze(items),
    }),
  })
}
export function createTaskExecutionOutputAPI(fetcher?: Fetch): TaskExecutionOutputAPI {
  const request = accountTransport(fetcher)
  return {
    async get(projectID, taskID, executionID, signal) {
      const project = inputID(projectID),
        task = inputID(taskID),
        execution = inputID(executionID)
      return request(
        'getTaskExecutionOutput',
        (value) => parseTaskExecutionOutput(value, project, task, execution),
        { signal, projectID: project, target: task, executionID: execution },
      )
    },
  }
}
