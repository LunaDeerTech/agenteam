<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiState from '../../components/ui/UiState.vue'
import {
  meetingSummaryReason,
  type MeetingSummarySettingsController,
} from '../../composables/useMeetingSummarySettings'
const props = defineProps<{ settings: MeetingSummarySettingsController }>()
const page = props.settings
const {
  selection,
  references,
  draft,
  draftDetails,
  editor,
  state,
  activePurpose,
  progress,
  blocked,
  locked,
  feedback,
  canSave,
} = page
const choice = page.choices.summary
const heading = ref<HTMLElement | null>(null)
const actions = ref<HTMLElement | null>(null)
let alive = true
onBeforeUnmount(() => {
  alive = false
})
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let current: HTMLElement | null = node; current; current = current.parentElement)
    if (current.inert || current.hidden || current.getAttribute('aria-hidden') === 'true')
      return false
  return true
}
function restoreFocus(previous?: EventTarget | null) {
  if (!alive) return
  const current = document.activeElement
  if (current instanceof HTMLElement && current !== document.body && operable(current)) return
  const target =
    previous instanceof HTMLElement && operable(previous)
      ? previous
      : (actions.value?.querySelector<HTMLElement>('button:not(:disabled)') ?? heading.value)
  if (target && operable(target)) target.focus()
}
async function perform(work: () => Promise<unknown>, event: Event) {
  const previous = event.currentTarget
  await work()
  await nextTick()
  restoreFocus(previous)
}
watch(
  () => editor.open,
  async (open, previous) => {
    if (open || !previous) return
    await nextTick()
    restoreFocus()
  },
)
</script>
<template>
  <section class="meeting-summary-settings" aria-labelledby="meeting-summary-heading">
    <h2 id="meeting-summary-heading" ref="heading" tabindex="-1">会议 Summary</h2>
    <p class="meta">
      为所有项目的会议摘要初始、更新及首轮标题选择 System chat Model。独立保存，不要求
      json_schema；配置保存不表示生成或外部调用已可用。
    </p>
    <div ref="actions" class="actions" aria-label="会议 Summary 操作">
      <UiButton :disabled="blocked" @click="perform(page.refresh, $event)"
        >重读会议 Summary 配置</UiButton
      >
      <UiButton
        :disabled="blocked || selection.phase !== 'ready' || state.requiresReload || editor.open"
        :state="feedback === 'success' ? 'success' : 'idle'"
        success-label="会议 Summary 已保存"
        @click="page.openEditor"
        >{{ selection.value?.model ? '编辑会议 Summary' : '配置会议 Summary' }}</UiButton
      >
    </div>
    <p v-if="state.writeMessage" class="notice" role="status">{{ state.writeMessage }}</p>
    <p v-if="state.requiresReload" class="notice">
      需要明确重读并核对会议 Summary，才能开始新的保存。
    </p>
    <UiState
      v-if="selection.phase === 'loading'"
      kind="loading"
      title="正在读取会议 Summary 配置"
    />
    <UiState
      v-else-if="selection.phase === 'error'"
      kind="error"
      title="会议 Summary 配置读取失败"
      :description="selection.message"
    />
    <template v-else-if="selection.value">
      <p class="meta">
        会议 Summary 版本：<code>{{ selection.value.version }}</code>
      </p>
      <UiState
        v-if="selection.value.model === null"
        kind="empty"
        title="尚未配置会议 Summary"
        description="可独立选择 chat Model，无需先配置四项平台用途。保存后不提供清空。"
      />
      <div v-else class="current-summary">
        <p>
          保存的 Model ID：<code>{{ selection.value.model }}</code>
        </p>
        <p v-if="references.summary.phase === 'loading'" role="status">
          正在读取 Model 与 Provider 详情。
        </p>
        <template v-else-if="references.summary.value">
          <dl>
            <div>
              <dt>Model</dt>
              <dd>{{ references.summary.value.model.input.name }}</dd>
            </div>
            <div>
              <dt>原生模型 ID</dt>
              <dd>{{ references.summary.value.model.input.provider_model_id }}</dd>
            </div>
            <div>
              <dt>Provider</dt>
              <dd>{{ references.summary.value.provider.input.name }}</dd>
            </div>
            <div>
              <dt>启用状态</dt>
              <dd>
                Model
                {{ references.summary.value.model.input.enabled ? '已启用' : '已停用' }}；Provider
                {{ references.summary.value.provider.input.enabled ? '已启用' : '已停用' }}
              </dd>
            </div>
          </dl>
          <p v-if="meetingSummaryReason(references.summary.value)" class="notice">
            当前不可选：{{ meetingSummaryReason(references.summary.value) }}。保存的绑定仍保留。
          </p>
        </template>
        <p v-else class="notice">
          {{ references.summary.message || '当前详情不可用；保存的 Model ID 仍保留。' }}
        </p>
        <UiButton
          :disabled="blocked"
          variant="ghost"
          @click="perform(() => page.retryReference('summary'), $event)"
          >重读会议 Summary 详情</UiButton
        >
      </div>
    </template>
    <form
      v-if="editor.open"
      class="summary-editor"
      aria-label="会议 Summary 编辑"
      @submit.prevent="page.save"
    >
      <p class="meta">
        本次编辑版本：<code>{{ editor.version }}</code
        >。仅保存会议 Summary，四项平台用途不变。
      </p>
      <p v-if="editor.message" class="notice" role="status">{{ editor.message }}</p>
      <p v-if="draft.summary">
        草稿 Model：{{ draftDetails.summary?.model.input.name || draft.summary }}
      </p>
      <p v-else class="meta">尚未选择 Model。</p>
      <div class="actions">
        <UiButton :disabled="blocked || locked" @click="page.choosePurpose('summary')"
          >选择会议 Summary Model</UiButton
        >
        <UiButton
          type="submit"
          :disabled="!canSave"
          :state="feedback === 'loading' ? 'loading' : 'idle'"
          >保存会议 Summary</UiButton
        >
        <UiButton variant="ghost" @click="perform(page.closeEditor, $event)"
          >取消会议 Summary 编辑</UiButton
        >
      </div>
      <div v-if="activePurpose === 'summary'" class="summary-picker" aria-label="会议 Summary 候选">
        <div class="actions">
          <UiButton
            v-if="choice.mode === 'models'"
            :disabled="blocked || locked"
            variant="ghost"
            @click="page.backToProviders"
            >返回会议 Summary Providers</UiButton
          >
          <UiButton variant="ghost" @click="page.returnToPurposes">关闭会议 Summary 候选</UiButton>
        </div>
        <template v-if="choice.mode === 'providers'">
          <UiState
            v-if="choice.providers.phase === 'loading'"
            kind="loading"
            title="正在读取会议 Summary Providers"
          />
          <UiState
            v-else-if="choice.providers.phase === 'error'"
            kind="error"
            title="会议 Summary Provider 页读取失败"
            :description="choice.providers.message"
          />
          <UiState
            v-else-if="choice.providers.phase === 'empty'"
            kind="empty"
            title="当前 Provider 页为空"
            description="这里只表示当前页没有结果；可翻页或前往管理页配置 Provider。"
          />
          <ul class="choices">
            <li v-for="provider in choice.providers.rows" :key="provider.id">
              <strong>{{ provider.input.name }}</strong
              ><span
                >{{ provider.input.protocol }}；{{
                  provider.input.enabled ? '已启用' : '已停用'
                }}</span
              ><UiButton :disabled="blocked || locked" @click="page.selectProvider(provider)"
                >查看此 Provider 的 Models</UiButton
              >
            </li>
          </ul>
          <div class="actions" aria-label="会议 Summary Provider 分页">
            <UiButton
              :disabled="blocked || locked || !choice.providers.hasPrevious"
              @click="page.pageAction('providers', 'previous')"
              >上一页 Providers</UiButton
            >
            <UiButton
              :disabled="blocked || locked || !choice.providers.hasNext"
              @click="page.pageAction('providers', 'next')"
              >下一页 Providers</UiButton
            >
            <UiButton
              :disabled="blocked || locked"
              @click="
                page.pageAction('providers', choice.providers.cursorInvalid ? 'refresh' : 'retry')
              "
              >{{
                choice.providers.cursorInvalid ? '返回 Providers 首页' : '重读 Providers 当前页'
              }}</UiButton
            >
          </div>
        </template>
        <template v-else>
          <p v-if="choice.provider.value">
            Provider：{{ choice.provider.value.input.name }}（{{
              choice.provider.value.input.enabled ? '已启用' : '已停用'
            }}）
          </p>
          <UiState
            v-if="choice.provider.phase === 'loading'"
            kind="loading"
            title="正在核对会议 Summary Provider"
          />
          <UiState
            v-else-if="choice.provider.phase === 'error'"
            kind="error"
            title="会议 Summary Provider 详情不可用"
            :description="choice.provider.message"
            ><UiButton :disabled="blocked || locked" @click="page.retryProvider"
              >重读会议 Summary Provider</UiButton
            ></UiState
          >
          <UiState
            v-if="choice.models.phase === 'loading'"
            kind="loading"
            title="正在读取会议 Summary Models"
          />
          <UiState
            v-else-if="choice.models.phase === 'error'"
            kind="error"
            title="会议 Summary Model 页读取失败"
            :description="choice.models.message"
          />
          <UiState
            v-else-if="choice.models.phase === 'empty'"
            kind="empty"
            title="当前 Model 页为空"
            description="可翻页或前往管理页配置 Model，当前页不代表全域。"
          />
          <ul class="choices">
            <li v-for="model in choice.models.rows" :key="model.id">
              <strong>{{ model.input.name }}</strong
              ><span>{{ model.input.provider_model_id }} · {{ model.input.type }}</span>
              <p v-if="page.candidateReason(model)" class="notice">
                {{ page.candidateReason(model) }}
              </p>
              <UiButton
                :disabled="blocked || locked || !!page.candidateReason(model)"
                @click="page.selectModel(model)"
                >选择此会议 Summary Model</UiButton
              >
            </li>
          </ul>
          <div class="actions" aria-label="会议 Summary Model 分页">
            <UiButton
              :disabled="blocked || locked || !choice.models.hasPrevious"
              @click="page.pageAction('models', 'previous')"
              >上一页 Models</UiButton
            >
            <UiButton
              :disabled="blocked || locked || !choice.models.hasNext"
              @click="page.pageAction('models', 'next')"
              >下一页 Models</UiButton
            >
            <UiButton
              :disabled="blocked || locked"
              @click="page.pageAction('models', choice.models.cursorInvalid ? 'refresh' : 'retry')"
              >{{
                choice.models.cursorInvalid ? '返回 Models 首页' : '重读 Models 当前页'
              }}</UiButton
            >
          </div>
        </template>
        <p>
          <RouterLink to="/system/models">管理 Models</RouterLink> ·
          <RouterLink to="/system/providers">管理 Providers</RouterLink>
        </p>
      </div>
      <div v-if="progress?.phase === 'uncertain'" class="recovery" role="status">
        <p>
          会议 Summary 保存结果未确认。检查只读取原请求历史，必须显式重试原请求才能确认本次输入。
        </p>
        <p v-if="progress.observation !== 'none'">
          原请求观察：{{
            progress.observation === 'found'
              ? '找到历史回执'
              : progress.observation === 'missing'
                ? '尚未找到历史回执'
                : '历史读取失败'
          }}
        </p>
        <div class="actions">
          <UiButton :disabled="blocked" @click="perform(page.checkOriginal, $event)"
            >检查会议 Summary 原请求</UiButton
          >
          <UiButton
            :disabled="blocked || !progress.canRetryOriginal"
            @click="perform(page.retryOriginal, $event)"
            >重试会议 Summary 原请求</UiButton
          >
          <UiButton variant="ghost" @click="perform(page.abandonOperation, $event)"
            >放弃会议 Summary 请求跟踪</UiButton
          >
        </div>
      </div>
      <UiButton
        v-else-if="progress?.phase === 'rejected' || state.requiresReload"
        :disabled="blocked"
        @click="perform(page.review, $event)"
        >读取最新会议 Summary 并核对</UiButton
      >
    </form>
  </section>
</template>
<style scoped>
.meeting-summary-settings,
.summary-editor,
.summary-picker,
.recovery,
.current-summary {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.meeting-summary-settings {
  border-top: 1px solid var(--border);
  padding-top: 24px;
}
.actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: center;
}
.meta {
  color: var(--muted);
}
.notice {
  color: var(--text);
}
.choices {
  display: grid;
  gap: 16px;
  list-style: none;
  margin: 0;
  padding: 0;
}
.choices li {
  display: grid;
  gap: 8px;
  min-width: 0;
  border-top: 1px solid var(--border);
  padding-block: 12px;
}
dl {
  display: grid;
  gap: 8px;
  margin: 0;
}
dl > div {
  display: grid;
  grid-template-columns: minmax(80px, 0.35fr) minmax(0, 1fr);
  gap: 12px;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
  min-width: 0;
}
p,
dd,
code,
strong,
span {
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  dl > div {
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
  }
}
</style>
