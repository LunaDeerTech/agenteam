<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch, useId } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import UiCheckbox from '../../components/ui/UiCheckbox.vue'
import UiSwitch from '../../components/ui/UiSwitch.vue'
import { useProjectModelSettings } from '../../composables/useProjectModelSettings'
const page = useProjectModelSettings()
const {
  visible,
  viewContext,
  model,
  modelForm,
  modelProvider,
  modelReviewProvider,
  blocked,
  pending,
  canMutate,
  canSaveModel,
  canAdoptModel,
  progress,
  canLookup,
  canReplay,
  message,
} = page
const form = ref<HTMLFormElement | null>(null)
const capabilityNote = useId()
const locked = computed(
  () =>
    blocked.value ||
    pending.value ||
    !canMutate.value ||
    model.phase !== 'ready' ||
    !model.supported ||
    model.conflict ||
    model.requiresRead,
)
const structured = computed(() =>
  modelProvider.value?.input.protocol === 'openai-chat-completions'
    ? ['text', 'json_schema']
    : ['text'],
)
const canDelete = computed(
  () =>
    !blocked.value &&
    !pending.value &&
    canMutate.value &&
    model.phase === 'ready' &&
    !!model.original &&
    !model.conflict &&
    !model.requiresRead,
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
      !model.open ||
      progress.value?.phase !== 'confirmed' ||
      !(
        progress.value?.domain === 'configuration' &&
        progress.value.kind.startsWith('model.') &&
        !!receipt &&
        'resource_id' in receipt &&
        receipt.resource_id === model.target
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
  () => visible.value && model.open,
  (showing) => {
    if (!showing) clearSuccess()
  },
)
const buttonState = computed(() =>
  progress.value?.kind.startsWith('model.') && progress.value?.phase === 'submitting'
    ? 'loading'
    : successVisible.value &&
        !page.modelDirty.value &&
        !!progress.value?.receipt &&
        'resource_id' in progress.value.receipt &&
        progress.value.receipt.resource_id === model.target
      ? 'success'
      : 'idle',
)
onBeforeUnmount(() => {
  alive = false
  clearSuccess()
})
function toggle(
  field: 'input_modalities' | 'output_modalities' | 'structured_output_modes',
  value: string,
  checked: boolean,
) {
  if (locked.value) return
  modelForm[field] = checked
    ? [...modelForm[field].filter((item) => item !== value), value]
    : modelForm[field].filter((item) => item !== value)
}
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
  if (!alive || !context || viewContext.value !== context || !visible.value || !model.open) return
  await nextTick()
  const root = form.value
  if (
    !alive ||
    viewContext.value !== context ||
    !visible.value ||
    !model.open ||
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
</script>
<template>
  <UiDialog
    :open="visible && model.open"
    :title="model.mode === 'create' ? '创建 Model' : 'Model 详情与编辑'"
    :close-on-outside="false"
    :close-on-escape="!blocked && !pending"
    @update:open="!$event && page.closeModel()"
  >
    <form
      ref="form"
      id="project-model-form"
      class="model-editor"
      novalidate
      @submit.prevent="perform(page.saveModel, $event)"
    >
      <p class="meta">仅管理本项目 chat 配置。能力是配置声明，不代表已经验证外部模型支持。</p>
      <p v-if="!canMutate" class="notice" role="status">
        当前项目只读，不能保存新配置；原配置请求的合法恢复仍按当前条件提供。
      </p>
      <p v-if="model.phase === 'loading'" role="status">正在读取 Model 及其 Provider…</p>
      <p v-if="model.message" class="notice" role="alert">{{ model.message }}</p>
      <p v-if="message && message !== model.message" class="notice" role="status">{{ message }}</p>
      <p v-if="blocked" class="meta" role="status">等待当前请求或确认完成。</p>
      <p v-if="model.conflict || model.requiresRead" class="notice">
        请明确重读并核对当前值。不会自动合并、采用新版本或重新保存。
      </p>
      <p v-if="!model.supported" class="notice">
        当前配置含本表单不能编辑的值，表单保持只读，完整原配置保留在下方；删除仍需独立确认。
      </p>
      <dl class="facts">
        <template v-if="model.target"
          ><dt>Model ID</dt>
          <dd class="identifier">{{ model.target }}</dd>
          <dt>当前编辑版本</dt>
          <dd>{{ model.version || '尚未读取' }}</dd></template
        >
        <template v-if="modelProvider"
          ><dt>Provider</dt>
          <dd>{{ modelProvider.input.name }}</dd>
          <dt>Provider ID</dt>
          <dd class="identifier">{{ modelProvider.id }}</dd>
          <dt>协议</dt>
          <dd>{{ modelProvider.input.protocol }}</dd></template
        >
        <dt>类型</dt>
        <dd>chat（只读）</dd>
        <template v-if="model.original"
          ><dt>创建时间</dt>
          <dd>{{ model.original.created_at }}</dd>
          <dt>更新时间</dt>
          <dd>{{ model.original.updated_at }}</dd></template
        >
      </dl>
      <p class="meta">所属 Provider 和类型创建后不可修改。</p>
      <UiButton
        v-if="model.target"
        :disabled="blocked"
        @click="perform(() => page.readModel(model.target, true), $event)"
        >重新读取 Model</UiButton
      >
      <section v-if="model.review" class="notice" aria-label="Model 当前值核对">
        <h3>本次重读的当前值（尚未采用）</h3>
        <pre>{{ JSON.stringify(model.review, null, 2) }}</pre>
        <p v-if="modelReviewProvider">
          当前 Provider 协议：{{ modelReviewProvider.input.protocol }}
        </p>
        <UiButton :disabled="!canAdoptModel" @click="perform(page.adoptModel, $event)"
          >采用当前 Model 值</UiButton
        >
      </section>
      <template v-if="model.phase !== 'inactive'">
        <UiField
          v-slot="field"
          label="Model 名称"
          required
          :error="model.fields.name"
          hint="1–128 个 Unicode 字符；原文保存，不自动修整。"
          ><UiInput
            :id="field.id"
            v-model="modelForm.name"
            :disabled="locked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            autocomplete="off"
        /></UiField>
        <UiField
          v-slot="field"
          label="原生型号"
          required
          :error="model.fields.provider_model_id"
          hint="Provider 接受的型号标识，1–256 UTF-8 字节；不自动修整。"
          ><UiInput
            :id="field.id"
            v-model="modelForm.provider_model_id"
            :disabled="locked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            autocomplete="off"
            spellcheck="false"
        /></UiField>
        <UiSwitch v-model="modelForm.enabled" :disabled="locked">启用状态</UiSwitch>
        <fieldset
          class="capabilities"
          tabindex="-1"
          :aria-invalid="!!model.fields.capabilities || undefined"
          :aria-describedby="model.fields.capabilities ? capabilityNote : undefined"
        >
          <legend>模型能力</legend>
          <p v-if="model.fields.capabilities" :id="capabilityNote" class="error-text" role="alert">
            {{ model.fields.capabilities }}
          </p>
          <div class="choices">
            <UiCheckbox v-model="modelForm.tool_calls" :disabled="locked">工具调用</UiCheckbox
            ><UiCheckbox v-model="modelForm.parallel_tool_calls" :disabled="locked"
              >并行工具调用</UiCheckbox
            ><UiCheckbox v-model="modelForm.streaming" :disabled="locked">流式</UiCheckbox
            ><UiCheckbox v-model="modelForm.reasoning" :disabled="locked">推理</UiCheckbox>
          </div>
          <p class="meta">并行工具调用要求启用工具调用；本表单不设置推理等级。</p>
          <fieldset class="choice-group">
            <legend>输入形式</legend>
            <div class="choices">
              <UiCheckbox
                v-for="value in ['text', 'image', 'file', 'vector']"
                :key="value"
                :model-value="modelForm.input_modalities.includes(value)"
                :disabled="locked"
                @update:model-value="toggle('input_modalities', value, $event)"
                >{{ value }}</UiCheckbox
              >
            </div>
          </fieldset>
          <fieldset class="choice-group">
            <legend>输出形式</legend>
            <UiCheckbox
              :model-value="modelForm.output_modalities.includes('text')"
              :disabled="locked"
              @update:model-value="toggle('output_modalities', 'text', $event)"
              >text</UiCheckbox
            >
          </fieldset>
          <fieldset class="choice-group">
            <legend>结构化输出</legend>
            <div class="choices">
              <UiCheckbox
                v-for="value in structured"
                :key="value"
                :model-value="modelForm.structured_output_modes.includes(value)"
                :disabled="locked"
                @update:model-value="toggle('structured_output_modes', value, $event)"
                >{{ value }}</UiCheckbox
              >
            </div>
          </fieldset>
          <div class="capacity-fields">
            <UiField v-slot="field" label="上下文长度" hint="可留空；填写精确的正整数十进制。"
              ><UiInput
                :id="field.id"
                v-model="modelForm.context_length"
                :disabled="locked"
                :aria-describedby="field.describedby"
                inputmode="numeric"
                autocomplete="off" /></UiField
            ><UiField
              v-slot="field"
              label="最大输出"
              hint="可留空；有值时不能超过已填写的上下文长度。"
              ><UiInput
                :id="field.id"
                v-model="modelForm.max_output"
                :disabled="locked"
                :aria-describedby="field.describedby"
                inputmode="numeric"
                autocomplete="off"
            /></UiField>
          </div>
        </fieldset>
        <section v-if="model.original" class="read-only-values" aria-label="当前 Model 完整配置">
          <h3>当前配置（只读）</h3>
          <p class="meta">已有参数、覆盖和推理等级完整保留；本表单不会把不支持的值改写为空。</p>
          <h4>parameters</h4>
          <pre>{{ JSON.stringify(model.original.input.parameters, null, 2) }}</pre>
          <h4>request_overwrite</h4>
          <pre>{{ JSON.stringify(model.original.input.request_overwrite, null, 2) }}</pre>
          <h4>header_overwrite</h4>
          <pre>{{ JSON.stringify(model.original.input.header_overwrite, null, 2) }}</pre>
          <h4>capabilities</h4>
          <pre>{{ JSON.stringify(model.original.input.capabilities, null, 2) }}</pre>
        </section>
        <p v-else class="meta">
          parameters、request_overwrite、header_overwrite 固定为空对象，reasoning_efforts
          固定为空数组。
        </p>
        <p v-if="model.fields.expected_version" role="alert">{{ model.fields.expected_version }}</p>
      </template>
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
          已观察到历史回执；这不确认完整原输入，也不会自动推进操作。
        </p>
        <p v-else-if="progress.observation === 'not_observed'">
          本次未观察到回执；这不是回滚或未执行证明。
        </p>
        <p v-else-if="progress.observation === 'failed'">本次查证失败，原请求状态保持。</p>
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
      <UiButton
        v-if="model.mode === 'edit'"
        variant="danger"
        :disabled="!canDelete"
        @click="page.openDelete('model')"
        >删除 Model</UiButton
      >
      <p
        v-if="
          !canSaveModel &&
          !blocked &&
          !pending &&
          !model.conflict &&
          !model.requiresRead &&
          model.supported &&
          canMutate
        "
        class="meta"
      >
        没有可保存的改动。
      </p>
    </form>
    <template #footer
      ><UiButton :disabled="blocked || pending" @click="page.closeModel()">取消</UiButton
      ><UiButton
        form="project-model-form"
        type="submit"
        variant="primary"
        :disabled="!canSaveModel"
        :state="buttonState"
        loading-label="保存中"
        success-label="已保存"
        >保存 Model</UiButton
      ></template
    >
  </UiDialog>
</template>
<style scoped>
.model-editor {
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
h4 {
  margin: 0;
  font-size: 13px;
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
.actions,
.choices {
  display: flex;
  gap: 8px 16px;
  flex-wrap: wrap;
  align-items: center;
}
.capabilities {
  display: grid;
  gap: 16px;
  min-width: 0;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
legend {
  padding: 0 4px;
  font-weight: 600;
}
.choice-group {
  display: grid;
  gap: 8px;
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.capacity-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.read-only-values {
  display: grid;
  gap: 12px;
  min-width: 0;
}
@media (max-width: 650px) {
  .facts,
  .capacity-fields {
    grid-template-columns: minmax(0, 1fr);
  }
  dt {
    margin-top: 4px;
  }
}
</style>
