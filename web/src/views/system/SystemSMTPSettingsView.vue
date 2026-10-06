<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, onUnmounted, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiCheckbox from '../../components/ui/UiCheckbox.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import UiSelect from '../../components/ui/UiSelect.vue'
import UiState from '../../components/ui/UiState.vue'
import {
  smtpFields,
  smtpRetryFields,
  useSystemSMTPSettings,
} from '../../composables/useSystemSMTPSettings'
const page = useSystemSMTPSettings()
const {
  observation,
  draft,
  editor,
  state,
  confirmation,
  disableConfirmation,
  feedback,
  material,
  progress,
  blocked,
  locked,
  errors,
  canSave,
  disableReady,
  maximumVersion,
} = page
const heading = ref<HTMLElement | null>(null),
  actions = ref<HTMLElement | null>(null),
  passwordInput = ref<HTMLInputElement | null>(null),
  alive = ref(true)
const identity = page.auth.personalContext.identity
const fallbackFocus = computed(() => {
  const current = page.auth.personalContext.identity
  return alive.value &&
    page.auth.state.phase === 'authenticated' &&
    page.auth.personalContext.phase === 'current' &&
    identity &&
    current &&
    identity.userID === current.userID &&
    identity.sessionID === current.sessionID &&
    identity.epoch === current.epoch &&
    page.auth.state.user?.role === 'admin' &&
    !page.auth.system.denied
    ? heading.value
    : null
})
const encryptionOptions = [
  { value: 'tls', label: 'TLS（tls）' },
  { value: 'starttls', label: 'STARTTLS（starttls）' },
  { value: 'none', label: '无加密（none）' },
]
const saveFeedback = computed(() => {
  const kind = editor.configuring ? 'configure' : 'policy'
  if (feedback.value === 'success' && state.confirmed?.kind !== kind) return 'idle'
  if (feedback.value === 'loading' && progress.value?.kind !== kind) return 'idle'
  return feedback.value
})
const dialogOpen = () => confirmation.open || disableConfirmation.open
function clearPasswordDOM() {
  if (passwordInput.value) passwordInput.value.value = ''
}
watch(
  () => material.value.revision,
  () => {
    if (!material.value.present) clearPasswordDOM()
  },
  { flush: 'sync' },
)
watch(
  () => progress.value?.phase,
  (phase) => {
    if (phase === 'submitting') clearPasswordDOM()
  },
  { flush: 'sync' },
)
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let a: HTMLElement | null = node; a; a = a.parentElement)
    if (a.inert || a.hidden || a.getAttribute('aria-hidden') === 'true') return false
  return true
}
async function perform(work: () => Promise<unknown>, event: Event) {
  const previous = event instanceof SubmitEvent ? event.submitter : event.currentTarget
  await work()
  if (!alive.value || !fallbackFocus.value) return
  await nextTick()
  if (!alive.value || !fallbackFocus.value || blocked.value || dialogOpen()) return
  const focused = document.activeElement
  if (focused instanceof HTMLElement && focused !== document.body && operable(focused)) return
  const target =
    previous instanceof HTMLElement && operable(previous)
      ? previous
      : actions.value?.querySelector<HTMLElement>('button:not(:disabled)')
  if (target && operable(target)) target.focus()
}
onMounted(async () => {
  page.attach()
  await nextTick()
  if (alive.value && !dialogOpen() && fallbackFocus.value && operable(fallbackFocus.value))
    fallbackFocus.value.focus()
})
onBeforeUnmount(() => {
  alive.value = false
  clearPasswordDOM()
})
onUnmounted(page.detach)
</script>
<template>
  <div class="system-smtp-settings">
    <h1 ref="heading" tabindex="-1">SMTP</h1>
    <p class="meta">
      配置邮件传输与重试策略。保存只确认配置持久化，不证明连接、认证、发送或收件成功。
    </p>
    <div ref="actions" class="smtp-actions" aria-label="SMTP 页面操作">
      <UiButton :disabled="blocked" @click="page.refresh">读取当前配置</UiButton>
      <UiButton v-if="observation.phase === 'loading'" variant="ghost" @click="page.cancelRead"
        >取消读取</UiButton
      >
      <UiButton v-if="observation.value?.configured" :disabled="!disableReady" @click="page.disable"
        >停用并清除配置</UiButton
      >
    </div>
    <p v-if="page.auth.state.busy" class="meta" role="status">正在等待当前请求结束。</p>
    <p v-if="state.writeMessage" class="notice" role="status">{{ state.writeMessage }}</p>
    <p v-if="state.confirmed" class="meta" aria-label="本次原操作的历史确认">
      原操作已确认，applied_version：<code>{{ state.confirmed.applied_version }}</code
      >。历史确认独立于当前配置。
    </p>
    <UiState
      v-if="progress?.phase === 'uncertain'"
      kind="error"
      title="请求结果未确认"
      description="当前配置不能证明原请求是否接受。检查会话后显式重试原请求将使用原密码和完整原输入；明确放弃只停止客户端追踪。"
    >
      <div class="smtp-actions">
        <UiButton :disabled="blocked" @click="page.checkOriginal">检查当前会话与配置</UiButton>
        <UiButton
          :disabled="blocked || !progress.canRetryOriginal"
          @click="perform(page.retryOriginal, $event)"
          >重试原请求</UiButton
        >
        <UiButton variant="ghost" @click="page.abandonOperation">放弃本次操作</UiButton>
      </div>
    </UiState>
    <UiState v-if="observation.phase === 'loading'" kind="loading" title="正在读取 SMTP 配置" />
    <UiState
      v-else-if="observation.phase === 'error'"
      kind="error"
      :title="state.confirmed ? '原操作已确认，当前配置读取失败' : 'SMTP 配置读取失败'"
      :description="observation.message"
    />
    <section v-else-if="observation.value" aria-label="当前读取的 SMTP 配置">
      <h2>当前读取值</h2>
      <p class="meta">
        当前配置版本：<code>{{ observation.value.version }}</code
        >；SMTP：{{ observation.value.configured ? '已配置' : '未配置' }}；凭据：{{
          observation.value.credential_present ? '已配置' : '未配置'
        }}。
      </p>
      <p v-if="!observation.value.configured" class="notice">
        SMTP 未配置。后台恢复链接由授权运维获取并转交；本页不读取恢复日志、链接或 token。
      </p>
      <dl class="current-values">
        <template v-if="observation.value.configured">
          <div v-for="field in smtpFields" :key="field.key">
            <dt>{{ field.label }}</dt>
            <dd>{{ observation.value[field.key] || '空' }}</dd>
          </div>
          <div>
            <dt>加密方式</dt>
            <dd>{{ observation.value.encryption }}</dd>
          </div>
        </template>
        <div v-for="field in smtpRetryFields" :key="field.key">
          <dt>{{ field.label }}</dt>
          <dd>{{ observation.value[field.key] }}</dd>
        </div>
      </dl>
    </section>
    <p v-if="state.requiresRead" class="notice">
      请在当前请求结束后明确读取配置；新的编辑基线必须来自成功的当前读取。
    </p>
    <form
      v-if="editor.ready"
      aria-label="修改 SMTP 配置"
      novalidate
      @submit.prevent="perform(page.save, $event)"
    >
      <h2>{{ editor.configuring ? '修改 SMTP 配置' : '修改未配置时的重试策略' }}</h2>
      <p class="meta">
        编辑基线版本：<code>{{ editor.version }}</code>
      </p>
      <p v-if="editor.message" class="notice" role="status">{{ editor.message }}</p>
      <UiState
        v-if="editor.conflict && progress?.phase !== 'uncertain'"
        kind="error"
        title="需要核对最新配置"
        description="当前读取不会改写原草稿、新密码或编辑版本。核对后明确采用最新值，再重新编辑。"
      >
        <div class="smtp-actions">
          <UiButton :disabled="blocked" @click="page.readLatest">读取最新配置</UiButton
          ><UiButton :disabled="blocked || observation.phase !== 'ready'" @click="page.adoptLatest"
            >采用最新值重新编辑</UiButton
          >
        </div>
      </UiState>
      <p v-if="maximumVersion" class="notice">配置版本已达上限；当前值可读取，不能继续保存。</p>
      <UiButton
        v-if="!editor.configuring"
        :disabled="blocked || locked || maximumVersion"
        @click="page.configure"
        >配置 SMTP</UiButton
      >
      <div v-if="editor.configuring" class="smtp-fields">
        <UiField
          v-for="field in smtpFields"
          :key="field.key"
          v-slot="control"
          :label="field.label"
          :hint="field.hint"
          :error="errors[field.key]"
          :required="['host', 'port', 'sender_email'].includes(field.key)"
        >
          <UiInput
            :id="control.id"
            :model-value="draft[field.key]"
            :invalid="control.invalid"
            :aria-describedby="control.describedby"
            :inputmode="field.key === 'port' ? 'numeric' : undefined"
            autocomplete="off"
            :disabled="
              blocked ||
              locked ||
              maximumVersion ||
              (editor.removeCredential && field.key === 'username')
            "
            @update:model-value="page.updateField(field.key, $event)"
          />
        </UiField>
        <UiField
          v-slot="control"
          label="加密方式"
          hint="明确选择 TLS、STARTTLS 或无加密；失败时不会自动降级。"
          :error="errors.encryption"
          required
        >
          <UiSelect
            :id="control.id"
            :model-value="draft.encryption"
            :options="encryptionOptions"
            label="加密方式"
            :disabled="blocked || locked || maximumVersion"
            :aria-describedby="control.describedby"
            @update:model-value="page.updateField('encryption', $event as string)"
          />
        </UiField>
        <UiField
          v-slot="control"
          label="新密码（仅写入）"
          hint="留空保持旧凭据；非空材料替换。1–2048 UTF-8 字节，不自动修整合法空格。"
          :error="errors.password || state.materialMessage"
        >
          <!-- Material is never a Vue model/prop. It stays in this input DOM and the private Session owner only. -->
          <input
            :id="control.id"
            ref="passwordInput"
            class="ui-input"
            type="password"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            :disabled="blocked || locked || maximumVersion || editor.removeCredential"
            :aria-invalid="control.invalid || undefined"
            :aria-describedby="control.describedby"
            @input="page.setMaterial(($event.target as HTMLInputElement).value)"
          />
        </UiField>
      </div>
      <template v-if="editor.configuring">
        <p v-if="material.present" class="meta" role="status">
          已保留新密码输入；重挂后不会回显。<UiButton
            variant="ghost"
            :disabled="blocked || locked"
            @click="page.setMaterial('')"
            >清除新密码输入</UiButton
          >
        </p>
        <UiCheckbox
          :model-value="editor.removeCredential"
          :disabled="blocked || locked || maximumVersion"
          @update:model-value="page.setRemoveCredential"
          >移除凭据并清空用户名</UiCheckbox
        >
        <p class="meta">移除凭据不等于停用 SMTP；取消尚未保存的移除会回到保持原凭据。</p>
        <p v-if="errors.transport" class="notice" role="status">{{ errors.transport }}</p>
      </template>
      <div class="smtp-fields">
        <UiField
          v-for="field in smtpRetryFields"
          :key="field.key"
          v-slot="control"
          :label="field.label"
          :hint="field.hint"
          :error="errors[field.key]"
          required
        >
          <UiInput
            :id="control.id"
            :model-value="draft[field.key]"
            :invalid="control.invalid"
            :aria-describedby="control.describedby"
            inputmode="numeric"
            autocomplete="off"
            :disabled="blocked || locked || maximumVersion"
            @update:model-value="page.updateField(field.key, $event)"
          />
        </UiField>
      </div>
      <div class="smtp-actions">
        <UiButton
          type="submit"
          variant="primary"
          :disabled="!canSave"
          :state="saveFeedback"
          :success-label="editor.configuring ? 'SMTP 配置已保存' : '重试策略已保存'"
          >{{ editor.configuring ? '保存 SMTP 配置' : '保存重试策略' }}</UiButton
        ><UiButton variant="ghost" :disabled="page.checking.value" @click="page.cancel"
          >取消修改</UiButton
        >
      </div>
    </form>
    <p class="meta">
      停用会清除传输配置和凭据引用，保留已保存重试策略；不保证立即物理擦除旧材料或终止已开始的投递。这里不提供连接检查、测试发送或投递任务管理。
    </p>
    <UiDialog
      :open="disableConfirmation.open"
      title="停用并清除 SMTP 配置？"
      :fallback-focus="fallbackFocus"
      @update:open="!$event && page.finishDisable(false)"
    >
      <p>将清除主机、认证、发件信息和凭据，保留已保存的重试策略；不撤回邮件或保证终止在途投递。</p>
      <p>
        目标版本：<code>{{ disableConfirmation.version }}</code
        >。保留自动重试 {{ disableConfirmation.auto_retry_count }} 次、间隔
        {{ disableConfirmation.retry_interval_seconds }} 秒。
      </p>
      <template #footer
        ><UiButton variant="ghost" @click="page.finishDisable(false)">取消停用</UiButton
        ><UiButton :disabled="blocked" @click="page.finishDisable(true)"
          >确认停用并清除</UiButton
        ></template
      >
    </UiDialog>
    <UiDialog
      :open="confirmation.open"
      :title="confirmation.title"
      :fallback-focus="fallbackFocus"
      @update:open="!$event && page.finishConfirmation(false)"
    >
      <p>{{ confirmation.message }}</p>
      <template #footer
        ><UiButton variant="ghost" @click="page.finishConfirmation(false)">继续编辑</UiButton
        ><UiButton @click="page.finishConfirmation(true)">放弃修改</UiButton></template
      >
    </UiDialog>
  </div>
</template>
<style scoped>
.system-smtp-settings,
form,
section {
  display: grid;
  gap: 20px;
  min-width: 0;
}
h2 {
  font-size: 1.1rem;
}
.meta {
  color: var(--muted);
}
p,
dt,
dd,
code {
  overflow-wrap: anywhere;
}
.smtp-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.smtp-fields,
.current-values {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px;
  min-width: 0;
}
.smtp-fields :deep(input) {
  min-width: 0;
  width: 100%;
}
.current-values {
  margin: 0;
  gap: 16px;
}
.current-values > div {
  display: grid;
  gap: 6px;
  min-width: 0;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
}
@media (max-width: 680px) {
  .smtp-fields,
  .current-values {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
