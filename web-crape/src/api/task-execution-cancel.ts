import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import { parseTaskExecution, type TaskExecution } from './task-executions'
import { workID, workVersion } from './work-review'

export type TaskExecutionCancelInput = Readonly<{
  expected_version: string
  expected_task_version: string
  reason?: string
}>
export type TaskExecutionCancelReceipt = Readonly<{
  execution: TaskExecution
  task: Readonly<{ id: string; version: string; state: 'blocked'; blocker_id: string }>
}>
export type TaskExecutionCancelResult = Readonly<{
  receipt: TaskExecutionCancelReceipt
  replayed: boolean
}>
export type TaskExecutionCancelLookup =
  | Readonly<{ found: true; receipt: TaskExecutionCancelReceipt }>
  | Readonly<{ found: false; receipt: null }>
export interface TaskExecutionCancelAPI {
  cancel(
    project: string,
    task: string,
    execution: string,
    input: TaskExecutionCancelInput,
    write: WriteOptions,
  ): Promise<TaskExecutionCancelResult>
  lookup(
    project: string,
    task: string,
    execution: string,
    input: TaskExecutionCancelInput,
    write: WriteOptions,
  ): Promise<TaskExecutionCancelLookup>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(read: () => T): T {
  try {
    return read()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureTaskExecutionCancelInput(value: unknown): TaskExecutionCancelInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'expected_task_version'], ['reason'])
    let reason: string | undefined
    if (Object.hasOwn(v, 'reason')) {
      reason = string(v.reason, 1, 1024)
      requireValue(
        new TextEncoder().encode(reason).byteLength <= 1024 &&
          !reason.includes('\0') &&
          !/^\p{White_Space}*$/u.test(reason) &&
          !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(reason),
      )
    }
    return Object.freeze({
      expected_version: workVersion(v.expected_version),
      expected_task_version: workVersion(v.expected_task_version),
      ...(reason === undefined ? {} : { reason }),
    })
  })
}
function parseReceipt(
  value: unknown,
  project: string,
  taskID: string,
  executionID: string,
  original: TaskExecutionCancelInput,
): TaskExecutionCancelReceipt {
  const v = shape(value, ['execution', 'task']),
    execution = parseTaskExecution(v.execution, project, taskID, executionID),
    task = shape(v.task, ['id', 'version', 'state', 'blocker_id'])
  const id = workID(task.id),
    version = workVersion(task.version),
    blocker = workID(task.blocker_id)
  requireValue(
    id === taskID &&
      task.state === 'blocked' &&
      BigInt(version) === BigInt(original.expected_task_version) + 1n &&
      BigInt(execution.version) === BigInt(original.expected_version) + 1n &&
      execution.cancel_requested_at !== null &&
      !['succeeded', 'failed', 'cancelled'].includes(execution.status),
  )
  return Object.freeze({
    execution,
    task: Object.freeze({ id, version, state: 'blocked', blocker_id: blocker }),
  })
}
export function createTaskExecutionCancelAPI(fetcher?: Fetch): TaskExecutionCancelAPI {
  const call = accountTransport(fetcher)
  function capture(
    project: string,
    task: string,
    execution: string,
    value: TaskExecutionCancelInput,
    write: WriteOptions,
  ) {
    const original = captureTaskExecutionCancelInput(value)
    const scope = input(() => ({
      projectID: workID(project),
      target: workID(task),
      executionID: workID(execution),
    }))
    return {
      original,
      scope,
      options: {
        ...scope,
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      },
    }
  }
  return {
    cancel(project, task, execution, value, write) {
      const { original, scope, options } = capture(project, task, execution, value, write)
      return call(
        'cancelTaskExecution',
        (value) => {
          const v = shape(value, ['receipt', 'replayed'])
          requireValue(typeof v.replayed === 'boolean')
          return Object.freeze({
            receipt: parseReceipt(
              v.receipt,
              scope.projectID,
              scope.target,
              scope.executionID,
              original,
            ),
            replayed: v.replayed,
          })
        },
        options,
      )
    },
    lookup(project, task, execution, value, write) {
      const { original, scope, options } = capture(project, task, execution, value, write)
      return call(
        'lookupTaskExecutionCancel',
        (value): TaskExecutionCancelLookup => {
          const v = shape(value, ['found', 'receipt'])
          if (v.found === true)
            return Object.freeze({
              found: true,
              receipt: parseReceipt(
                v.receipt,
                scope.projectID,
                scope.target,
                scope.executionID,
                original,
              ),
            })
          requireValue(v.found === false && v.receipt === null)
          return Object.freeze({ found: false, receipt: null })
        },
        options,
      )
    },
  }
}
