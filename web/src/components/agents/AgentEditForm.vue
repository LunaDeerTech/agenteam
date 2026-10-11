<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { UiButton, UiField, UiInput, UiSelect, UiTextarea } from '../ui'
import type { ProjectAgents } from '../../composables/useProjectAgents'
const props = defineProps<{ owner: ProjectAgents }>()
const emit = defineEmits<{ cancel: [] }>()
const form = ref<HTMLFormElement | null>(null)
const disabled = computed(
  () => props.owner.busy.value || !props.owner.canEdit.value || props.owner.editStale.value,
)
const policyDisabled = computed(() => disabled.value || props.owner.state.modelPhase !== 'ready' ||
  props.owner.state.configuration?.reasoning_effort !== null)
const approvalOptions = [
  { value: 'default', label: '默认审批' },
  { value: 'auto', label: '模型自动审批' },
  { value: 'allow', label: '允许工具调用' },
]
const injectOptions = [
  { value: 'yes', label: '注入 AGENTS.md' },
  { value: 'no', label: '不注入 AGENTS.md' },
]
let focused = false
function focusName() {
  const input = form.value?.querySelector('input')
  if (!focused && input && !input.disabled) {
    input.focus()
    focused = true
  }
}
onMounted(focusName)
watch(disabled, focusName, { flush: 'post' })
</script>
<template>
  <form
    ref="form"
    class="agent-edit-form"
    aria-label="编辑 Agent 配置"
    @submit.prevent="owner.submitEdit()"
  >
    <h2>编辑 Agent 配置</h2>
    <p>基于版本 {{ owner.edit.version }}，只保存本次编辑的字段。描述和指令可以明确清空。</p>
    <UiField label="Agent 名称" required v-slot="field">
      <UiInput
        :id="field.id"
        :model-value="owner.edit.name"
        :disabled="disabled"
        @update:model-value="owner.touchEdit('name', $event)"
      />
    </UiField>
    <UiField label="Agent 描述" v-slot="field">
      <UiTextarea
        :id="field.id"
        :model-value="owner.edit.description"
        :disabled="disabled"
        rows="4"
        @update:model-value="owner.touchEdit('description', $event)"
      />
    </UiField>
    <UiField label="指令" v-slot="field">
      <UiTextarea
        :id="field.id"
        :model-value="owner.edit.instructions"
        :disabled="disabled"
        rows="6"
        @update:model-value="owner.touchEdit('instructions', $event)"
      />
    </UiField>
    <UiField label="主模型" required v-slot="field">
      <UiSelect
        :id="field.id"
        label="主模型"
        :model-value="owner.edit.modelID"
        :options="owner.editModelOptions.value"
        :disabled="policyDisabled"
        @update:model-value="owner.touchEditPolicy('model_ref', $event)"
      />
    </UiField>
    <p>可选择当前受支持的文本聊天模型。保存后用于后续执行，已有执行的配置不变。</p>
    <p v-if="owner.state.modelPhase === 'loading'" role="status">正在读取可用模型。</p>
    <p v-if="owner.state.modelMessage" role="alert">{{ owner.state.modelMessage }}</p>
    <p v-if="owner.editPolicyMessage.value" role="status">{{ owner.editPolicyMessage.value }}</p>
    <div class="agent-edit-actions">
      <UiButton type="button" :disabled="disabled" @click="owner.loadModels()">刷新可用模型</UiButton>
      <UiButton v-if="owner.state.modelNext" type="button" :disabled="disabled" @click="owner.loadModels(true)">加载更多模型</UiButton>
    </div>
    <UiField label="AGENTS.md 注入" required v-slot="field">
      <UiSelect
        :id="field.id"
        label="AGENTS.md 注入"
        :model-value="owner.edit.injectAgentsMD"
        :options="injectOptions"
        :disabled="disabled"
        @update:model-value="owner.touchEditPolicy('inject_agents_md', $event)"
      />
    </UiField>
    <UiField label="审批策略" required v-slot="field">
      <UiSelect
        :id="field.id"
        label="审批策略"
        :model-value="owner.edit.approvalPolicy"
        :options="approvalOptions"
        :disabled="policyDisabled"
        @update:model-value="owner.touchEditPolicy('approval_policy', $event)"
      />
    </UiField>
    <UiField v-if="owner.edit.approvalPolicy === 'auto'" label="审批模型" required v-slot="field">
      <UiSelect
        :id="field.id"
        label="审批模型"
        :model-value="owner.edit.approvalModelID"
        :options="owner.editApprovalOptions.value"
        :disabled="policyDisabled"
        @update:model-value="owner.touchEditPolicy('approval_model_ref', $event)"
      />
    </UiField>
    <p v-if="owner.editStale.value" role="alert">
      配置已有新版本。请取消编辑并重新读取，核对后再保存；不会自动替换原版本。
    </p>
    <p v-if="owner.edit.message" role="alert">{{ owner.edit.message }}</p>
    <div class="agent-edit-actions">
      <UiButton type="submit" :disabled="!owner.canSave.value">保存配置</UiButton>
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.pending.value"
        @click="emit('cancel')"
        >取消编辑</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.agent-edit-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.agent-edit-form h2,
.agent-edit-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.agent-edit-form p {
  color: var(--muted);
}
.agent-edit-actions {
  display: flex;
  gap: var(--content-gap);
  flex-wrap: wrap;
}
</style>
