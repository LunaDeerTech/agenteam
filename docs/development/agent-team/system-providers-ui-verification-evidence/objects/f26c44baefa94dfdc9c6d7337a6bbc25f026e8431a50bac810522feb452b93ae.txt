<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import UiSelect from '../../components/ui/UiSelect.vue'
import { providerProtocols, type ProviderProtocol } from '../../api/system-providers'
import { useSystemProviders } from '../../composables/useSystemProviders'
const page = useSystemProviders()
const { draft, editor, state, progress, material, locked, blocked, feedback, confirmation } = page
const protocols = providerProtocols.map((value) => ({ value, label: value }))
const enabled = [
  { value: 'true', label: '启用' },
  { value: 'false', label: '禁用' },
]
const form = ref<HTMLFormElement | null>(null),
  materialInput = ref<HTMLTextAreaElement | null>(null)
let alive = true
function clearMaterialDOM() {
  if (materialInput.value) materialInput.value.value = ''
}
watch(() => material.value.revision, clearMaterialDOM, { flush: 'sync' })
onBeforeUnmount(() => {
  alive = false
  clearMaterialDOM()
})
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let current: HTMLElement | null = node; current; current = current.parentElement)
    if (current.inert || current.hidden || current.getAttribute('aria-hidden') === 'true')
      return false
  return true
}
async function perform(work: () => Promise<void>, event: Event) {
  const target = event instanceof SubmitEvent ? event.submitter : event.currentTarget
  await work()
  if (!alive || !form.value?.isConnected) return
  await nextTick()
  if (
    alive &&
    editor.mode &&
    !confirmation.open &&
    target instanceof HTMLElement &&
    operable(target)
  )
    target.focus()
}
</script>
<template>
  <UiDialog
    :open="editor.mode !== null"
    :title="editor.mode === 'create' ? '创建 Provider' : '编辑 Provider'"
    @update:open="!$event && page.closeEditor()"
  >
    <form ref="form" class="provider-editor" @submit.prevent="perform(page.save, $event)">
      <p class="meta">仅管理配置，不测试连接或调用外部服务。</p>
      <UiField v-slot="field" label="名称" required>
        <UiInput :id="field.id" v-model="draft.name" :disabled="locked" autocomplete="off" />
      </UiField>
      <UiField v-slot="field" label="协议" required hint="创建后协议不可修改。">
        <UiInput
          v-if="editor.mode === 'edit'"
          :id="field.id"
          :model-value="draft.protocol"
          readonly
          :aria-describedby="field.describedby"
        />
        <UiSelect
          v-else
          :id="field.id"
          :model-value="draft.protocol"
          :options="protocols"
          label="协议"
          :disabled="locked"
          @update:model-value="draft.protocol = $event as ProviderProtocol"
        />
      </UiField>
      <UiField
        v-slot="field"
        label="Base URL"
        required
        hint="输入完整 HTTP/HTTPS 地址；原文保存，不自动附加路径。"
      >
        <UiInput
          :id="field.id"
          v-model="draft.base_url"
          type="text"
          :disabled="locked"
          autocomplete="off"
          spellcheck="false"
          :aria-describedby="field.describedby"
        />
      </UiField>
      <UiField v-slot="field" label="启用状态" required>
        <UiSelect
          :id="field.id"
          :model-value="draft.enabled"
          :options="enabled"
          label="启用状态"
          :disabled="locked"
          @update:model-value="draft.enabled = $event as 'true' | 'false'"
        />
      </UiField>
      <p class="meta">当前无额外可配置选项。</p>
      <UiField
        v-slot="field"
        label="新凭据（仅写入）"
        :error="state.materialMessage"
        hint="留空保留现有凭据；填写后创建新凭据并替换引用。最多 65536 UTF-8 字节，内容不会自动修整。"
      >
        <!-- Deliberately no v-model/prop: material exists only in this DOM and
             the Session owner's private, non-reactive memory. -->
        <textarea
          :id="field.id"
          ref="materialInput"
          class="ui-input"
          rows="3"
          :disabled="locked || !!progress?.credential"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          :aria-invalid="field.invalid || undefined"
          :aria-describedby="field.describedby"
          @input="page.setMaterial(($event.target as HTMLTextAreaElement).value)"
        />
      </UiField>
      <p v-if="material.present" class="meta" role="status">
        已保留新凭据输入；不会回显保存的内容。
      </p>
      <p v-if="progress?.credential" role="status">
        新凭据已创建。{{
          progress.phase === 'confirmed'
            ? 'Provider 配置已确认。'
            : 'Provider尚未确认保存；不会再次创建这份凭据。'
        }}
      </p>
      <p v-if="progress?.phase === 'submitting'" role="status">
        {{ progress.stage === 'credential' ? '正在创建新凭据…' : '正在保存 Provider 配置…' }}
      </p>
      <p v-if="editor.currentVersion" class="meta">本次核对版本：{{ editor.currentVersion }}</p>
      <p v-if="editor.message" role="status">{{ editor.message }}</p>
      <p v-if="state.writeMessage" role="status">{{ state.writeMessage }}</p>
      <div v-if="progress?.phase === 'uncertain'" class="provider-actions">
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
      <div v-if="progress?.phase === 'rejected'" class="provider-actions">
        <UiButton
          v-if="progress.canRebase && !editor.reviewed"
          :disabled="blocked"
          @click="perform(page.reviewConflict, $event)"
          >读取当前配置并核对</UiButton
        >
        <UiButton variant="ghost" @click="page.abandonOperation">放弃本次操作</UiButton>
      </div>
      <div class="provider-actions">
        <UiButton
          type="submit"
          :disabled="locked || (!page.changed.value && !material.present && !editor.reviewed)"
          :state="feedback"
          success-label="配置已保存"
          >保存配置</UiButton
        >
        <UiButton variant="ghost" @click="page.closeEditor">取消</UiButton>
      </div>
    </form>
  </UiDialog>
</template>
<style scoped>
.provider-editor {
  display: grid;
  gap: 18px;
  min-width: 0;
}
.provider-editor p {
  overflow-wrap: anywhere;
}
.provider-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.provider-actions :deep(button) {
  max-width: 100%;
  white-space: normal;
}
.provider-editor :deep(.ui-select),
.provider-editor :deep(.ui-input) {
  min-width: 0;
  width: 100%;
}
</style>
