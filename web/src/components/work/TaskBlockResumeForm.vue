<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiCheckbox, UiField, UiSelect, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks }>()
defineEmits<{ submit: []; cancel: [] }>()
const marking = computed(() => props.owner.draft.action === 'blocked')
const label = computed(() => (marking.value ? '标记受阻' : '解决阻塞并恢复待执行'))
const disabled = computed(
  () =>
    props.owner.busy.value ||
    !props.owner.canManageTaskBlocking.value ||
    props.owner.blockingStale.value,
)
const agentOptions = computed(() => [
  {
    value: '',
    label: props.owner.state.task?.assignee_agent_id
      ? `保留当前负责人：${props.owner.name(props.owner.state.task.assignee_agent_id)}`
      : '请选择执行 Agent',
  },
  ...props.owner.agentOptions.value.filter((item) => item.value !== ''),
])
</script>
<template>
  <form class="block-resume-form" :aria-label="label" @submit.prevent="$emit('submit')">
    <h3>{{ label }}</h3>
    <p v-if="marking">
      确认根据现有未解决的阻塞将任务标记为受阻？当前负责人将保留；仍有执行或待派发操作时无法提交。
    </p>
    <p v-else>
      明确选择本次解决的全部未解决阻塞。解除阻塞与恢复待执行会一起提交，任何条件不满足都不会部分生效。
    </p>
    <p v-if="owner.blockingStale.value" role="alert">
      任务已变化，请选择“保留当前状态”，重新读取并核对后再操作。
    </p>
    <p v-if="owner.state.blockerPhase !== 'ready'" role="status">请先重新读取阻塞与依赖。</p>
    <p v-else-if="!owner.unresolvedBlockers.value.length" role="status">当前没有未解决的阻塞。</p>
    <template v-if="!marking">
      <fieldset :disabled="disabled">
        <legend>本次解决的阻塞</legend>
        <UiCheckbox
          v-for="blocker in owner.unresolvedBlockers.value"
          :key="blocker.id"
          :model-value="owner.draft.resolveBlockerIDs.includes(blocker.id)"
          @update:model-value="owner.selectBlockerResolution(blocker.id, $event)"
        >
          {{
            blocker.type === 'technical'
              ? '技术阻塞'
              : blocker.type === 'rely_on'
                ? '任务依赖'
                : '等待人工处理'
          }}：{{ blocker.description }}
        </UiCheckbox>
      </fieldset>
      <p v-if="owner.state.blockerNext" role="status">
        请在“阻塞与依赖”中读取全部记录后，再确认本次解除。
      </p>
      <p v-if="owner.unresolvedBlockers.value.length > 16" role="status">
        一次最多解决 16 项。请先明确解除部分可单独处理的阻塞，保留至少一项，再重新读取。
      </p>
      <UiField label="恢复后的执行 Agent" v-slot="field">
        <UiSelect
          :id="field.id"
          v-model="owner.draft.agentID"
          label="恢复后的执行 Agent"
          :options="agentOptions"
          :disabled="disabled"
        />
      </UiField>
      <p v-if="owner.state.agentMessage" role="alert">{{ owner.state.agentMessage }}</p>
      <UiButton v-if="owner.state.agentNext" :disabled="disabled" @click="owner.loadAgents(true)"
        >更多 Agent</UiButton
      >
      <UiButton v-if="owner.state.agentMessage" :disabled="disabled" @click="owner.loadAgents()"
        >重新读取 Agent 目录</UiButton
      >
    </template>
    <UiField label="状态变更说明" hint="可选，填写后将作为任务历史保留。" v-slot="field">
      <UiTextarea
        :id="field.id"
        v-model="owner.draft.comment"
        :aria-describedby="field.describedby"
        :disabled="disabled"
        rows="3"
      />
    </UiField>
    <div class="actions">
      <UiButton type="submit" :disabled="!owner.canSubmitTaskBlocking.value"
        >确认{{ label }}</UiButton
      >
      <UiButton :disabled="owner.busy.value || !!owner.pending.value" @click="$emit('cancel')"
        >保留当前状态</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.block-resume-form,
fieldset {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
  min-width: 0;
}
.block-resume-form h3,
.block-resume-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
fieldset {
  margin: 0;
  border: 1px solid var(--border);
  padding: var(--space);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
