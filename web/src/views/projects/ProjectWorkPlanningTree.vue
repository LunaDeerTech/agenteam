<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { UiButton, UiTree } from '../../components/ui'
import type { TreeNode } from '../../components/ui/types'
import { useProjectWorkPlanning } from '../../composables/useProjectWorkPlanning'
const work = useProjectWorkPlanning(),
  router = useRouter()
const placeholder = (key: string, label: string): TreeNode[] => [
  { id: `note:${key}`, label, disabled: true },
]
const nodes = computed<TreeNode[]>(() =>
  work.milestones.items.map((milestone) => {
    const sprints = work.sprints.get(milestone.id)
    return {
      id: `milestone:${milestone.id}`,
      label: milestone.title,
      disabled: work.blocked.value,
      children:
        !sprints || sprints.phase === 'inactive'
          ? placeholder(milestone.id, '展开以读取 Sprint')
          : sprints.phase === 'loading'
            ? placeholder(milestone.id, '正在读取 Sprint')
            : sprints.items.length
              ? sprints.items.map((sprint) => {
                  const tasks = work.tasks.get(sprint.id)
                  return {
                    id: `sprint:${sprint.id}`,
                    label: sprint.title,
                    disabled: work.blocked.value,
                    children:
                      !tasks || tasks.phase === 'inactive'
                        ? placeholder(sprint.id, '展开以读取 Task')
                        : tasks.phase === 'loading'
                          ? placeholder(sprint.id, '正在读取 Task')
                          : tasks.items.length
                            ? tasks.items.map((task) => ({
                                id: `task:${task.id}`,
                                label: task.title,
                                disabled: work.blocked.value,
                              }))
                            : placeholder(
                                sprint.id,
                                tasks.phase === 'error'
                                  ? '读取失败，请选择 Sprint 重读'
                                  : '本页没有符合条件的 Task',
                              ),
                  }
                })
              : placeholder(
                  milestone.id,
                  sprints.phase === 'error' ? '读取失败，请选择 Milestone 重读' : '本页没有 Sprint',
                ),
    }
  }),
)
const sprintPage = computed(() =>
  work.detail.milestone ? work.sprints.get(work.detail.milestone.id) : undefined,
)
const taskPage = computed(() =>
  work.detail.sprint ? work.tasks.get(work.detail.sprint.id) : undefined,
)
async function select(key: string) {
  if (work.blocked.value) return
  const [kind, id] = key.split(':')
  if (!id || (kind !== 'milestone' && kind !== 'sprint' && kind !== 'task')) return
  const path = work.targetPath(kind, id)
  if (!path) return
  const result = await router.push(path)
  if (!result) work.treeOpen.value = false
}
</script>
<template>
  <section class="work-tree" aria-label="任务规划结构">
    <h2>规划结构</h2>
    <p class="muted">展开读取子项，选择查看详情。Task 列表显示未指派的 backlog。</p>
    <p v-if="work.milestones.phase === 'loading'" role="status">正在读取 Milestone…</p>
    <p v-if="work.milestones.phase === 'empty'">尚无 Milestone，可从详情区新建。</p>
    <p
      v-if="work.milestones.message"
      :role="work.milestones.phase === 'error' ? 'alert' : 'status'"
    >
      {{ work.milestones.message }}
    </p>
    <UiTree
      :nodes="nodes"
      label="Milestone、Sprint 与 Task"
      :selected="work.selected.value"
      :expanded="work.expanded.value"
      @update:selected="select"
      @update:expanded="work.expand"
    />
    <div class="pages" aria-label="Milestone 分页">
      <UiButton :disabled="work.blocked.value" @click="work.loadMilestones('first')"
        >从首页读取 Milestone</UiButton
      >
      <UiButton
        :disabled="
          work.blocked.value || work.milestones.invalid || !work.milestones.previous.length
        "
        @click="work.loadMilestones('previous')"
        >上一页</UiButton
      >
      <span>第 {{ work.milestones.page }} 页</span>
      <UiButton
        :disabled="work.blocked.value || work.milestones.invalid || !work.milestones.next"
        @click="work.loadMilestones('next')"
        >下一页</UiButton
      >
    </div>
    <div v-if="work.detail.milestone" class="pages" aria-label="所选 Milestone 的 Sprint 分页">
      <p>{{ work.detail.milestone.title }} 的 Sprint</p>
      <p v-if="sprintPage?.message" role="status">{{ sprintPage.message }}</p>
      <UiButton :disabled="work.blocked.value" @click="work.loadSprints(work.detail.milestone!.id)"
        >从首页读取 Sprint</UiButton
      >
      <UiButton
        :disabled="
          work.blocked.value || !sprintPage || sprintPage.invalid || !sprintPage.previous.length
        "
        @click="work.loadSprints(work.detail.milestone!.id, 'previous')"
        >上一页</UiButton
      >
      <span>第 {{ sprintPage?.page ?? 1 }} 页</span>
      <UiButton
        :disabled="work.blocked.value || !sprintPage || sprintPage.invalid || !sprintPage.next"
        @click="work.loadSprints(work.detail.milestone!.id, 'next')"
        >下一页</UiButton
      >
    </div>
    <div v-if="work.detail.sprint" class="pages" aria-label="所选 Sprint 的 Task 分页">
      <p>{{ work.detail.sprint.title }} 的 Task</p>
      <p v-if="taskPage?.message" role="status">{{ taskPage.message }}</p>
      <UiButton :disabled="work.blocked.value" @click="work.loadTasks(work.detail.sprint!.id)"
        >从首页读取 Task</UiButton
      >
      <UiButton
        :disabled="work.blocked.value || !taskPage || taskPage.invalid || !taskPage.previous.length"
        @click="work.loadTasks(work.detail.sprint!.id, 'previous')"
        >上一页</UiButton
      >
      <span>第 {{ taskPage?.page ?? 1 }} 页</span>
      <UiButton
        :disabled="work.blocked.value || !taskPage || taskPage.invalid || !taskPage.next"
        @click="work.loadTasks(work.detail.sprint!.id, 'next')"
        >下一页</UiButton
      >
    </div>
  </section>
</template>
<style scoped>
.work-tree {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space);
}
h2 {
  margin: 0;
  font-size: 16px;
}
p {
  margin: 0;
  overflow-wrap: anywhere;
}
.muted {
  color: var(--muted);
  font-size: 13px;
}
.pages {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
  border-top: 1px solid var(--border);
  padding-top: var(--space);
  font-size: 13px;
}
.pages > p {
  flex-basis: 100%;
}
</style>
