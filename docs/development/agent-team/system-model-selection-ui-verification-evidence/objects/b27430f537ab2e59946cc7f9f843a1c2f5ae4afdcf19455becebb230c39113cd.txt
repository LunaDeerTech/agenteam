<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiState from '../../components/ui/UiState.vue'
import {
  selectionLabels,
  selectionPurposes,
  selectionReason,
  useSystemModelSelection,
} from '../../composables/useSystemModelSelection'
const page = useSystemModelSelection()
const {
  editor,
  draft,
  draftDetails,
  activePurpose,
  choices,
  progress,
  state,
  blocked,
  locked,
  confirmation,
  feedback,
  canSave,
} = page
const choice = computed(() => (activePurpose.value ? choices[activePurpose.value] : null))
const listing = computed(() => (choice.value ? choice.value[choice.value.mode] : null))
const form = ref<HTMLFormElement | null>(null),
  pickerHeading = ref<HTMLElement | null>(null)
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
async function perform(work: () => unknown | Promise<unknown>, event: Event) {
  const previous = event instanceof SubmitEvent ? event.submitter : event.currentTarget
  await work()
  if (!alive || !form.value?.isConnected) return
  await nextTick()
  if (!alive || !editor.open || confirmation.open) return
  const target =
    previous instanceof HTMLElement && previous.tagName !== 'FORM' && operable(previous)
      ? previous
      : (pickerHeading.value ?? form.value?.querySelector<HTMLElement>('button:not(:disabled)'))
  if (target && operable(target)) target.focus()
}
</script>
<template>
  <UiDialog
    :open="editor.open"
    title="配置平台模型用途"
    @update:open="!$event && page.closeEditor()"
  >
    <form ref="form" class="selection-editor" @submit.prevent="perform(page.save, $event)">
      <p class="meta">
        核对版本：<code>{{ editor.version }}</code
        >。四项用途一起保存，只保存 Model 引用，不调用外部服务。
      </p>
      <ul class="draft-purposes" aria-label="四项用途草稿">
        <li v-for="purpose in selectionPurposes" :key="purpose">
          <h3>
            {{ selectionLabels[purpose] }}
            <span class="meta">{{
              purpose === 'embedding' || purpose === 'memory' ? '必填' : '可选'
            }}</span>
          </h3>
          <template v-if="draft[purpose]">
            <p v-if="draftDetails[purpose]">
              {{ draftDetails[purpose]!.model.input.name }} ·
              {{ draftDetails[purpose]!.model.input.provider_model_id }} ·
              {{ draftDetails[purpose]!.provider.input.name }}
            </p>
            <p v-else class="notice">尚未读取完整的当前详情，请重读或明确重新选择。</p>
            <p class="meta">
              Model ID：<code>{{ draft[purpose] }}</code>
            </p>
            <p
              v-if="draftDetails[purpose] && selectionReason(purpose, draftDetails[purpose]!)"
              class="notice"
            >
              当前不可选：{{ selectionReason(purpose, draftDetails[purpose]!) }}
            </p>
          </template>
          <p v-else class="meta">
            {{ purpose === 'embedding' || purpose === 'memory' ? '尚未选择' : '不配置' }}
          </p>
          <div class="choice-actions">
            <UiButton :disabled="locked" @click="perform(() => page.choosePurpose(purpose), $event)"
              >选择 {{ selectionLabels[purpose] }}</UiButton
            >
            <UiButton
              v-if="purpose === 'reranker' || purpose === 'image'"
              variant="ghost"
              :disabled="locked || draft[purpose] === null"
              @click="perform(() => page.clearOptional(purpose), $event)"
              >不配置 {{ selectionLabels[purpose] }}</UiButton
            >
            <UiButton
              v-if="draft[purpose] && draft[purpose] === page.references[purpose].id"
              variant="ghost"
              :disabled="blocked || locked"
              @click="perform(() => page.retryReference(purpose), $event)"
              >重读 {{ selectionLabels[purpose] }}</UiButton
            >
          </div>
        </li>
      </ul>
      <section
        v-if="activePurpose && choice && listing"
        class="candidate-picker"
        :aria-label="`选择 ${selectionLabels[activePurpose]} 的 Model`"
      >
        <h3 ref="pickerHeading" tabindex="-1">选择用途：{{ selectionLabels[activePurpose] }}</h3>
        <p class="meta">
          {{
            activePurpose === 'memory'
              ? '需要启用的 chat Model 与 Provider，并声明 json_schema 能力。'
              : '从匹配用途且 Model 与 Provider 均启用的候选中选择。'
          }}
        </p>
        <div class="choice-actions">
          <UiButton variant="ghost" @click="perform(page.returnToPurposes, $event)"
            >返回用途草稿</UiButton
          >
          <UiButton
            v-if="choice.mode === 'models'"
            variant="ghost"
            :disabled="locked"
            @click="perform(page.backToProviders, $event)"
            >返回 Provider 列表</UiButton
          >
        </div>
        <template v-if="choice.mode === 'models'">
          <p v-if="choice.provider.value">
            当前 Provider：{{ choice.provider.value.input.name }} ·
            {{ choice.provider.value.input.protocol }} ·
            {{ choice.provider.value.input.enabled ? '已启用' : '已停用' }}
          </p>
          <UiState
            v-else-if="choice.provider.phase === 'loading'"
            kind="loading"
            title="正在读取 Provider"
          />
          <UiState
            v-else-if="choice.provider.phase === 'error'"
            kind="error"
            title="Provider 详情读取失败"
            :description="choice.provider.message"
            ><UiButton :disabled="blocked || locked" @click="perform(page.retryProvider, $event)"
              >重读 Provider</UiButton
            ></UiState
          >
        </template>
        <UiState
          v-if="listing.phase === 'loading'"
          kind="loading"
          :title="choice.mode === 'providers' ? '正在读取 Providers' : '正在读取 Models'"
        />
        <UiState
          v-else-if="listing.phase === 'error'"
          kind="error"
          title="候选读取失败"
          :description="listing.message"
          ><UiButton
            :disabled="blocked || locked"
            @click="perform(() => page.pageAction(choice!.mode, 'retry'), $event)"
            >{{ listing.cursorInvalid ? '返回候选首页' : '重试候选读取' }}</UiButton
          ></UiState
        >
        <UiState
          v-else-if="listing.phase === 'empty'"
          kind="empty"
          :title="
            choice.mode === 'providers' ? '当前页没有 Provider' : '当前 Provider 的这一页没有 Model'
          "
          description="当前页为空不表示其他 Provider 或其他页没有合法候选。"
        >
          <RouterLink :to="choice.mode === 'providers' ? '/system/providers' : '/system/models'">{{
            choice.mode === 'providers' ? '管理 Providers' : '管理 Models'
          }}</RouterLink>
        </UiState>
        <ul
          v-if="choice.mode === 'providers' && choice.providers.phase === 'ready'"
          class="candidates"
          aria-label="当前 Provider 候选页"
        >
          <li v-for="row in choice.providers.rows" :key="row.id">
            <div>
              <strong>{{ row.input.name }}</strong>
              <p class="meta">
                {{ row.input.protocol }} · {{ row.input.enabled ? '已启用' : '已停用' }}
              </p>
            </div>
            <UiButton
              :disabled="blocked || locked"
              @click="perform(() => page.selectProvider(row), $event)"
              >浏览 {{ row.input.name }}</UiButton
            >
          </li>
        </ul>
        <ul
          v-if="choice.mode === 'models' && choice.models.phase === 'ready'"
          class="candidates"
          aria-label="当前 Model 候选页"
        >
          <li v-for="row in choice.models.rows" :key="row.id">
            <div>
              <strong>{{ row.input.name }}</strong>
              <p>{{ row.input.provider_model_id }}</p>
              <p class="meta">{{ row.input.type }} · {{ choice.provider.value?.input.name }}</p>
              <p v-if="page.candidateReason(row)" class="notice">
                不可选：{{ page.candidateReason(row) }}
              </p>
            </div>
            <UiButton
              :disabled="blocked || locked || !!page.candidateReason(row)"
              @click="perform(() => page.selectModel(row), $event)"
              >选择 {{ row.input.name }}</UiButton
            >
          </li>
        </ul>
        <nav
          class="choice-actions"
          :aria-label="choice.mode === 'providers' ? 'Provider 候选分页' : 'Model 候选分页'"
        >
          <UiButton
            :disabled="blocked || locked || !listing.hasPrevious"
            @click="perform(() => page.pageAction(choice!.mode, 'previous'), $event)"
            >上一页</UiButton
          >
          <UiButton
            :disabled="blocked || locked || !listing.hasNext"
            @click="perform(() => page.pageAction(choice!.mode, 'next'), $event)"
            >下一页</UiButton
          >
          <UiButton
            :disabled="blocked || locked || (choice.mode === 'models' && !choice.provider.value)"
            @click="perform(() => page.pageAction(choice!.mode, 'refresh'), $event)"
            >刷新候选</UiButton
          >
        </nav>
      </section>
      <p v-if="editor.message" class="notice" role="status">{{ editor.message }}</p>
      <p v-if="state.writeMessage" class="notice" role="status">{{ state.writeMessage }}</p>
      <div v-if="progress?.phase === 'uncertain'" class="recovery-actions">
        <p>
          结果未确认。历史查证{{
            progress.observation === 'found'
              ? '找到记录'
              : progress.observation === 'missing'
                ? '未找到记录'
                : progress.observation === 'failed'
                  ? '读取失败'
                  : '尚未执行'
          }}，不会代替原请求的执行回执。
        </p>
        <div class="choice-actions">
          <UiButton :disabled="blocked" @click="perform(page.checkOriginal, $event)"
            >检查原请求</UiButton
          ><UiButton
            :disabled="blocked || !progress.canRetryOriginal"
            @click="perform(page.retryOriginal, $event)"
            >重试原请求</UiButton
          ><UiButton variant="ghost" @click="perform(page.abandonOperation, $event)"
            >放弃原请求</UiButton
          >
        </div>
      </div>
      <UiButton
        v-else-if="progress?.phase === 'rejected' || state.requiresReload"
        :disabled="blocked"
        @click="perform(page.review, $event)"
        >读取最新配置并核对</UiButton
      >
      <div class="form-actions">
        <UiButton variant="ghost" @click="perform(page.closeEditor, $event)">取消</UiButton>
        <UiButton
          type="submit"
          variant="primary"
          :disabled="!canSave"
          :state="feedback"
          loading-label="正在保存"
          success-label="配置已保存"
          >保存用途配置</UiButton
        >
      </div>
    </form>
  </UiDialog>
</template>
<style scoped>
.selection-editor {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.meta {
  color: var(--muted);
}
.draft-purposes,
.candidates {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: 16px;
  min-width: 0;
}
.draft-purposes > li {
  display: grid;
  gap: 8px;
  min-width: 0;
  border-bottom: 1px solid var(--border);
  padding-bottom: 16px;
}
.draft-purposes h3 {
  font-size: 1rem;
}
.draft-purposes h3 span {
  font-size: var(--text-sm);
  font-weight: normal;
}
.choice-actions,
.form-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.form-actions {
  justify-content: flex-end;
}
.candidate-picker,
.recovery-actions {
  display: grid;
  gap: 14px;
  min-width: 0;
}
.candidates > li {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  align-items: start;
  min-width: 0;
  padding-block: 10px;
  border-bottom: 1px solid var(--border);
}
.candidates > li > div {
  min-width: 0;
}
.candidates > li > button {
  flex-shrink: 0;
  max-width: 50%;
  white-space: normal;
  overflow-wrap: anywhere;
}
p,
strong,
code,
h3 {
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .candidates > li {
    flex-direction: column;
  }
  .candidates > li > button {
    max-width: 100%;
  }
}
</style>
