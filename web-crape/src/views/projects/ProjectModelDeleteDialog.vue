<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiField from '../../components/ui/UiField.vue'
import UiSelect from '../../components/ui/UiSelect.vue'
import { useProjectModelSettings } from '../../composables/useProjectModelSettings'
const props = defineProps<{ fallbackFocus?: HTMLElement | null }>()
const page = useProjectModelSettings()
const {
  visible,
  viewContext,
  deletion,
  available,
  blocked,
  pending,
  canMutate,
  progress,
  canLookup,
  canReplay,
  message,
} = page
const content = ref<HTMLElement | null>(null)
const title = computed(() =>
  deletion.kind === 'provider'
    ? '删除 Provider'
    : deletion.kind === 'model'
      ? '删除 Model'
      : '删除凭据',
)
const options = computed(() => [
  { value: 'unset', label: '请选择无替代或候选', disabled: true },
  { value: '', label: '无替代' },
  ...available.state.items
    .filter((item) => item.id !== deletion.target)
    .map((item) => ({
      value: item.id,
      label: `${item.name} · ${item.scope.kind === 'system' ? 'System' : 'Project'} · ${item.id}`,
      disabled: available.state.stale,
    })),
])
const limits = [25, 50, 100].map((value) => ({ value: String(value), label: String(value) }))
let alive = true
onBeforeUnmount(() => {
  alive = false
})
function setLimit(value: string) {
  const limit = Number(value)
  if (limit === 25 || limit === 50 || limit === 100) void available.setLimit(limit)
}
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let cursor: HTMLElement | null = node; cursor; cursor = cursor.parentElement)
    if (cursor.inert || cursor.hidden || cursor.getAttribute('aria-hidden') === 'true') return false
  return true
}
async function perform(work: () => unknown | Promise<unknown>, event: Event) {
  const context = viewContext.value
  const deletedTarget = deletion.target
  const deletedKind = deletion.kind
  const trigger = event.currentTarget
  await work()
  if (!alive || !context || viewContext.value !== context || !visible.value) return
  await nextTick()
  if (!alive || viewContext.value !== context || !visible.value || page.confirmation.open) return
  const dialogs = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].filter(operable)
  if (!deletion.open) {
    const current = progress.value
    if (
      (deletedKind !== 'provider' && deletedKind !== 'model') ||
      current?.phase !== 'confirmed' ||
      current.kind !== `${deletedKind}.delete` ||
      !current.receipt ||
      !('resource_id' in current.receipt) ||
      current.receipt.resource_id !== deletedTarget ||
      dialogs.length
    )
      return
    const focused = document.activeElement
    if (focused instanceof HTMLElement && focused !== document.body && operable(focused)) return
    const fallback = props.fallbackFocus
    if (fallback && operable(fallback)) fallback.focus({ preventScroll: true })
    return
  }
  const root = content.value
  if (!root?.isConnected) return
  const dialog = root.closest<HTMLElement>('[role="dialog"]')
  const top = dialogs.at(-1)
  if (top !== dialog) return
  const invalid = root.querySelector<HTMLElement>('[aria-invalid="true"]')
  const target =
    invalid && operable(invalid)
      ? invalid
      : trigger instanceof HTMLElement && dialog?.contains(trigger)
        ? trigger
        : null
  if (target && operable(target)) target.focus()
}
</script>
<template>
  <UiDialog
    :open="visible && deletion.open"
    :title="title"
    :close-on-outside="false"
    :close-on-escape="!blocked"
    @update:open="!$event && page.closeDelete()"
  >
    <div ref="content" class="delete-content">
      <dl class="facts">
        <dt>对象</dt>
        <dd>{{ deletion.name }}</dd>
        <dt>稳定 ID</dt>
        <dd class="identifier">{{ deletion.target }}</dd>
        <dt>捕获版本</dt>
        <dd>{{ deletion.version }}</dd>
      </dl>
      <p v-if="deletion.kind === 'provider'">
        删除 Provider 不会删除凭据。不能从本地 Models 页判断它是否为空；仍有任何 Model
        时，服务会拒绝删除。
      </p>
      <template v-else-if="deletion.kind === 'model'">
        <p>
          仍被使用的模型暂不能在此删除。此操作不迁移 Agent 或其他引用，不修改会议
          Summary。合法无引用删除的受影响引用数为 0。
        </p>
        <p class="meta">
          明确选择无替代或当前安全目录中的候选。候选资格与引用变化由服务在提交时重新检查；这里没有删除预览或引用总数。
        </p>
        <section aria-label="删除替代候选" :aria-busy="available.state.phase === 'loading'">
          <UiButton :disabled="blocked" @click="available.fromFirst()">刷新替代候选</UiButton>
          <p v-if="available.state.phase === 'loading'" role="status">正在读取可用候选…</p>
          <p
            v-else-if="available.state.phase === 'waiting' || available.state.phase === 'inactive'"
            role="status"
          >
            请明确读取候选目录。
          </p>
          <div v-if="available.state.phase === 'error'" class="notice" role="alert">
            <p>{{ available.state.message }}</p>
            <UiButton
              :disabled="blocked"
              @click="available.state.cursorInvalid ? available.fromFirst() : available.retry()"
              >{{ available.state.cursorInvalid ? '从第一页读取候选' : '重试读取候选' }}</UiButton
            >
          </div>
          <p v-if="available.state.stale" class="meta" role="status">
            当前候选是旧观察，请先明确重读。
          </p>
          <p v-if="available.state.phase === 'empty'">本页没有可用候选；这不证明目标没有引用。</p>
          <UiField v-slot="field" label="删除替代" required :error="deletion.message"
            ><UiSelect
              :id="field.id"
              v-model="deletion.replacement"
              :options="options"
              label="删除替代"
              :disabled="blocked || pending || !canMutate"
              :invalid="field.invalid"
              :aria-describedby="field.describedby"
          /></UiField>
          <p v-if="deletion.replacement !== 'unset'" class="meta">
            已选：<span class="identifier">{{ deletion.replacement || '无替代' }}</span>
          </p>
          <div class="pagination" aria-label="删除候选分页">
            <UiField v-slot="field" label="每页候选"
              ><UiSelect
                :id="field.id"
                :model-value="String(available.state.limit)"
                :options="limits"
                label="每页候选"
                :disabled="blocked"
                @update:model-value="setLimit" /></UiField
            ><span>第 {{ available.state.page }} 页</span
            ><UiButton
              :disabled="blocked || !available.state.hasPrevious"
              @click="available.previous()"
              >上一页候选</UiButton
            ><UiButton :disabled="blocked || !available.state.hasNext" @click="available.next()"
              >下一页候选</UiButton
            >
          </div>
        </section>
      </template>
      <p v-else>
        删除凭据是独立命令。被引用或使用时服务会拒绝，不会自动解绑或删除 Provider；可返回 Provider
        明确保存解绑，再重新读取凭据信息并确认删除。
      </p>
      <p v-if="!canMutate" class="notice" role="status">当前项目只读，不能发起新的删除请求。</p>
      <p v-if="blocked" class="meta" role="status">等待当前请求或确认完成。</p>
      <p v-if="deletion.message && deletion.kind !== 'model'" class="notice" role="alert">
        {{ deletion.message }}
      </p>
      <p v-if="message" class="notice" role="status">{{ message }}</p>
      <p class="meta">
        若版本或状态冲突，请取消此确认，回到详情明确重读、核对后重新打开删除确认。不会自动更换捕获版本。
      </p>
      <section v-if="progress" class="notice" aria-label="原请求恢复">
        <h3>原请求与历史观察</h3>
        <p>
          操作：{{ progress.kind }}；{{
            progress.phase === 'confirmed'
              ? '原执行已严格确认'
              : progress.phase === 'uncertain'
                ? '结果尚未确认'
                : progress.phase === 'submitting'
                  ? '正在提交'
                  : '本次请求被明确拒绝'
          }}。
        </p>
        <p v-if="progress.keyConflict">原请求标识冲突，不能重放或自动换标识。</p>
        <p v-if="progress.observation === 'observed'">
          已观察到历史回执；这不确认完整原输入，也不自动推进删除。
        </p>
        <p v-else-if="progress.observation === 'not_observed'">
          未观察到回执不代表未执行或已经回滚。
        </p>
        <p v-else-if="progress.observation === 'failed'">查证失败，原请求状态保持。</p>
        <pre v-if="progress.receipt" aria-label="严格执行回执">{{
          JSON.stringify(progress.receipt, null, 2)
        }}</pre>
        <pre v-if="progress.observedResult" aria-label="历史观察">{{
          JSON.stringify(progress.observedResult, null, 2)
        }}</pre>
        <p>原 DELETE 重放使用捕获的请求，不依赖当前对象或候选是否仍然存在。</p>
        <p v-if="progress.domain === 'credential' && !canMutate">
          只读项目不允许凭据原写重放，可以明确查证历史。
        </p>
        <div class="actions">
          <UiButton :disabled="!canLookup" @click="perform(page.lookupOriginal, $event)"
            >查证原请求</UiButton
          ><UiButton :disabled="!canReplay" @click="perform(page.replayOriginal, $event)"
            >按原请求重放</UiButton
          ><UiButton :disabled="blocked" @click="perform(page.abandonPending, $event)"
            >放弃本地追踪</UiButton
          >
        </div>
      </section>
    </div>
    <template #footer
      ><UiButton :disabled="blocked" @click="page.closeDelete()">取消</UiButton
      ><UiButton
        variant="danger"
        :disabled="blocked || pending || !canMutate"
        :state="progress?.phase === 'submitting' ? 'loading' : 'idle'"
        loading-label="删除中"
        @click="perform(page.deleteSelected, $event)"
        >确认删除</UiButton
      ></template
    >
  </UiDialog>
</template>
<style scoped>
.delete-content {
  display: grid;
  gap: 16px;
  min-width: 0;
}
p {
  margin: 0;
}
h3 {
  margin: 0;
  font-size: 15px;
}
.meta,
dt {
  color: var(--muted);
}
.notice {
  display: grid;
  gap: 12px;
  padding: 12px;
  border-radius: var(--radius);
  background: var(--surface-alt);
  overflow-wrap: anywhere;
}
.facts {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 4px 12px;
  margin: 0;
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.identifier,
pre {
  font-family: var(--font-mono);
  font-size: 12px;
}
pre {
  margin: 0;
  max-height: 240px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  overflow: auto;
}
.actions,
.pagination {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.pagination {
  margin-top: 12px;
}
.pagination :deep(.ui-field) {
  min-width: 110px;
}
section[aria-label='删除替代候选'] {
  display: grid;
  gap: 12px;
}
@media (max-width: 650px) {
  .facts {
    grid-template-columns: minmax(0, 1fr);
  }
  dt {
    margin-top: 4px;
  }
}
</style>
