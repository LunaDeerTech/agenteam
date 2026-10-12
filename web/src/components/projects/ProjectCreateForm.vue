<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiField, UiInput, UiTextarea } from '../ui'
import type { ProjectCreation } from '../../composables/useProjectCreation'
const props = defineProps<{ owner: ProjectCreation }>()
const emit = defineEmits<{ close: [] }>()
const progress = computed(() => props.owner.progress.value)
const reasons = {
  dependency_unbound: '创建所需服务暂未准备好。',
  dependency_unavailable: '创建所需服务暂时不可用。',
  work_pending: '初始化仍有工作等待完成。',
  outcome_unknown: '初始化结果尚未确认。',
  operation_failed: '初始化未完成。',
}
const reason = computed(() => {
  const result = progress.value?.result
  return result?.state === 'accepted' && result.operation.safe_reason
    ? reasons[result.operation.safe_reason]
    : ''
})
</script>
<template>
  <section class="project-create" aria-labelledby="project-create-heading">
    <h2 id="project-create-heading" tabindex="-1">新建项目</h2>
    <form v-if="owner.editable.value" @submit.prevent="owner.act('create')">
      <UiField
        v-slot="field"
        label="项目名称"
        required
        hint="名称用于项目地址，可包含字母、数字、点、下划线和连字符。"
      >
        <UiInput
          :id="field.id"
          v-model="owner.draft.name"
          :aria-describedby="field.describedby"
          required
          maxlength="64"
          autocomplete="off"
          :readonly="owner.blocked.value"
        />
      </UiField>
      <UiField v-slot="field" label="项目描述">
        <UiTextarea
          :id="field.id"
          v-model="owner.draft.description"
          :readonly="owner.blocked.value"
          rows="4"
        />
      </UiField>
      <p v-if="owner.message.value" role="alert">{{ owner.message.value }}</p>
      <UiButton type="submit" variant="primary" :disabled="owner.blocked.value"
        >确认创建项目</UiButton
      >
    </form>
    <template v-else-if="progress">
      <p v-if="progress.phase === 'submitting'" role="status">
        正在创建“{{ progress.name }}”，请等待原请求返回。
      </p>
      <p v-else-if="progress.phase === 'ready'" role="status">项目已完成初始化，可以进入使用。</p>
      <p v-else-if="progress.phase === 'failed'" role="alert">
        项目初始化未完成。{{ reason }} 可使用原创建请求继续。
      </p>
      <p v-else-if="progress.phase === 'accepted'" role="status">
        创建已被接受，项目尚未准备好。{{ reason }}
      </p>
      <p v-else role="status">创建结果尚未确认。请查询状态或显式恢复原创建请求，不会自动重发。</p>
      <p v-if="owner.message.value" role="alert">{{ owner.message.value }}</p>
      <div class="creation-actions">
        <RouterLink v-if="owner.projectPath.value" :to="owner.projectPath.value"
          >进入项目</RouterLink
        >
        <UiButton
          v-if="progress.phase === 'ready'"
          :disabled="owner.blocked.value"
          @click="owner.another()"
          >再建一个项目</UiButton
        >
        <template v-else-if="progress.phase !== 'submitting'">
          <UiButton
            v-if="progress.result?.state === 'accepted'"
            :disabled="owner.blocked.value || !progress.contextValid"
            @click="owner.act('check')"
            >查询创建状态</UiButton
          >
          <UiButton
            :disabled="owner.blocked.value || !progress.canResume"
            @click="owner.act('resume')"
            >继续原创建</UiButton
          >
        </template>
      </div>
    </template>
    <UiButton :disabled="owner.blocked.value" @click="emit('close')">关闭创建面板</UiButton>
  </section>
</template>
<style scoped>
.project-create,
form {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
}
.project-create {
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
}
.project-create h2 {
  margin: 0;
}
.project-create p {
  overflow-wrap: anywhere;
}
.creation-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
  align-items: center;
}
</style>
