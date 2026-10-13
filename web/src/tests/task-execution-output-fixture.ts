import type { TaskExecution } from '../api/task-executions'
import type { TaskExecutionOutput } from '../api/task-execution-output'
import { at, execution, id, path } from './task-execution-fixture'
export const outputPath = path + '/' + id(100) + '/runtime-view'
export const finalExecution = (patch: Partial<TaskExecution> = {}) =>
  execution(100, {
    status: 'succeeded',
    started_at: at,
    completed_at: at,
    snapshot_id: id(200),
    reason: 'completed',
    ...patch,
  })
export function output(content = '完整文本', row = finalExecution()): TaskExecutionOutput {
  return {
    execution: row,
    availability: 'available',
    snapshot: {
      schema: 'durable_final_text_v1',
      generation: row.snapshot_id!,
      included_update_progress: '1',
      execution_version: row.version,
      items:
        row.status !== 'succeeded'
          ? []
          : [
              {
                id: id(300),
                execution_id: row.id,
                seq: '1',
                type: 'text',
                status: 'completed',
                revision: '1',
                started_at: at,
                updated_at: row.completed_at!,
                completed_at: row.completed_at!,
                source_reference: { kind: 'execution_round', id: id(300) },
                data: { content },
              },
            ],
    },
  }
}
