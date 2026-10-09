<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiDrawer, UiField, UiInput, UiSelect } from '../../components/ui'
import { useProjectWorkPlanning } from '../../composables/useProjectWorkPlanning'
import ProjectWorkPlanningTree from './ProjectWorkPlanningTree.vue'
import ProjectWorkStructureEditor from './ProjectWorkStructureEditor.vue'
import ProjectWorkTaskEditor from './ProjectWorkTaskEditor.vue'
import ProjectWorkBlockers from './ProjectWorkBlockers.vue'
import ProjectWorkRecovery from './ProjectWorkRecovery.vue'
const work = useProjectWorkPlanning()
function clearFilters() {
  work.filters.text = ''
  work.filters.type = ''
  work.filters.priority = ''
  void work.applyFilters()
}
const object = computed(() => work.detail.task ?? work.detail.sprint ?? work.detail.milestone)
const outsidePage = computed(() => {
  const address = work.address.value
  if (!address?.id || !object.value) return false
  if (address.kind === 'milestone')
    return !work.milestones.items.some((row) => row.id === address.id)
  if (address.kind === 'sprint')
    return !work.sprints.get(work.detail.milestone!.id)?.items.some((row) => row.id === address.id)
  return !work.tasks.get(work.detail.sprint!.id)?.items.some((row) => row.id === address.id)
})
const types = [
  { value: '', label: '所有类型' },
  { value: 'feature', label: '功能' },
  { value: 'bug', label: '缺陷' },
  { value: 'task', label: '任务' },
  { value: 'spike', label: '调研' },
  { value: 'chore', label: '维护' },
]
const priorities = [
  { value: '', label: '所有优先级' },
  { value: 'critical', label: '紧急' },
  { value: 'high', label: '高' },
  { value: 'medium', label: '中' },
  { value: 'low', label: '低' },
]
const taskStates = {
  backlog: '待规划',
  todo: '待开始',
  in_progress: '进行中',
  in_review: '待评审',
  blocked: '已阻塞',
  done: '已完成',
  cancelled: '已取消',
}
const sprintStates = { planned: '已规划', current: '当前', completed: '已完成' }
const reorderOptions = computed(() => [
  { value: '', label: '请选择当前同组页中的对象', disabled: true },
  ...work.reorderPage.items
    .filter((row) => row.id !== work.editor.targetID)
    .map((row) => ({ value: row.id, label: `${row.title} · ${row.id}` })),
])
</script>
<template>
  <section v-if="work.visible.value" class="work-planning" aria-labelledby="work-planning-title">
    <header class="work-heading">
      <div>
        <h1 id="work-planning-title" tabindex="-1">任务规划</h1>
        <p class="muted">按 Milestone、Sprint 和 Task 组织计划。</p>
      </div>
      <UiButton
        class="tree-toggle"
        :disabled="work.blocked.value"
        @click="work.treeOpen.value = true"
        >打开规划树</UiButton
      >
    </header>
    <p v-if="work.readOnly.value" role="status">
      项目当前只读；可查看内容、查证及重放此前原命令，不能发起新的修改。
    </p>
    <div class="work-columns">
      <aside class="desktop-tree"><ProjectWorkPlanningTree /></aside>
      <main class="work-detail" aria-label="规划详情">
        <nav v-if="work.detail.milestone" class="breadcrumbs" aria-label="规划层级">
          <RouterLink :to="work.targetPath('milestone', work.detail.milestone.id)">{{
            work.detail.milestone.title
          }}</RouterLink>
          <template v-if="work.detail.sprint"
            ><span aria-hidden="true">/</span
            ><RouterLink :to="work.targetPath('sprint', work.detail.sprint.id)">{{
              work.detail.sprint.title
            }}</RouterLink></template
          >
          <template v-if="work.detail.task"
            ><span aria-hidden="true">/</span
            ><span aria-current="page">{{ work.detail.task.title }}</span></template
          >
        </nav>
        <div class="actions" aria-label="新建规划对象">
          <UiButton
            :disabled="
              work.blocked.value ||
              work.readOnly.value ||
              work.progress.value?.phase === 'uncertain'
            "
            @click="work.beginCreate('milestone')"
            >新建 Milestone</UiButton
          >
          <UiButton
            v-if="work.detail.milestone"
            :disabled="
              work.blocked.value ||
              work.readOnly.value ||
              work.progress.value?.phase === 'uncertain'
            "
            @click="work.beginCreate('sprint')"
            >新建 Sprint</UiButton
          >
          <UiButton
            v-if="work.detail.sprint"
            :disabled="
              work.blocked.value ||
              work.readOnly.value ||
              work.detail.sprint.state === 'completed' ||
              work.progress.value?.phase === 'uncertain'
            "
            @click="work.beginCreate('task')"
            >新建 Task</UiButton
          >
        </div>
        <p v-if="work.detail.phase === 'loading'" role="status">正在读取所选对象与父级…</p>
        <p v-if="work.detail.message" :role="work.detail.phase === 'error' ? 'alert' : 'status'">
          {{ work.detail.message }}
        </p>
        <UiButton
          v-if="work.detail.phase === 'error'"
          :disabled="work.blocked.value"
          @click="work.loadSelection"
          >重新读取所选对象</UiButton
        >
        <p v-if="outsidePage" class="muted">
          所选对象不在已加载的树页中。这里显示直接读取的详情与父级；树中的位置尚未加载。
        </p>
        <section v-if="object" class="current-value" aria-label="当前读取内容">
          <h2>当前内容：{{ object.title }}</h2>
          <p class="raw">{{ object.description || '未填写描述' }}</p>
          <dl>
            <dt>对象 ID</dt>
            <dd>{{ object.id }}</dd>
            <dt>当前版本</dt>
            <dd>{{ object.version }}</dd>
            <dt>创建时间</dt>
            <dd>{{ object.created_at }}</dd>
            <dt>更新时间</dt>
            <dd>{{ object.updated_at }}</dd>
            <template v-if="work.detail.task"
              ><dt>状态</dt>
              <dd>{{ taskStates[work.detail.task.state] }}</dd>
              <dt>类型</dt>
              <dd>{{ types.find((option) => option.value === work.detail.task!.type)?.label }}</dd>
              <dt>优先级</dt>
              <dd>
                {{
                  priorities.find((option) => option.value === work.detail.task!.priority)?.label
                }}
              </dd></template
            >
            <template v-else-if="work.detail.sprint"
              ><dt>Sprint 状态</dt>
              <dd>{{ sprintStates[work.detail.sprint.state] }}</dd>
              <dt>开始时间</dt>
              <dd>{{ work.detail.sprint.started_at ?? '尚未开始' }}</dd>
              <dt>完成时间</dt>
              <dd>{{ work.detail.sprint.completed_at ?? '尚未完成' }}</dd></template
            >
          </dl>
          <template v-if="work.detail.task"
            ><h3>当前 Plan</h3>
            <p class="raw">{{ work.detail.task.plan || '尚未填写 Plan' }}</p>
            <p v-if="!work.editableTask.value" class="muted">
              本界面仅编辑未指派的 backlog Task；该任务仍可只读查看。
            </p></template
          >
          <p v-if="work.detail.phase !== 'ready'" class="muted">
            以上是上次读取内容，当前读取尚未成功。
          </p>
        </section>
        <ProjectWorkTaskEditor
          v-if="work.editor.open && work.editor.kind === 'task'"
          v-model:title="work.draft.title"
          v-model:description="work.draft.description"
          v-model:type="work.draft.type"
          v-model:priority="work.draft.priority"
          v-model:plan="work.draft.plan"
          :creating="work.editor.creating"
          :disabled="work.blocked.value"
          :read-only="work.editorReadOnly.value"
          :can-save="work.canSave.value"
          :can-read="!work.blocked.value"
          :can-adopt="work.canAdopt.value"
          :conflict="work.editor.conflict"
          :feedback="work.editor.feedback"
          :message="work.editor.message"
          :errors="work.editor.errors"
          @save="work.save"
          @cancel="work.cancelCreate"
          @read-current="work.loadSelection"
          @adopt-current="work.adoptCurrent"
        />
        <ProjectWorkStructureEditor
          v-else-if="work.editor.open"
          v-model:title="work.draft.title"
          v-model:description="work.draft.description"
          :kind="work.editor.kind === 'sprint' ? 'sprint' : 'milestone'"
          :creating="work.editor.creating"
          :disabled="work.blocked.value"
          :read-only="work.editorReadOnly.value"
          :can-save="work.canSave.value"
          :can-read="!work.blocked.value"
          :can-adopt="work.canAdopt.value"
          :conflict="work.editor.conflict"
          :feedback="work.editor.feedback"
          :message="work.editor.message"
          :errors="work.editor.errors"
          @save="work.save"
          @cancel="work.cancelCreate"
          @read-current="work.loadSelection"
          @adopt-current="work.adoptCurrent"
        />
        <section
          v-if="work.editor.open && !work.editor.creating"
          class="reorder"
          aria-label="同组排序"
        >
          <UiButton
            :disabled="work.blocked.value || work.editorReadOnly.value || work.changed.value"
            @click="work.openReorder"
            >调整同组顺序</UiButton
          >
          <template v-if="work.reorder.open">
            <p class="muted">
              候选来自无文本或类型筛选的同组分页。选中对象之前，或明确移动到组尾。
            </p>
            <p v-if="work.reorderPage.message" role="status">{{ work.reorderPage.message }}</p>
            <UiSelect
              v-model="work.reorder.beforeID"
              label="移到哪个对象之前"
              :options="reorderOptions"
              :disabled="work.blocked.value || work.reorderPage.invalid"
            />
            <div class="actions">
              <UiButton :disabled="work.blocked.value" @click="work.loadReorder('first')"
                >从首页读取同组候选</UiButton
              ><UiButton
                :disabled="
                  work.blocked.value ||
                  work.reorderPage.invalid ||
                  !work.reorderPage.previous.length
                "
                @click="work.loadReorder('previous')"
                >上一页候选</UiButton
              ><span>第 {{ work.reorderPage.page }} 页</span
              ><UiButton
                :disabled="work.blocked.value || work.reorderPage.invalid || !work.reorderPage.next"
                @click="work.loadReorder('next')"
                >下一页候选</UiButton
              >
            </div>
            <p v-if="work.reorder.message" role="alert">{{ work.reorder.message }}</p>
            <div class="actions">
              <UiButton
                :disabled="
                  work.blocked.value || work.editorReadOnly.value || !work.reorder.beforeID
                "
                @click="work.saveReorder(false)"
                >移到所选对象之前</UiButton
              ><UiButton
                :disabled="work.blocked.value || work.editorReadOnly.value"
                @click="work.saveReorder(true)"
                >移到组尾</UiButton
              ><UiButton :disabled="work.blocked.value" @click="work.reorder.open = false"
                >关闭排序</UiButton
              >
            </div>
          </template>
        </section>
        <ProjectWorkRecovery />
        <ProjectWorkBlockers />
        <form
          v-if="work.detail.sprint"
          class="filters"
          aria-label="Task 筛选"
          @submit.prevent="work.applyFilters"
        >
          <h2>筛选 Sprint 中的 Task</h2>
          <UiField v-slot="field" label="标题或描述"
            ><UiInput :id="field.id" v-model="work.filters.text" :disabled="work.blocked.value"
          /></UiField>
          <div class="actions">
            <UiSelect
              v-model="work.filters.type"
              label="筛选 Task 类型"
              :options="types"
              :disabled="work.blocked.value"
            /><UiSelect
              v-model="work.filters.priority"
              label="筛选 Task 优先级"
              :options="priorities"
              :disabled="work.blocked.value"
            /><UiButton type="submit" :disabled="work.blocked.value">从首页筛选</UiButton
            ><UiButton :disabled="work.blocked.value" @click="clearFilters">清除筛选</UiButton>
          </div>
        </form>
      </main>
    </div>
    <UiDrawer v-model:open="work.treeOpen.value" title="规划结构"
      ><ProjectWorkPlanningTree
    /></UiDrawer>
  </section>
  <p v-else role="status">正在确认项目读取资格。</p>
</template>
<style scoped>
.work-planning {
  min-width: 0;
  padding: var(--space);
  display: flex;
  flex-direction: column;
  gap: var(--space);
}
.work-heading,
.actions,
.breadcrumbs {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.work-heading {
  justify-content: space-between;
}
h1 {
  font-size: 20px;
  margin: 0;
}
h2 {
  font-size: 16px;
  margin: 0;
}
h3 {
  font-size: 14px;
  margin: 0;
}
p {
  margin: 0;
  overflow-wrap: anywhere;
}
.muted {
  color: var(--muted);
  font-size: 13px;
}
.raw {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.work-columns {
  display: grid;
  grid-template-columns: minmax(220px, 30%) minmax(0, 1fr);
  gap: var(--space);
  align-items: start;
}
.desktop-tree {
  padding-right: var(--space);
  border-right: 1px solid var(--border);
  min-width: 0;
}
.work-detail,
.reorder,
.filters,
.current-value {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space);
}
.current-value {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: var(--space);
}
.current-value dl {
  display: grid;
  grid-template-columns: minmax(88px, auto) minmax(0, 1fr);
  gap: var(--content-gap);
  margin: 0;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.breadcrumbs {
  font-size: 13px;
  overflow-wrap: anywhere;
}
.filters,
.reorder {
  padding-top: var(--space);
  border-top: 1px solid var(--border);
}
.tree-toggle {
  display: none;
}
@media (max-width: 767px) {
  .work-columns {
    grid-template-columns: minmax(0, 1fr);
  }
  .desktop-tree {
    display: none;
  }
  .tree-toggle {
    display: inline-flex;
  }
}
</style>
