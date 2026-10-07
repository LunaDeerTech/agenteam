<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, onUnmounted, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiState from '../../components/ui/UiState.vue'
import SystemModelSelectionEditor from './SystemModelSelectionEditor.vue'
import SystemMeetingSummarySettings from './SystemMeetingSummarySettings.vue'
import {
  selectionLabels,
  selectionPurposes,
  selectionReason,
  useSystemModelSelection,
} from '../../composables/useSystemModelSelection'
const page = useSystemModelSelection()
const { selection, references, editor, state, confirmation, blocked, feedback } = page
const heading = ref<HTMLElement | null>(null),
  actions = ref<HTMLElement | null>(null)
let alive = true
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let current: HTMLElement | null = node; current; current = current.parentElement)
    if (current.inert || current.hidden || current.getAttribute('aria-hidden') === 'true')
      return false
  return true
}
function restoreFocus(previous?: EventTarget | null) {
  if (!alive || editor.open || confirmation.open) return
  const target =
    previous instanceof HTMLElement && operable(previous)
      ? previous
      : (actions.value?.querySelector<HTMLElement>('button:not(:disabled)') ?? heading.value)
  if (target && operable(target)) target.focus()
}
async function perform(work: () => Promise<unknown>, event: Event) {
  const previous = event.currentTarget
  await work()
  if (!alive) return
  await nextTick()
  if (!blocked.value) restoreFocus(previous)
}
watch(
  () => editor.open,
  async (open, previous) => {
    if (open || !previous) return
    await nextTick()
    if (!alive || editor.open || confirmation.open) return
    const focused = document.activeElement
    if (focused instanceof HTMLElement && focused !== document.body && operable(focused)) return
    restoreFocus()
  },
)
onMounted(async () => {
  page.attach()
  await nextTick()
  if (alive && !editor.open && !confirmation.open && heading.value && operable(heading.value))
    heading.value.focus()
})
onBeforeUnmount(() => {
  alive = false
})
onUnmounted(page.detach)
</script>
<template>
  <div class="system-model-selection">
    <h1 ref="heading" tabindex="-1">平台模型用途</h1>
    <p class="meta">
      为四项平台用途和独立的会议 Summary 选择已有的 System
      Model。各区独立保存；保存仅确认配置，不表示外部服务可调用、索引已重建或检索已切换。
    </p>
    <div ref="actions" class="selection-actions" aria-label="用途配置页面操作">
      <UiButton :disabled="blocked" @click="perform(page.refresh, $event)">刷新配置</UiButton>
      <UiButton
        :disabled="blocked || selection.phase !== 'ready' || state.requiresReload"
        :state="feedback === 'success' ? 'success' : 'idle'"
        success-label="配置已保存"
        @click="page.openEditor"
        >{{ selection.value?.configured ? '编辑用途' : '配置用途' }}</UiButton
      >
      <RouterLink to="/system/models">管理 Models</RouterLink>
      <RouterLink to="/system/providers">管理 Providers</RouterLink>
    </div>
    <p v-if="page.auth.state.busy" class="meta" role="status">正在等待当前请求结束。</p>
    <p v-if="state.writeMessage" class="notice" role="status">{{ state.writeMessage }}</p>
    <p v-if="state.requiresReload" class="notice">
      需要明确重新读取并核对当前配置，不能据旧观察开始新保存。
    </p>
    <UiState v-if="selection.phase === 'loading'" kind="loading" title="正在读取用途配置" />
    <UiState
      v-else-if="selection.phase === 'error'"
      kind="error"
      title="用途配置读取失败"
      :description="selection.message"
    >
      <UiButton :disabled="blocked" @click="perform(page.refresh, $event)">重新读取配置</UiButton>
    </UiState>
    <template v-else-if="selection.value">
      <p class="meta">
        配置版本：<code>{{ selection.value.version }}</code>
      </p>
      <UiState
        v-if="selection.value.configured === null"
        kind="empty"
        title="尚未配置平台模型用途"
        description="首次保存需要同时选择 Embedding 和 Memory。Reranker 与 Image Generation 可明确不配置。"
      />
      <ul v-else class="current-references" aria-label="当前保存的四项用途">
        <li v-for="purpose in selectionPurposes" :key="purpose">
          <h2>{{ selectionLabels[purpose] }}</h2>
          <p v-if="references[purpose].id === null" class="meta">未配置（可选）</p>
          <template v-else>
            <p class="reference-id">
              保存的 Model ID：<code>{{ references[purpose].id }}</code>
            </p>
            <p v-if="references[purpose].phase === 'loading'" role="status">
              正在读取 Model 与 Provider 详情。
            </p>
            <template v-else-if="references[purpose].value">
              <dl>
                <div>
                  <dt>Model</dt>
                  <dd>{{ references[purpose].value!.model.input.name }}</dd>
                </div>
                <div>
                  <dt>原生模型 ID</dt>
                  <dd>{{ references[purpose].value!.model.input.provider_model_id }}</dd>
                </div>
                <div>
                  <dt>类型</dt>
                  <dd>{{ references[purpose].value!.model.input.type }}</dd>
                </div>
                <div>
                  <dt>Provider</dt>
                  <dd>{{ references[purpose].value!.provider.input.name }}</dd>
                </div>
                <div>
                  <dt>启用状态</dt>
                  <dd>
                    Model
                    {{
                      references[purpose].value!.model.input.enabled ? '已启用' : '已停用'
                    }}；Provider
                    {{ references[purpose].value!.provider.input.enabled ? '已启用' : '已停用' }}
                  </dd>
                </div>
              </dl>
              <p v-if="selectionReason(purpose, references[purpose].value!)" class="notice">
                当前不可选：{{
                  selectionReason(purpose, references[purpose].value!)
                }}。保存的绑定仍保留。
              </p>
            </template>
            <p v-else class="notice">
              {{ references[purpose].message || '尚未获得完整详情；保存的引用仍保留。' }}
            </p>
            <UiButton
              variant="ghost"
              :disabled="blocked"
              @click="perform(() => page.retryReference(purpose), $event)"
              >重读 {{ selectionLabels[purpose] }} 详情</UiButton
            >
          </template>
        </li>
      </ul>
    </template>
    <SystemMeetingSummarySettings :settings="page.meetingSummary" />
    <SystemModelSelectionEditor />
    <!-- The business dialog and section remount before the sole confirmation host. -->
    <UiDialog
      :open="confirmation.open"
      :title="confirmation.title"
      @update:open="!$event && page.finishConfirmation(false)"
    >
      <p>{{ confirmation.message }}</p>
      <template #footer>
        <UiButton variant="ghost" @click="page.finishConfirmation(false)">继续编辑</UiButton>
        <UiButton @click="page.finishConfirmation(true)">{{
          confirmation.title === '读取最新配置并核对？' ? '读取并核对' : '放弃修改'
        }}</UiButton>
      </template>
    </UiDialog>
  </div>
</template>
<style scoped>
.system-model-selection {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.meta {
  color: var(--muted);
}
.selection-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.current-references {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px;
  padding: 0;
  margin: 0;
  list-style: none;
}
.current-references > li {
  display: grid;
  gap: 12px;
  align-content: start;
  min-width: 0;
  padding-block: 12px;
  border-top: 1px solid var(--border);
}
.current-references h2 {
  font-size: 1.1rem;
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
h2 {
  overflow-wrap: anywhere;
}
.notice {
  color: var(--text);
}
@media (max-width: 760px) {
  .current-references {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 450px) {
  dl > div {
    grid-template-columns: minmax(0, 1fr);
    gap: 3px;
  }
}
</style>
