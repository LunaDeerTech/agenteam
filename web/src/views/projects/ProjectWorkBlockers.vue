<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiField, UiInput, UiSelect, UiTextarea } from '../../components/ui'
import { useProjectWorkPlanning } from '../../composables/useProjectWorkPlanning'
const work = useProjectWorkPlanning()
const stateLabels = {
  backlog: '待规划',
  todo: '待开始',
  in_progress: '进行中',
  in_review: '待评审',
  blocked: '已阻塞',
  done: '已完成',
  cancelled: '已取消',
}
function cancelResolve() {
  work.blockerDraft.resolvingID = ''
  work.blockerDraft.resolution = ''
}
const status = computed({
  get: () => work.blockerStatus.value,
  set: (value) => {
    void work.setBlockerStatus(value)
  },
})
const targets = computed(() => [
  { value: '', label: '请选择本项目依赖任务', disabled: true },
  ...work.dependencyPage.items
    .filter((task) => task.id !== work.detail.task?.id)
    .map((task) => ({
      value: task.id,
      label: `${task.title} · ${task.id} · ${stateLabels[task.state]}`,
    })),
])
const statuses = [
  { value: 'unresolved', label: '未解除' },
  { value: 'resolved', label: '已解除' },
  { value: 'all', label: '全部状态' },
]
const types = [
  { value: '', label: '请选择阻塞类型', disabled: true },
  { value: 'rely_on', label: '依赖其它任务' },
  { value: 'waiting_for_human', label: '等待人工处理' },
]
</script>
<template>
  <section v-if="work.detail.task" class="work-blockers" aria-label="任务阻塞记录">
    <h2>阻塞记录</h2>
    <p class="muted">记录或解除阻塞原因不会改变任务状态。列表与 Task 当前版本分别读取。</p>
    <div class="actions">
      <UiSelect
        v-model="status"
        label="阻塞记录状态"
        :options="statuses"
        :disabled="work.blocked.value"
      />
      <UiButton :disabled="work.blocked.value" @click="work.loadBlockers('first')"
        >从首页读取阻塞记录</UiButton
      >
    </div>
    <p v-if="work.blockers.phase === 'loading'" role="status">正在读取阻塞记录…</p>
    <p v-if="work.blockers.phase === 'empty'">本页没有符合状态的阻塞记录。</p>
    <p v-if="work.blockers.message" :role="work.blockers.phase === 'error' ? 'alert' : 'status'">
      {{ work.blockers.message }}
    </p>
    <ul class="records">
      <li v-for="blocker in work.blockers.items" :key="blocker.id">
        <h3>
          {{ blocker.type === 'rely_on' ? '依赖其它任务' : '等待人工处理' }} ·
          {{ blocker.resolved_at ? '已解除' : '未解除' }}
        </h3>
        <p class="raw">{{ blocker.description || '未填写说明' }}</p>
        <p v-if="blocker.type === 'rely_on'">
          依赖任务：<RouterLink :to="work.targetPath('task', blocker.metadata.related_task_id)">{{
            blocker.metadata.related_task_id
          }}</RouterLink>
        </p>
        <template v-if="blocker.type === 'rely_on'">
          <p v-if="work.relatedTasks.get(blocker.metadata.related_task_id)?.task">
            {{ work.relatedTasks.get(blocker.metadata.related_task_id)!.task!.title }} ·
            {{ stateLabels[work.relatedTasks.get(blocker.metadata.related_task_id)!.task!.state] }}
          </p>
          <p v-else class="muted">依赖任务的当前标题与状态尚未读取。</p>
          <p v-if="work.relatedTasks.get(blocker.metadata.related_task_id)?.message" role="status">
            {{ work.relatedTasks.get(blocker.metadata.related_task_id)!.message }}
          </p>
          <UiButton
            :disabled="work.blocked.value || work.blockers.stale"
            @click="work.loadRelatedTask(blocker.metadata.related_task_id)"
            >读取依赖任务当前信息</UiButton
          >
        </template>
        <p class="muted">记录 ID：{{ blocker.id }} · 创建于 {{ blocker.created_at }}</p>
        <p v-if="blocker.resolved_at" class="raw">
          解除于 {{ blocker.resolved_at }} ·
          {{ blocker.resolution_comment === null ? '无备注' : blocker.resolution_comment }}
        </p>
        <UiButton
          v-else
          :disabled="!work.canBlock.value || work.blockers.stale"
          @click="work.beginResolve(blocker.id)"
          >解除这条阻塞</UiButton
        >
      </li>
    </ul>
    <div class="actions" aria-label="阻塞记录分页">
      <UiButton
        :disabled="work.blocked.value || work.blockers.invalid || !work.blockers.previous.length"
        @click="work.loadBlockers('previous')"
        >上一页</UiButton
      >
      <span>第 {{ work.blockers.page }} 页</span>
      <UiButton
        :disabled="work.blocked.value || work.blockers.invalid || !work.blockers.next"
        @click="work.loadBlockers('next')"
        >下一页</UiButton
      >
    </div>
    <form
      v-if="work.blockerDraft.resolvingID"
      aria-label="解除阻塞"
      @submit.prevent="work.resolveBlocker"
    >
      <h3>解除阻塞</h3>
      <UiField
        v-slot="field"
        label="解除备注"
        hint="可完全留空；最多 1024 字节，非空备注不能只有空白。"
      >
        <UiTextarea
          :id="field.id"
          v-model="work.blockerDraft.resolution"
          :aria-describedby="field.describedby"
          :disabled="!work.canBlock.value"
          :rows="3"
        />
      </UiField>
      <div class="actions">
        <UiButton type="submit" :disabled="!work.canBlock.value">确认解除</UiButton
        ><UiButton :disabled="work.blocked.value" @click="cancelResolve">取消解除</UiButton>
      </div>
    </form>
    <form
      v-if="work.editableTask.value && !work.readOnly.value"
      aria-label="添加阻塞"
      @submit.prevent="work.addBlocker"
    >
      <h3>添加阻塞</h3>
      <UiField v-slot="field" label="阻塞类型" required
        ><UiSelect
          :id="field.id"
          v-model="work.blockerDraft.type"
          label="阻塞类型"
          :options="types"
          :disabled="!work.canBlock.value"
      /></UiField>
      <template v-if="work.blockerDraft.type === 'rely_on'">
        <UiField v-slot="field" label="查找依赖任务"
          ><UiInput
            :id="field.id"
            v-model="work.dependencyText.value"
            :disabled="!work.canBlock.value"
        /></UiField>
        <div class="actions">
          <UiButton :disabled="!work.canBlock.value" @click="work.loadDependencies('first')"
            >读取依赖候选首页</UiButton
          ><UiButton
            :disabled="
              !work.canBlock.value ||
              work.dependencyPage.invalid ||
              !work.dependencyPage.previous.length
            "
            @click="work.loadDependencies('previous')"
            >上一页候选</UiButton
          ><span>第 {{ work.dependencyPage.page }} 页</span
          ><UiButton
            :disabled="
              !work.canBlock.value || work.dependencyPage.invalid || !work.dependencyPage.next
            "
            @click="work.loadDependencies('next')"
            >下一页候选</UiButton
          >
        </div>
        <p v-if="work.dependencyPage.message" role="status">{{ work.dependencyPage.message }}</p>
        <UiField v-slot="field" label="依赖任务" required
          ><UiSelect
            :id="field.id"
            v-model="work.blockerDraft.relatedTaskID"
            label="依赖任务"
            :options="targets"
            :disabled="!work.canBlock.value"
        /></UiField>
      </template>
      <UiField v-slot="field" label="阻塞说明" hint="可留空，最多 1024 字节。"
        ><UiTextarea
          :id="field.id"
          v-model="work.blockerDraft.description"
          :aria-describedby="field.describedby"
          :disabled="!work.canBlock.value"
          :rows="3"
      /></UiField>
      <UiButton type="submit" variant="primary" :disabled="!work.canBlock.value">添加阻塞</UiButton>
    </form>
    <p v-else class="muted">当前任务或项目只读，仍可查看阻塞历史。</p>
    <p v-if="work.blockerDraft.message" role="alert">{{ work.blockerDraft.message }}</p>
  </section>
</template>
<style scoped>
.work-blockers,
form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.work-blockers {
  border-top: 1px solid var(--border);
  padding-top: var(--space);
}
h2,
h3,
p {
  margin: 0;
  overflow-wrap: anywhere;
}
h2 {
  font-size: 16px;
}
h3 {
  font-size: 14px;
}
.muted {
  color: var(--muted);
  font-size: 13px;
}
.raw {
  white-space: pre-wrap;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
.records {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: var(--space);
}
.records li {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: var(--space);
  min-width: 0;
}
form {
  background: var(--surface-alt);
  border-radius: var(--radius);
  padding: var(--space);
}
</style>
