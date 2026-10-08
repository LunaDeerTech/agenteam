<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import UiSelect from '../../components/ui/UiSelect.vue'
import UiSwitch from '../../components/ui/UiSwitch.vue'
import { projectChatProtocols, type ProjectChatProtocol } from '../../api/project-models'
import { useProjectModelSettings } from '../../composables/useProjectModelSettings'
const page = useProjectModelSettings()
const {
  visible,
  viewContext,
  provider,
  providerForm,
  blocked,
  pending,
  canMutate,
  canSaveProvider,
  canAdoptProvider,
  progress,
  canLookup,
  canReplay,
  preparedCredential,
  message,
} = page
const protocols = projectChatProtocols.map((value) => ({ value, label: value }))
const form = ref<HTMLFormElement | null>(null)
const locked = computed(
  () =>
    blocked.value ||
    pending.value ||
    !canMutate.value ||
    provider.phase !== 'ready' ||
    !provider.supported ||
    provider.conflict ||
    provider.requiresRead,
)
const canDelete = computed(
  () =>
    !blocked.value &&
    !pending.value &&
    canMutate.value &&
    provider.phase === 'ready' &&
    !!provider.original &&
    !provider.conflict &&
    !provider.requiresRead,
)
let alive = true
const successVisible = ref(false)
let successTimer: ReturnType<typeof setTimeout> | undefined
function clearSuccess() {
  if (successTimer !== undefined) clearTimeout(successTimer)
  successTimer = undefined
  successVisible.value = false
}
watch(
  () => progress.value?.receipt,
  (receipt) => {
    clearSuccess()
    if (
      !visible.value ||
      !provider.open ||
      progress.value?.phase !== 'confirmed' ||
      !(
        progress.value?.domain === 'configuration' &&
        progress.value.kind.startsWith('provider.') &&
        !!receipt &&
        'resource_id' in receipt &&
        receipt.resource_id === provider.target
      )
    )
      return
    successVisible.value = true
    successTimer = setTimeout(() => {
      successTimer = undefined
      if (alive && progress.value?.receipt === receipt) successVisible.value = false
    }, 2400)
  },
)
watch(
  () => visible.value && provider.open,
  (showing) => {
    if (!showing) clearSuccess()
  },
)
const buttonState = computed(() =>
  progress.value?.kind.startsWith('provider.') && progress.value?.phase === 'submitting'
    ? 'loading'
    : successVisible.value &&
        !page.providerDirty.value &&
        !!progress.value?.receipt &&
        'resource_id' in progress.value.receipt &&
        progress.value.receipt.resource_id === provider.target
      ? 'success'
      : 'idle',
)
onBeforeUnmount(() => {
  alive = false
  clearSuccess()
})
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let cursor: HTMLElement | null = node; cursor; cursor = cursor.parentElement)
    if (cursor.inert || cursor.hidden || cursor.getAttribute('aria-hidden') === 'true') return false
  return true
}
async function perform(work: () => unknown | Promise<unknown>, event: Event) {
  const context = viewContext.value
  const trigger = event instanceof SubmitEvent ? event.submitter : event.currentTarget
  await work()
  if (!alive || !context || viewContext.value !== context || !visible.value || !provider.open)
    return
  await nextTick()
  const root = form.value
  if (
    !alive ||
    viewContext.value !== context ||
    !visible.value ||
    !provider.open ||
    page.confirmation.open ||
    !root?.isConnected
  )
    return
  const top = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].filter(operable).at(-1)
  if (top !== root.closest('[role="dialog"]')) return
  const invalid = root.querySelector<HTMLElement>('[aria-invalid="true"]')
  const target =
    invalid && operable(invalid)
      ? invalid
      : trigger instanceof HTMLElement && top?.contains(trigger)
        ? trigger
        : null
  if (target && operable(target)) target.focus()
}
function chooseProtocol(value: string) {
  if (projectChatProtocols.includes(value as ProjectChatProtocol))
    providerForm.protocol = value as ProjectChatProtocol
}
</script>
<template>
  <UiDialog
    :open="visible && provider.open"
    :title="provider.mode === 'create' ? '创建 Provider' : 'Provider 详情与编辑'"
    :close-on-outside="false"
    :close-on-escape="!blocked && !pending"
    @update:open="!$event && page.closeProvider()"
  >
    <form
      ref="form"
      id="project-provider-form"
      class="provider-editor"
      novalidate
      @submit.prevent="perform(page.saveProvider, $event)"
    >
      <p class="meta">只保存本项目配置，不测试连接或调用外部服务。协议创建后不可修改。</p>
      <p v-if="!canMutate" class="notice" role="status">
        当前项目只读，不能保存新配置。合法的配置原请求重放仍按当前恢复条件提供。
      </p>
      <p v-if="provider.phase === 'loading'" role="status">正在读取 Provider…</p>
      <p v-if="provider.message" class="notice" role="alert">{{ provider.message }}</p>
      <p v-if="message && message !== provider.message" class="notice" role="status">
        {{ message }}
      </p>
      <p v-if="blocked" class="meta" role="status">等待当前请求或确认完成。</p>
      <p v-if="provider.conflict || provider.requiresRead" class="notice">
        请明确重读并核对当前值。重读不会自动合并、换版本保存或替代原写确认。
      </p>
      <p v-if="!provider.supported" class="notice">
        这份配置含本表单不支持修改的字段值，表单保持只读。删除仍需独立确认，不会改写这些字段。
      </p>
      <dl v-if="provider.target" class="facts">
        <dt>Provider ID</dt>
        <dd class="identifier">{{ provider.target }}</dd>
        <dt>当前编辑版本</dt>
        <dd>{{ provider.version || '尚未读取' }}</dd>
        <template v-if="provider.original"
          ><dt>创建时间</dt>
          <dd>{{ provider.original.created_at }}</dd>
          <dt>更新时间</dt>
          <dd>{{ provider.original.updated_at }}</dd></template
        >
      </dl>
      <UiButton
        v-if="provider.target"
        :disabled="blocked"
        @click="perform(() => page.readProvider(provider.target, true), $event)"
        >重新读取 Provider</UiButton
      >
      <section v-if="provider.review" class="notice" aria-label="Provider 当前值核对">
        <h3>本次重读的当前值（尚未采用）</h3>
        <pre>{{ JSON.stringify(provider.review, null, 2) }}</pre>
        <UiButton :disabled="!canAdoptProvider" @click="perform(page.adoptProvider, $event)"
          >采用当前 Provider 值</UiButton
        >
      </section>
      <template v-if="provider.phase !== 'inactive'">
        <UiField
          v-slot="field"
          label="Provider 名称"
          required
          :error="provider.fields.name"
          hint="1–128 个 Unicode 字符；原文保存，不自动修整。"
          ><UiInput
            :id="field.id"
            v-model="providerForm.name"
            :disabled="locked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            autocomplete="off"
        /></UiField>
        <UiField v-slot="field" label="协议" required :error="provider.fields.protocol"
          ><UiInput
            v-if="provider.mode === 'edit'"
            :id="field.id"
            :model-value="providerForm.protocol"
            readonly
            :aria-describedby="field.describedby" /><UiSelect
            v-else
            :id="field.id"
            :model-value="providerForm.protocol"
            :options="protocols"
            label="协议"
            :disabled="locked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            @update:model-value="chooseProtocol"
        /></UiField>
        <UiField
          v-slot="field"
          label="Base URL"
          required
          :error="provider.fields.base_url"
          hint="完整 HTTP/HTTPS 地址，不含用户信息、query 或 fragment；不会自动附加路径。"
          ><UiInput
            :id="field.id"
            v-model="providerForm.base_url"
            :disabled="locked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            autocomplete="off"
            spellcheck="false"
        /></UiField>
        <UiSwitch v-model="providerForm.enabled" :disabled="locked">启用状态</UiSwitch>
        <UiField
          v-slot="field"
          label="凭据 ID"
          :error="provider.fields.credential_ref"
          hint="只引用本项目 model 凭据；留空表示不绑定。保存 Provider 不会创建或删除凭据。"
          ><UiInput
            :id="field.id"
            v-model="providerForm.credential_ref"
            :disabled="locked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            autocomplete="off"
            spellcheck="false"
        /></UiField>
        <div class="actions">
          <UiButton
            :disabled="blocked || pending || !canMutate"
            @click="page.openCredential('create')"
            >创建凭据</UiButton
          ><UiButton
            :disabled="blocked || pending"
            @click="page.openCredential('manage', providerForm.credential_ref)"
            >管理凭据</UiButton
          >
        </div>
        <section v-if="preparedCredential" class="notice" aria-label="可选已创建凭据">
          <p>
            已创建凭据：<span class="identifier">{{ preparedCredential.credential_id }}</span>
          </p>
          <p>
            {{
              preparedCredential.bound
                ? '已用于本地确认的 Provider 保存。'
                : '尚未绑定；使用到草稿后仍需单独保存 Provider。'
            }}
          </p>
          <UiButton :disabled="locked" @click="page.usePreparedCredential()"
            >使用已创建凭据</UiButton
          >
        </section>
        <section v-if="provider.original" aria-label="Provider 只读选项">
          <h3>当前 options（只读）</h3>
          <pre>{{ JSON.stringify(provider.original.input.options, null, 2) }}</pre>
        </section>
        <p v-else class="meta">新配置的 options 固定为空对象。</p>
        <p v-if="provider.fields.expected_version" role="alert">
          {{ provider.fields.expected_version }}
        </p>
      </template>
      <section v-if="progress" class="notice recovery" aria-label="原请求恢复">
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
          历史回执已观察到；这不确认完整原输入，不会自动推进下一步。
        </p>
        <p v-else-if="progress.observation === 'not_observed'">
          本次未观察到回执；这不是回滚或未执行证明。
        </p>
        <p v-else-if="progress.observation === 'failed'">查证失败，原请求状态保持。</p>
        <pre v-if="progress.receipt" aria-label="严格执行回执">{{
          JSON.stringify(progress.receipt, null, 2)
        }}</pre>
        <pre v-if="progress.observedResult" aria-label="历史观察">{{
          JSON.stringify(progress.observedResult, null, 2)
        }}</pre>
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
      <div v-if="provider.mode === 'edit'" class="actions">
        <UiButton
          :disabled="blocked || pending || !canMutate || !provider.target"
          @click="provider.target && page.newModel(provider.target)"
          >创建 Model</UiButton
        ><UiButton variant="danger" :disabled="!canDelete" @click="page.openDelete('provider')"
          >删除 Provider</UiButton
        >
      </div>
      <p
        v-if="
          !canSaveProvider &&
          !blocked &&
          !pending &&
          !provider.conflict &&
          !provider.requiresRead &&
          provider.supported &&
          canMutate
        "
        class="meta"
      >
        没有可保存的改动。
      </p>
    </form>
    <template #footer
      ><UiButton :disabled="blocked || pending" @click="page.closeProvider()">取消</UiButton
      ><UiButton
        form="project-provider-form"
        type="submit"
        variant="primary"
        :disabled="!canSaveProvider"
        :state="buttonState"
        loading-label="保存中"
        success-label="已保存"
        >保存 Provider</UiButton
      ></template
    >
  </UiDialog>
</template>
<style scoped>
.provider-editor {
  display: grid;
  gap: 16px;
  min-width: 0;
}
p {
  margin: 0;
}
h3 {
  margin: 0 0 12px;
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
  background: var(--surface-alt);
  border-radius: var(--radius);
  overflow-wrap: anywhere;
}
.facts {
  display: grid;
  grid-template-columns: 120px minmax(0, 1fr);
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
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  overflow: auto;
  max-height: 260px;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
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
