<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiState from '../../components/ui/UiState.vue'
import { useSystemModels } from '../../composables/useSystemModels'
const page = useSystemModels()
const {
  deletion,
  impact,
  pages,
  replacementProvider,
  state,
  progress,
  blocked,
  locked,
  canDelete,
  feedback,
  confirmation,
} = page
const content = ref<HTMLElement | null>(null)
let alive = true
onBeforeUnmount(() => {
  alive = false
})
const labels: Record<string, string> = {
  'agent/agent_model': 'Agent 主模型',
  'agent/approval_model': 'Agent 审批模型',
  'platform_selector/embedding': '平台 embedding 用途',
  'platform_selector/image': '平台 image 用途',
  'platform_selector/meeting_summary': '系统会议 Summary 模型',
  'platform_selector/memory': '平台 memory 用途',
  'platform_selector/reranker': '平台 reranker 用途',
  'project_summary/meeting_summary': '项目会议摘要模型',
}
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let current: HTMLElement | null = node; current; current = current.parentElement)
    if (current.inert || current.hidden || current.getAttribute('aria-hidden') === 'true')
      return false
  return true
}
async function perform(work: () => Promise<unknown>, event: Event) {
  const previous = event.currentTarget
  await work()
  if (!alive || !content.value?.isConnected) return
  await nextTick()
  if (!alive || !deletion.open || confirmation.open || blocked.value) return
  const target =
    previous instanceof HTMLElement && operable(previous)
      ? previous
      : content.value?.querySelector<HTMLElement>('button:not(:disabled)')
  if (target && operable(target)) target.focus()
}
</script>
<template>
  <UiDialog :open="deletion.open" title="删除 Model" @update:open="!$event && page.closeDelete()">
    <div ref="content" class="model-delete">
      <p>
        待删除：<strong>{{ deletion.model?.input.name }}</strong>
      </p>
      <p class="meta">
        原生 ID：{{ deletion.model?.input.provider_model_id }}；版本：{{ deletion.model?.version }}
      </p>
      <p>
        预览只反映读取时登记的引用。正式删除会重新检查当前权限、引用与替代配置，并在同一事务处理。
      </p>
      <UiState v-if="impact.phase === 'loading'" kind="loading" title="正在读取删除影响" />
      <UiState
        v-else-if="impact.phase !== 'ready'"
        kind="error"
        title="删除影响尚未确认"
        :description="impact.message"
      >
        <UiButton
          :disabled="blocked || progress?.phase === 'uncertain'"
          @click="perform(page.reviewDeletion, $event)"
          >重新读取删除预览</UiButton
        >
      </UiState>
      <template v-else-if="impact.value">
        <p>
          登记引用：{{ impact.value.reference_count }} 条。计数是引用边数，不是去重后的项目或 Agent
          数量。
        </p>
        <ul v-if="impact.value.reference_groups.length">
          <li
            v-for="group in impact.value.reference_groups"
            :key="group.owner_kind + '/' + group.role"
          >
            {{ labels[group.owner_kind + '/' + group.role] }}：{{ group.count }}
          </li>
        </ul>
        <UiState
          v-if="impact.value.delete_blocker"
          kind="error"
          title="当前引用替换能力尚未绑定"
          description="存在外域登记引用，本页不能确认删除；选择替代也不能绕过此限制。"
        />
        <p v-else-if="impact.value.replacement_requirement === 'none'">
          当前未登记引用，本次 replacement 明确为空；正式删除仍会重新检查。
        </p>
        <section v-else class="replacement-picker" aria-label="删除替代选择">
          <h2>选择替代</h2>
          <p v-if="impact.value.replacement_requirement === 'required'">
            当前用途要求明确选择合法替代，不能清空。
          </p>
          <div v-else class="model-actions">
            <UiButton
              :disabled="locked"
              :aria-pressed="deletion.choice === 'clear'"
              @click="perform(page.chooseClear, $event)"
              >清空相应用途引用</UiButton
            >
            <span>或明确选择一个替代 Model。</span>
          </div>
          <p v-if="deletion.choice === 'clear'" role="status">已明确选择清空相应用途引用。</p>
          <p v-if="deletion.replacement" role="status">
            已选择替代：{{ deletion.replacement.input.name }}（{{
              deletion.replacement.input.provider_model_id
            }}）
          </p>
          <template v-if="replacementProvider.phase === 'inactive'">
            <h3>替代 Provider</h3>
            <div class="model-actions" aria-label="替代 Provider 分页">
              <UiButton
                :disabled="locked || !pages.replacementProviders.hasPrevious"
                @click="perform(() => page.pageAction('replacementProviders', 'previous'), $event)"
                >上一页</UiButton
              >
              <UiButton
                :disabled="locked || !pages.replacementProviders.hasNext"
                @click="perform(() => page.pageAction('replacementProviders', 'next'), $event)"
                >下一页</UiButton
              >
            </div>
            <UiState
              v-if="pages.replacementProviders.phase === 'loading'"
              kind="loading"
              title="正在读取替代 Providers"
            />
            <UiState
              v-else-if="pages.replacementProviders.phase === 'error'"
              kind="error"
              title="替代 Provider 列表读取失败"
              :description="pages.replacementProviders.message"
            >
              <UiButton
                :disabled="locked"
                @click="perform(() => page.pageAction('replacementProviders', 'retry'), $event)"
                >{{
                  pages.replacementProviders.cursorInvalid ? '返回首页重新加载' : '重新读取'
                }}</UiButton
              >
            </UiState>
            <UiState
              v-else-if="pages.replacementProviders.phase === 'empty'"
              kind="empty"
              title="当前页没有 Provider"
              description="本页未扫描其他页，不代表全域没有可用替代。"
            />
            <ul v-else-if="pages.replacementProviders.phase === 'ready'" class="choices">
              <li v-for="row in pages.replacementProviders.rows" :key="row.id">
                <span
                  >{{ row.input.name }} · {{ row.input.protocol }} ·
                  {{ row.input.enabled ? '启用' : '禁用' }}</span
                >
                <UiButton
                  :disabled="locked"
                  @click="perform(() => page.selectReplacementProvider(row), $event)"
                  >查看此 Provider 的 Models</UiButton
                >
              </li>
            </ul>
          </template>
          <template v-else>
            <UiButton
              variant="ghost"
              :disabled="locked"
              @click="perform(page.backReplacement, $event)"
              >返回替代 Providers</UiButton
            >
            <UiState
              v-if="replacementProvider.phase === 'loading'"
              kind="loading"
              title="正在读取替代 Provider"
            />
            <UiState
              v-else-if="replacementProvider.phase === 'error'"
              kind="error"
              title="替代 Provider 读取失败"
              :description="replacementProvider.message"
            >
              <UiButton :disabled="locked" @click="perform(page.retryReplacementProvider, $event)"
                >重新读取此 Provider</UiButton
              >
            </UiState>
            <template v-else-if="replacementProvider.value">
              <h3>替代 Models：{{ replacementProvider.value.input.name }}</h3>
              <div class="model-actions" aria-label="替代 Model 分页">
                <UiButton
                  :disabled="locked || !pages.replacementModels.hasPrevious"
                  @click="perform(() => page.pageAction('replacementModels', 'previous'), $event)"
                  >上一页</UiButton
                >
                <UiButton
                  :disabled="locked || !pages.replacementModels.hasNext"
                  @click="perform(() => page.pageAction('replacementModels', 'next'), $event)"
                  >下一页</UiButton
                >
              </div>
              <UiState
                v-if="pages.replacementModels.phase === 'loading'"
                kind="loading"
                title="正在读取替代 Models"
              />
              <UiState
                v-else-if="pages.replacementModels.phase === 'error'"
                kind="error"
                title="替代 Model 列表读取失败"
                :description="pages.replacementModels.message"
              >
                <UiButton
                  :disabled="locked"
                  @click="perform(() => page.pageAction('replacementModels', 'retry'), $event)"
                  >{{
                    pages.replacementModels.cursorInvalid ? '返回首页重新加载' : '重新读取'
                  }}</UiButton
                >
              </UiState>
              <UiState
                v-else-if="pages.replacementModels.phase === 'empty'"
                kind="empty"
                title="当前页没有 Model"
                description="请明确翻页或选择其他 Provider；不会自动扫描全库。"
              />
              <ul v-else-if="pages.replacementModels.phase === 'ready'" class="choices">
                <li v-for="row in pages.replacementModels.rows" :key="row.id">
                  <span
                    >{{ row.input.name }} · {{ row.input.provider_model_id }} ·
                    {{ row.input.type }}</span
                  >
                  <p v-if="page.replacementReason(row)" class="meta">
                    {{ page.replacementReason(row) }}
                  </p>
                  <UiButton
                    :disabled="locked || !!page.replacementReason(row)"
                    :aria-pressed="deletion.replacement?.id === row.id"
                    @click="perform(() => page.selectReplacement(row), $event)"
                    >选择此替代 Model</UiButton
                  >
                </li>
              </ul>
            </template>
          </template>
        </section>
      </template>
      <p v-if="deletion.message" role="status">{{ deletion.message }}</p>
      <UiState
        v-if="progress?.phase === 'uncertain'"
        kind="error"
        title="请求结果未确认"
        :description="state.writeMessage"
      >
        <div class="model-actions">
          <UiButton :disabled="blocked" @click="perform(page.checkOriginal, $event)"
            >检查原请求</UiButton
          >
          <UiButton
            :disabled="blocked || !progress.canRetryOriginal"
            @click="perform(page.retryOriginal, $event)"
            >重试原请求</UiButton
          >
          <UiButton variant="ghost" @click="page.abandonOperation">放弃本次操作</UiButton>
        </div>
      </UiState>
      <p v-else-if="state.writeMessage" role="alert">{{ state.writeMessage }}</p>
      <UiButton
        v-if="progress?.phase === 'rejected'"
        :disabled="blocked"
        @click="perform(page.reviewDeletion, $event)"
        >重新读取并核对删除预览</UiButton
      >
      <div class="model-actions">
        <UiButton
          v-if="impact.phase === 'ready' && impact.value?.delete_blocker === null"
          variant="danger"
          :disabled="!canDelete"
          :state="feedback === 'loading' ? 'loading' : 'idle'"
          @click="perform(page.remove, $event)"
          >确认删除 Model</UiButton
        >
        <UiButton variant="ghost" @click="page.closeDelete">取消</UiButton>
      </div>
    </div>
  </UiDialog>
</template>
<style scoped>
.model-delete,
.replacement-picker {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.model-delete p,
.model-delete li,
.model-delete h2,
.model-delete h3 {
  overflow-wrap: anywhere;
}
.choices {
  display: grid;
  gap: 12px;
  list-style: none;
  margin: 0;
  padding: 0;
  min-width: 0;
}
.choices li {
  display: grid;
  gap: 8px;
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.choices :deep(button),
.model-actions :deep(button) {
  white-space: normal;
  max-width: 100%;
}
.model-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
</style>
