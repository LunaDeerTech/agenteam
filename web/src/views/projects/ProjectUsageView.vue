<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { UiButton, UiCard, UiState } from '../../components/ui'
import { usageTokenFields, type ProjectUsageRecord } from '../../api/project-usage'
import { useProjectUsage } from '../../composables/useProjectUsage'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { skillsNavigationReadReady } from '../../router/auth'

const route = useRoute(),
  navigationReady = skillsNavigationReadReady(useRouter())
const location = computed(() => (navigationReady.value ? route.fullPath : ''))
const owner = useProjectUsage(undefined, useProjectWorkspace(), location),
  page = owner.page
const detail = owner.selected,
  detailHeading = ref<HTMLElement | null>(null),
  pageHeading = ref<HTMLElement | null>(null)
let selectionTrigger: HTMLElement | null = null
const consumers = {
  agent: 'Agent',
  meeting: '会议',
  knowledge: '知识库',
  memory: '记忆',
  tool: '工具',
}
const purposes = {
  agent_generation: 'Agent 生成',
  agent_compaction: 'Agent 上下文压缩',
  approval_auto: '自动审批',
  meeting_summary_initial: '会议初始摘要',
  meeting_summary_update: '会议摘要更新',
  knowledge_embedding: '知识向量化',
  memory_embedding: '记忆向量化',
  memory_extraction: '记忆提取',
  memory_consolidation: '记忆整合',
  memory_reflection: '记忆反思',
  rerank: '重排',
  image_generation: '图片生成',
}
const dispatches = {
  reserved: '已登记',
  authorized: '已授权',
  sent: '已派发',
  not_sent: '未派发',
  unknown: '派发情况未知',
}
const finals = { succeeded: '成功', failed: '失败', cancelled: '已取消', unknown: '结果未知' }
const tokenLabels = {
  input_tokens: '输入 Token',
  output_tokens: '输出 Token',
  total_tokens: '总 Token',
  cached_input_tokens: '缓存命中输入 Token',
  cache_write_tokens: '缓存写入 Token',
  reasoning_tokens: '推理 Token',
}
const resultLabel = (item: ProjectUsageRecord) =>
  item.final ? finals[item.final.status] : '尚无最终记录'
async function select(id: string, event: MouseEvent) {
  if (owner.blocked.value) return
  const trigger = event.currentTarget as HTMLElement
  selectionTrigger = trigger
  owner.select(id)
  await nextTick()
  if (owner.visible.value && detail.value?.id === id && selectionTrigger === trigger)
    detailHeading.value?.focus()
}
async function closeDetail() {
  owner.closeDetail()
  await nextTick()
  if (selectionTrigger?.isConnected) selectionTrigger.focus()
  selectionTrigger = null
}
async function changePage(direction: 'next' | 'previous') {
  owner[direction]()
  await nextTick()
  if (owner.visible.value) pageHeading.value?.focus()
}
onBeforeRouteLeave(() => owner.cancel())
onBeforeRouteUpdate((to, from) => {
  if (to.fullPath !== from.fullPath) owner.cancel()
})
onBeforeUnmount(() => owner.dispose())
</script>

<template>
  <section class="usage-page" aria-label="项目模型调用记录">
    <header class="usage-header">
      <div>
        <h1>调用记录</h1>
        <p>查看近 30 天的模型调用。名称按调用时记录显示，未知用量不会记为零。</p>
      </div>
      <div class="usage-actions">
        <UiButton :disabled="owner.blocked.value" @click="owner.refresh()">刷新调用记录</UiButton>
        <UiButton v-if="owner.reading.value" variant="ghost" @click="owner.cancel()"
          >停止读取</UiButton
        >
      </div>
    </header>
    <p v-if="owner.visible.value && owner.window.value" class="usage-window">
      查询时间（UTC）：<time :datetime="owner.window.value.from">{{
        owner.window.value.from
      }}</time>
      至 <time :datetime="owner.window.value.to">{{ owner.window.value.to }}</time
      >（不含结束时间）。返回上一页会重新读取该页。
    </p>
    <h2 ref="pageHeading" tabindex="-1">调用明细</h2>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认调用记录访问身份" />
    <UiState
      v-else-if="page.phase === 'waiting' || page.phase === 'loading'"
      kind="loading"
      title="正在读取调用记录"
    />
    <UiState
      v-else-if="page.phase === 'error' || page.phase === 'unavailable'"
      kind="error"
      title="调用记录暂不可用"
      :description="page.message"
    />
    <template v-else>
      <p v-if="page.items.length === 0" role="status">本次时间范围内没有调用记录。</p>
      <div class="usage-content">
        <div>
          <ul class="usage-list" aria-label="调用记录列表">
            <li v-for="item in page.items" :key="item.id">
              <UiButton
                class="usage-select"
                :aria-pressed="detail?.id === item.id"
                :disabled="owner.blocked.value"
                @click="select(item.id, $event)"
              >
                {{ item.model_name }} · {{ purposes[item.purpose] }} · 第
                {{ item.attempt_index }} 次尝试
              </UiButton>
              <p>
                {{ item.provider_name }} · {{ consumers[item.consumer_kind] }} ·
                {{ dispatches[item.dispatch] }} · {{ resultLabel(item) }}
              </p>
              <p>
                <time :datetime="item.started_at">{{ item.started_at }}</time>
              </p>
              <dl class="usage-tokens">
                <div>
                  <dt>输入 Token</dt>
                  <dd>{{ item.usage.input_tokens ?? '未知' }}</dd>
                </div>
                <div>
                  <dt>输出 Token</dt>
                  <dd>{{ item.usage.output_tokens ?? '未知' }}</dd>
                </div>
                <div>
                  <dt>总 Token</dt>
                  <dd>{{ item.usage.total_tokens ?? '未知' }}</dd>
                </div>
              </dl>
            </li>
          </ul>
          <nav class="usage-actions usage-pagination" aria-label="调用记录分页">
            <UiButton
              :disabled="owner.blocked.value || page.index === 0"
              @click="changePage('previous')"
              >上一页</UiButton
            >
            <span aria-current="page">第 {{ page.index + 1 }} 页</span>
            <UiButton
              :disabled="owner.blocked.value || page.nextCursor === null"
              @click="changePage('next')"
              >下一页</UiButton
            >
          </nav>
        </div>
        <UiCard v-if="detail" class="usage-detail">
          <h2 ref="detailHeading" tabindex="-1">{{ detail.model_name }} 调用详情</h2>
          <UiButton variant="ghost" :disabled="owner.blocked.value" @click="closeDetail"
            >返回调用列表</UiButton
          >
          <dl class="usage-facts">
            <div>
              <dt>调用时 Provider</dt>
              <dd>{{ detail.provider_name }}</dd>
            </div>
            <div>
              <dt>调用时模型</dt>
              <dd>{{ detail.model_name }}</dd>
            </div>
            <div>
              <dt>Provider 模型标识</dt>
              <dd>{{ detail.provider_model_id }}</dd>
            </div>
            <div>
              <dt>协议 / 模型类型</dt>
              <dd>{{ detail.protocol }} / {{ detail.model_type }}</dd>
            </div>
            <div>
              <dt>调用来源 / 用途</dt>
              <dd>{{ consumers[detail.consumer_kind] }} / {{ purposes[detail.purpose] }}</dd>
            </div>
            <div>
              <dt>尝试序号</dt>
              <dd>{{ detail.attempt_index }}</dd>
            </div>
            <div>
              <dt>派发情况</dt>
              <dd>{{ dispatches[detail.dispatch] }}</dd>
            </div>
            <div>
              <dt>结果</dt>
              <dd>{{ resultLabel(detail) }}</dd>
            </div>
            <div>
              <dt>开始时间（UTC）</dt>
              <dd>{{ detail.started_at }}</dd>
            </div>
            <div>
              <dt>派发时间（UTC）</dt>
              <dd>{{ detail.dispatched_at ?? '无派发记录' }}</dd>
            </div>
            <div>
              <dt>结束时间（UTC）</dt>
              <dd>{{ detail.final?.finished_at ?? '尚无最终记录' }}</dd>
            </div>
            <div>
              <dt>错误类别</dt>
              <dd>{{ detail.final?.error_category ?? '无错误类别记录' }}</dd>
            </div>
            <div>
              <dt>用量来源</dt>
              <dd>{{ detail.usage.source === 'provider' ? 'Provider 返回' : '未知' }}</dd>
            </div>
            <div v-for="field in usageTokenFields" :key="field">
              <dt>{{ tokenLabels[field] }}</dt>
              <dd>{{ detail.usage[field] ?? '未知' }}</dd>
            </div>
            <div>
              <dt>记录 ID</dt>
              <dd>{{ detail.id }}</dd>
            </div>
            <div>
              <dt>调用 ID</dt>
              <dd>{{ detail.call_id }}</dd>
            </div>
            <div>
              <dt>项目 ID</dt>
              <dd>{{ detail.project_id }}</dd>
            </div>
            <div>
              <dt>Agent ID</dt>
              <dd>{{ detail.agent_id ?? '无关联' }}</dd>
            </div>
            <div>
              <dt>Execution ID</dt>
              <dd>{{ detail.execution_id ?? '无关联' }}</dd>
            </div>
            <div>
              <dt>会议 ID</dt>
              <dd>{{ detail.meeting_id ?? '无关联' }}</dd>
            </div>
            <div>
              <dt>调用时 Provider ID</dt>
              <dd>{{ detail.provider_id }}</dd>
            </div>
            <div>
              <dt>调用时模型 ID</dt>
              <dd>{{ detail.model_id }}</dd>
            </div>
            <div>
              <dt>当前 Provider 引用</dt>
              <dd>{{ detail.live_provider_id ?? '当前不可引用' }}</dd>
            </div>
            <div>
              <dt>当前模型引用</dt>
              <dd>{{ detail.live_model_id ?? '当前不可引用' }}</dd>
            </div>
            <div>
              <dt>Provider 请求标识</dt>
              <dd>{{ detail.provider_request_id ?? '无记录' }}</dd>
            </div>
          </dl>
        </UiCard>
      </div>
    </template>
  </section>
</template>

<style scoped>
.usage-page {
  min-width: 0;
  display: grid;
  gap: var(--content-gap);
}
.usage-header,
.usage-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.usage-header {
  justify-content: space-between;
}
.usage-header p,
.usage-window,
dt {
  color: var(--muted);
}
.usage-content {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--content-gap);
}
.usage-content > *,
.usage-detail {
  min-width: 0;
}
.usage-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: var(--content-gap);
}
.usage-list li {
  min-width: 0;
  padding-bottom: var(--content-gap);
  border-bottom: 1px solid var(--border);
}
.usage-select {
  max-width: 100%;
  height: auto;
  overflow-wrap: anywhere;
  white-space: normal;
}
.usage-select[aria-pressed='true'] {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.usage-facts,
.usage-tokens {
  display: grid;
  gap: var(--content-gap);
}
.usage-tokens {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
.usage-facts dd,
.usage-tokens dd {
  margin: 0;
}
.usage-page p,
.usage-page time,
.usage-page dd,
.usage-page h2 {
  overflow-wrap: anywhere;
}
.usage-pagination {
  margin-top: var(--content-gap);
}
@media (max-width: 834px) {
  .usage-content {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
