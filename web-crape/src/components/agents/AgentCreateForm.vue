<script setup lang="ts">
import AgentSecretFields from './AgentSecretFields.vue'
import AgentRunnerMountFields from './AgentRunnerMountFields.vue'
import { computed, onMounted, ref, watch } from 'vue'
import { UiButton, UiCheckbox, UiField, UiInput, UiSelect, UiTextarea } from '../ui'
import type { ProjectAgents } from '../../composables/useProjectAgents'
const props = defineProps<{ owner: ProjectAgents; embedded?: boolean }>()
const emit = defineEmits<{ cancel: [] }>()
const form = ref<HTMLFormElement | null>(null)
const disabled = computed(
  () =>
    props.owner.formInitializing.value || props.owner.busy.value || !props.owner.canCreate.value,
)
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
  if (
    !focused &&
    !disabled.value &&
    props.owner.visible.value &&
    props.owner.draft.open &&
    input?.isConnected &&
    !input.disabled
  ) {
    input.focus()
    focused = true
  }
}
onMounted(focusName)
watch(disabled, focusName, { flush: 'post' })
</script>
<template>
  <form
    id="agent-create-form"
    ref="form"
    class="agent-create"
    aria-label="创建 Agent"
    @submit.prevent="owner.submitCreation()"
  >
    <h2 v-if="!embedded">创建空白 Agent</h2>
    <p>
      填写配置后明确创建。可选择 Runner、资源目录与 Secret Variable 白名单；Tool
      不在此额外授权；内置初始化选项由服务实际执行。
    </p>
    <UiField
      label="Agent 名称"
      required
      hint="3–32 位英文字母、数字或连字符，首尾为字母或数字。"
      v-slot="field"
    >
      <UiInput
        :id="field.id"
        v-model="owner.draft.name"
        :disabled="disabled"
        :aria-describedby="field.describedby"
        autocomplete="off"
        data-autofocus
      />
    </UiField>
    <div class="agent-create-pair">
      <UiField label="显示名称" v-slot="field"
        ><UiInput :id="field.id" v-model="owner.draft.displayName" :disabled="disabled"
      /></UiField>
      <UiField label="标签颜色" hint="可选，格式为 #rrggbb。" v-slot="field"
        ><UiInput
          :id="field.id"
          v-model="owner.draft.tagColor"
          :disabled="disabled"
          :aria-describedby="field.describedby"
      /></UiField>
    </div>
    <UiField label="Agent 描述" v-slot="field"
      ><UiTextarea :id="field.id" v-model="owner.draft.description" :disabled="disabled" rows="3"
    /></UiField>
    <UiField label="指令" v-slot="field"
      ><UiTextarea :id="field.id" v-model="owner.draft.instructions" :disabled="disabled" rows="5"
    /></UiField>
    <UiField label="主模型" required v-slot="field">
      <UiSelect
        :id="field.id"
        label="主模型"
        :model-value="owner.draft.modelID"
        :options="owner.modelOptions.value"
        :disabled="disabled"
        @update:model-value="owner.selectModel"
      />
    </UiField>
    <p v-if="owner.state.modelPhase === 'loading'" role="status">正在读取可用模型。</p>
    <p v-else-if="owner.state.modelPhase === 'ready' && !owner.state.models.length" role="status">
      当前没有可用聊天模型。请先在项目或系统模型设置中配置。
    </p>
    <p v-if="owner.state.modelMessage" role="alert">{{ owner.state.modelMessage }}</p>
    <div class="agent-create-actions">
      <UiButton type="button" :disabled="disabled" @click="owner.loadModels()"
        >刷新可用模型</UiButton
      >
      <UiButton
        v-if="owner.state.modelNext"
        type="button"
        :disabled="disabled"
        @click="owner.loadModels(true)"
        >加载更多模型</UiButton
      >
    </div>
    <UiField v-if="owner.effortOptions.value.length > 1" label="推理强度" v-slot="field">
      <UiSelect
        :id="field.id"
        label="推理强度"
        v-model="owner.draft.reasoningEffort"
        :options="owner.effortOptions.value"
        :disabled="disabled"
      />
    </UiField>
    <UiField label="AGENTS.md 注入" required v-slot="field">
      <UiSelect
        :id="field.id"
        label="AGENTS.md 注入"
        v-model="owner.draft.injectAgentsMD"
        :options="injectOptions"
        :disabled="disabled"
      />
    </UiField>
    <UiField label="审批策略" required v-slot="field">
      <UiSelect
        :id="field.id"
        label="审批策略"
        :model-value="owner.draft.approvalPolicy"
        :options="approvalOptions"
        :disabled="disabled"
        @update:model-value="owner.chooseApproval"
      />
    </UiField>
    <UiField v-if="owner.draft.approvalPolicy === 'auto'" label="审批模型" required v-slot="field">
      <UiSelect
        :id="field.id"
        label="审批模型"
        v-model="owner.draft.approvalModelID"
        :options="owner.modelOptions.value"
        :disabled="disabled"
      />
    </UiField>
    <AgentRunnerMountFields :owner="owner" />
    <AgentSecretFields :owner="owner" />
    <fieldset :disabled="disabled">
      <legend>内置初始化</legend>
      <UiCheckbox v-model="owner.draft.addSkills" :disabled="disabled">添加默认 Skills</UiCheckbox>
      <UiCheckbox v-model="owner.draft.installSkill" :disabled="disabled"
        >启用 Install Skill 工具</UiCheckbox
      >
    </fieldset>
    <p v-if="owner.draft.message" role="alert">{{ owner.draft.message }}</p>
    <div v-if="!embedded" class="agent-create-actions">
      <UiButton type="submit" variant="primary" :disabled="!owner.canSubmit.value"
        >确认创建 Agent</UiButton
      >
      <UiButton type="button" :disabled="disabled" @click="emit('cancel')">取消创建</UiButton>
    </div>
  </form>
</template>
<style scoped>
.agent-create {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.agent-create h2,
.agent-create p {
  margin: 0;
  overflow-wrap: anywhere;
}
.agent-create p {
  color: var(--muted);
}
.agent-create-pair {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space);
}
.agent-create-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.agent-create fieldset {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: var(--space);
}
@media (max-width: 640px) {
  .agent-create-pair {
    grid-template-columns: 1fr;
  }
}
</style>
