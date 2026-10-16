<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import { useProjectModelSettings } from '../../composables/useProjectModelSettings'
const page = useProjectModelSettings()
const {
  visible,
  viewContext,
  credential,
  credentialValue,
  preparedCredential,
  blocked,
  pending,
  canMutate,
  canCreateCredential,
  canRotateCredential,
  progress,
  canLookup,
  canReplay,
  message,
} = page
const form = ref<HTMLFormElement | null>(null)
const materialInput = ref<HTMLInputElement | null>(null)
const targetMatches = computed(
  () => !!credential.metadata && credential.reference === credential.metadata.credential_id,
)
const materialLocked = computed(
  () =>
    blocked.value ||
    pending.value ||
    !canMutate.value ||
    credential.conflict ||
    credential.requiresRead ||
    credential.phase !== 'ready' ||
    (credential.mode === 'manage' && !targetMatches.value),
)
const canDelete = computed(
  () =>
    !blocked.value &&
    !pending.value &&
    canMutate.value &&
    targetMatches.value &&
    !credential.conflict &&
    !credential.requiresRead,
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
      !credential.open ||
      progress.value?.phase !== 'confirmed' ||
      !(
        progress.value?.domain === 'credential' &&
        progress.value.kind === 'update' &&
        !!receipt &&
        'credential_id' in receipt &&
        receipt.credential_id === credential.reference
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
  () => visible.value && credential.open,
  (showing) => {
    if (!showing) clearSuccess()
  },
)
const buttonState = computed(() =>
  progress.value?.domain === 'credential' && progress.value?.phase === 'submitting'
    ? 'loading'
    : successVisible.value &&
        credentialValue.value === '' &&
        progress.value?.kind === 'update' &&
        !!progress.value.receipt &&
        'credential_id' in progress.value.receipt &&
        progress.value.receipt.credential_id === credential.reference
      ? 'success'
      : 'idle',
)
onBeforeUnmount(() => {
  alive = false
  clearSuccess()
  if (materialInput.value) materialInput.value.value = ''
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
  if (!alive || !context || viewContext.value !== context || !visible.value || !credential.open)
    return
  await nextTick()
  const root = form.value
  if (
    !alive ||
    viewContext.value !== context ||
    !visible.value ||
    !credential.open ||
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
function submit(event: Event) {
  if (credential.mode === 'manage' && !targetMatches.value) return
  return perform(
    credential.mode === 'create' ? page.createCredential : page.rotateCredential,
    event,
  )
}
</script>
<template>
  <UiDialog
    :open="visible && credential.open"
    :title="credential.mode === 'create' ? '创建凭据' : '管理凭据'"
    :close-on-outside="false"
    :close-on-escape="!blocked"
    @update:open="!$event && page.closeCredential()"
  >
    <form
      ref="form"
      id="project-credential-form"
      class="credential-editor"
      novalidate
      @submit.prevent="submit"
    >
      <p class="meta">仅管理本项目 model 凭据。没有凭据目录，也不会读取或显示已有材料。</p>
      <p v-if="!canMutate" class="notice" role="status">
        当前项目只读，不能创建、轮换、删除或原样重放凭据写入；可以读取安全信息和查证历史。
      </p>
      <p v-if="blocked" role="status">等待当前操作完成。</p>
      <p v-if="credential.phase === 'loading'" role="status">正在读取凭据信息…</p>
      <p v-if="credential.message" class="notice" role="status">{{ credential.message }}</p>
      <p v-if="message && message !== credential.message" class="notice" role="status">
        {{ message }}
      </p>
      <template v-if="credential.mode === 'manage'">
        <UiField
          v-slot="field"
          label="Credential ID"
          required
          :error="credential.fields.reference"
          hint="输入明确的同项目凭据 ID；这里只读取 purpose 与版本。"
          ><UiInput
            :id="field.id"
            v-model="credential.reference"
            :disabled="blocked || pending"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            autocomplete="off"
            spellcheck="false"
        /></UiField>
        <UiButton :disabled="blocked" @click="perform(page.readCredential, $event)"
          >读取凭据信息</UiButton
        >
        <dl v-if="credential.metadata" class="facts">
          <dt>Credential ID</dt>
          <dd class="identifier">{{ credential.metadata.credential_id }}</dd>
          <dt>用途</dt>
          <dd>{{ credential.metadata.purpose }}</dd>
          <dt>已读版本</dt>
          <dd>{{ credential.metadata.version }}</dd>
        </dl>
        <p v-else class="meta">尚无当前可操作的凭据信息。历史回执不代替 metadata 读取。</p>
        <p v-if="credential.metadata && !targetMatches" class="notice">
          输入的 ID 与已读凭据不同。请先明确读取，再操作该目标。
        </p>
        <section v-if="credential.conflict || credential.requiresRead" class="notice">
          <p>先明确重读并核对安全版本，再采用当前版本。不会自动保存或比较材料是否相同。</p>
          <UiButton
            :disabled="blocked || pending || !targetMatches || credential.requiresRead"
            @click="perform(page.adoptCredentialMetadata, $event)"
            >采用当前凭据版本</UiButton
          >
        </section>
      </template>
      <UiField
        v-slot="field"
        label="新凭据材料"
        :required="credential.mode === 'create'"
        :error="credential.fields.value"
        hint="仅写入；1–65536 UTF-8 字节，保留原文。管理现有凭据时留空不旋转。"
      >
        <input
          :id="field.id"
          ref="materialInput"
          v-model="credentialValue"
          class="ui-input"
          type="password"
          :disabled="materialLocked"
          :aria-invalid="field.invalid || undefined"
          :aria-describedby="field.describedby"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
        />
      </UiField>
      <p class="meta">
        发送后输入框清空。待决原请求由既有私有状态保存，查证不会显示材料、自动重放或自动绑定
        Provider。
      </p>
      <section v-if="preparedCredential" class="notice" aria-label="已创建凭据安全引用">
        <h3>已创建凭据</h3>
        <dl class="facts">
          <dt>Credential ID</dt>
          <dd class="identifier">{{ preparedCredential.credential_id }}</dd>
          <dt>版本</dt>
          <dd>{{ preparedCredential.version }}</dd>
          <dt>本地绑定状态</dt>
          <dd>{{ preparedCredential.bound ? '已用于确认的 Provider 保存' : '尚未绑定' }}</dd>
        </dl>
        <UiButton
          :disabled="
            blocked ||
            pending ||
            !canMutate ||
            !page.provider.open ||
            !page.provider.supported ||
            page.provider.phase !== 'ready' ||
            page.provider.conflict ||
            page.provider.requiresRead
          "
          @click="page.usePreparedCredential()"
          >使用已创建凭据</UiButton
        >
        <p>这只把安全 ID 放入已打开的 Provider 草稿；关闭此层后仍需明确保存 Provider。</p>
      </section>
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
          已观察到历史回执；它不能确认完整材料，也不会清除待决或推进 Provider 写入。
        </p>
        <p v-else-if="progress.observation === 'not_observed'">
          本次未观察到回执；这不是未执行或回滚证明。
        </p>
        <p v-else-if="progress.observation === 'failed'">本次查证失败，原请求状态保持。</p>
        <pre v-if="progress.receipt" aria-label="严格执行回执">{{
          JSON.stringify(progress.receipt, null, 2)
        }}</pre>
        <pre v-if="progress.observedResult" aria-label="历史观察">{{
          JSON.stringify(progress.observedResult, null, 2)
        }}</pre>
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
      <UiButton
        v-if="credential.mode === 'manage'"
        variant="danger"
        :disabled="!canDelete"
        @click="page.openDelete('credential')"
        >删除凭据</UiButton
      >
    </form>
    <template #footer
      ><UiButton :disabled="blocked" @click="page.closeCredential()">关闭</UiButton
      ><UiButton
        form="project-credential-form"
        type="submit"
        variant="primary"
        :disabled="
          credential.mode === 'create'
            ? !canCreateCredential
            : !canRotateCredential || !targetMatches
        "
        :state="buttonState"
        loading-label="提交中"
        success-label="已确认"
        >{{ credential.mode === 'create' ? '创建凭据' : '轮换凭据' }}</UiButton
      ></template
    >
  </UiDialog>
</template>
<style scoped>
.credential-editor {
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
  max-height: 240px;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
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
