<script setup lang="ts">
import { computed, ref } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiTable from '../../components/ui/UiTable.vue'
import { useProjectVariables } from '../../composables/useProjectVariables'
import ProjectVariableEditor from './ProjectVariableEditor.vue'
const owner = useProjectVariables()
const {
  visible,
  page,
  blocked,
  canMutate,
  currentProject,
  progress,
  canLookup,
  canReplay,
  message,
} = owner
const heading = ref<HTMLElement | null>(null)
const rows = computed(() => page.items.map((row) => ({ ...row })))
const columns = [
  { key: 'name', label: '名称' },
  { key: 'description', label: '描述' },
  { key: 'version', label: '版本' },
  { key: 'updated_at', label: '更新时间' },
  { key: 'action', label: '操作' },
]
const phase = computed(
  () =>
    ({
      submitting: '正在提交',
      uncertain: '结果尚未确认',
      rejected: '本次请求被明确拒绝',
      confirmed: '原操作已确认',
    })[progress.value?.phase ?? 'submitting'],
)
</script>
<template>
  <section class="project-variables" aria-labelledby="project-variables-title">
    <header>
      <h1 id="project-variables-title" ref="heading" tabindex="-1">Variables</h1>
      <p>管理本项目的普通变量。此处不提供 Secret，也不会向 Agent 或运行环境自动注入。</p>
    </header>
    <div v-if="!visible" role="status" class="notice">
      <p>当前项目和身份尚未确认，变量内容已隐藏。同一会话内暂时检查身份会保留草稿。</p>
      <UiButton :disabled="owner.sessionBusy.value" @click="owner.readOwner()"
        >重新读取项目</UiButton
      >
    </div>
    <template v-else>
      <p v-if="!canMutate" role="status" class="notice">
        项目当前只读（{{ currentProject?.lifecycle }}）。仍可读取变量及查询、重放已完成的原操作。
      </p>
      <p class="meta">
        恢复材料仅保存在当前会话内存中。刷新、关闭页面或实际更换会话后不能在此恢复原请求。
      </p>
      <div class="actions">
        <UiButton variant="primary" :disabled="blocked || !canMutate" @click="owner.createNew()"
          >新建变量</UiButton
        >
        <UiButton :disabled="blocked" @click="owner.firstPage()">从第一页重新读取</UiButton>
        <UiButton :disabled="blocked || page.cursorInvalid" @click="owner.reloadPage()"
          >重读本页</UiButton
        >
      </div>
      <p v-if="message" class="notice" role="status">{{ message }}</p>
      <p v-if="page.stale" class="notice" role="status">
        当前目录为旧观察，请明确重读；不会自动跳页或改变顺序。
      </p>
      <p v-if="page.message" class="notice" role="status">{{ page.message }}</p>
      <p v-if="page.phase === 'loading'" role="status">正在读取变量目录…</p>
      <UiTable :columns="columns" :rows="rows" caption="普通变量目录（摘要不含值）">
        <template #cell-description="{ value }"
          ><span class="summary-description">{{ value || '（空）' }}</span></template
        >
        <template #cell-action="{ row }"
          ><UiButton
            :disabled="blocked"
            :aria-label="`查看变量 ${row.name}`"
            @click="owner.select(String(row.id))"
            >查看</UiButton
          ></template
        >
      </UiTable>
      <nav class="actions" aria-label="变量分页">
        <UiButton
          :disabled="blocked || !page.hasPrevious || page.cursorInvalid"
          @click="owner.previousPage()"
          >上一页</UiButton
        >
        <span>第 {{ page.page }} 页 · 每页50项</span>
        <UiButton
          :disabled="blocked || !page.hasNext || page.cursorInvalid"
          @click="owner.nextPage()"
          >下一页</UiButton
        >
      </nav>
      <section v-if="progress" class="notice history" aria-label="原操作与历史回执">
        <h2>原操作与历史回执</h2>
        <p>{{ phase }} · {{ progress.kind }} · {{ progress.targetID }}</p>
        <p v-if="progress.keyConflict">原请求标识冲突，禁止重放；不会自动生成新的标识。</p>
        <p v-if="progress.observation === 'not_observed'">
          本次未观察到原操作，不证明原操作没有提交。
        </p>
        <p v-else-if="progress.observation === 'in_progress'">
          已观察到原意图，未取得完成回执。不会自动重放。
        </p>
        <p v-else-if="progress.observation === 'failed'">
          本次查询失败，原操作状态及已确认历史保持。
        </p>
        <div v-if="progress.receipt" aria-label="已确认历史回执">
          <template v-if="'variable' in progress.receipt"
            ><p>
              历史名称：{{ progress.receipt.variable.name }} · 版本
              {{ progress.receipt.variable.version }}
            </p>
            <pre>{{ progress.receipt.variable.value }}</pre>
          </template>
          <p v-else>原删除已确认 · 墓碑版本 {{ progress.receipt.deleted.version }}</p>
          <p>这是历史执行结果，不代表当前变量仍然存在或仍为相同内容。</p>
        </div>
        <div class="actions">
          <UiButton :disabled="!canLookup" @click="owner.lookupOriginal()">查询原操作</UiButton>
          <UiButton :disabled="!canReplay" @click="owner.replayOriginal()">重放原操作</UiButton>
          <UiButton
            v-if="progress.phase === 'rejected'"
            :disabled="blocked || progress.keyConflict"
            @click="owner.editRejected()"
            >返回编辑</UiButton
          >
          <UiButton :disabled="blocked" @click="owner.readOriginalTarget()"
            >读取原对象当前信息</UiButton
          >
          <UiButton :disabled="blocked" @click="owner.abandonPending()">结束本地追踪</UiButton>
        </div>
      </section>
      <ProjectVariableEditor />
    </template>
  </section>
</template>
<style scoped>
.project-variables {
  display: grid;
  gap: var(--space);
  min-width: 0;
}
.actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.summary-description {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  display: inline-block;
  max-width: 24rem;
}
.notice,
.history {
  overflow-wrap: anywhere;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 18rem;
  overflow: auto;
}
@media (max-width: 600px) {
  .actions {
    align-items: stretch;
  }
}
</style>
