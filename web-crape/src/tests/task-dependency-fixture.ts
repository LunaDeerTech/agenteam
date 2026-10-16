import type {
  TaskDependencyAddInput,
  TaskDependencyResolveInput,
} from '../api/work-task-dependencies'
import { id, task, receipt as humanReceipt } from './task-human-blocker-fixture'
export {
  id,
  p,
  t,
  at,
  addPath,
  resolvePath,
  lookupPath,
  task,
  json,
  problem,
  session,
} from './task-human-blocker-fixture'
export const related = id(60)
export const draft = () => ({
  expected_version: task().version,
  request: {
    type: 'rely_on' as const,
    description: '  依赖原文\n🧩  ',
    metadata: { related_task_id: related },
  },
})
export const addInput = (): TaskDependencyAddInput => ({
  ...draft(),
  request: { blocker_id: id(5), ...draft().request },
})
export const resolveInput = (): TaskDependencyResolveInput => ({
  expected_version: '9007199254740994',
  request: { blocker_id: id(5), resolution_comment: null },
})
export function receipt(input: TaskDependencyAddInput | TaskDependencyResolveInput = addInput()) {
  const result = humanReceipt(
    'description' in input.request
      ? { ...input, request: { ...input.request, type: 'waiting_for_human', metadata: {} } }
      : { expected_version: input.expected_version, request: input.request },
  )
  return {
    ...result,
    blocker: {
      ...result.blocker,
      type: 'rely_on',
      description:
        'description' in input.request ? input.request.description : draft().request.description,
      metadata: {
        related_task_id:
          'description' in input.request ? input.request.metadata.related_task_id : related,
      },
    },
  }
}
export function candidate(n = 60, state = 'backlog', sprint = id(3)) {
  const { description: _description, plan: _plan, ...summary } = task()
  return { ...summary, id: id(n), title: `Task ${n}`, sprint_id: sprint, state }
}
