<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { UiButton, UiDialog, UiField, UiInput, UiTextarea } from '../ui'
import type { ProjectCreation } from '../../composables/useProjectCreation'
const props = defineProps<{ owner: ProjectCreation; fallbackFocus?: HTMLElement | null }>()
const open = computed({
  get: () => props.owner.open.value,
  set: (value) => {
    if (!value) props.owner.close()
  },
})
const formID = useId()
const form = ref<HTMLFormElement | null>(null)
const status = ref<HTMLElement | null>(null)
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
watch(
  () => props.owner.editable.value,
  async () => {
    await nextTick()
    if (!open.value) return
    if (props.owner.editable.value) form.value?.querySelector('input')?.focus()
    else status.value?.focus()
  },
)
</script>
<template>
  <UiDialog
    v-model:open="open"
    title="新建项目"
    :fallback-focus="fallbackFocus"
    :close-on-outside="false"
    :close-on-escape="!owner.blocked.value"
  >
    <form
      v-if="owner.editable.value"
      :id="formID"
      ref="form"
      class="project-create-form"
      @submit.prevent="owner.act('create')"
    >
      <p class="creation-description">为项目起一个名称，之后可以在项目设置中修改。</p>
      <UiField
        v-slot="field"
        label="项目名称"
        required
        hint="用于项目地址，可包含字母、数字、点、下划线和连字符。"
      >
        <UiInput
          :id="field.id"
          v-model="owner.draft.name"
          :aria-describedby="field.describedby"
          data-autofocus
          required
          maxlength="64"
          autocomplete="off"
          placeholder="例如 website-redesign"
          :readonly="owner.blocked.value"
        />
      </UiField>
      <UiField v-slot="field" label="项目描述" hint="可选，简要说明项目的目标或用途。">
        <UiTextarea
          :id="field.id"
          v-model="owner.draft.description"
          :aria-describedby="field.describedby"
          :readonly="owner.blocked.value"
          placeholder="这个项目要完成什么？"
          rows="4"
        />
      </UiField>
      <p v-if="owner.message.value" class="creation-error" role="alert">
        {{ owner.message.value }}
      </p>
    </form>
    <div v-else-if="progress" ref="status" class="creation-status" tabindex="-1" data-autofocus>
      <p class="creation-project-name">{{ progress.name }}</p>
      <p v-if="progress.phase === 'submitting'" role="status">正在创建项目，请等待请求完成。</p>
      <p v-else-if="progress.phase === 'ready'" role="status">项目已完成初始化，可以进入使用。</p>
      <p v-else-if="progress.phase === 'failed'" role="alert">
        项目初始化未完成。{{ reason }} 可继续本次创建。
      </p>
      <p v-else-if="progress.phase === 'accepted'" role="status">
        创建已被接受，项目尚未准备好。{{ reason }}
      </p>
      <p v-else role="status">创建结果尚未确认。请查询状态或继续原创建，避免重复创建项目。</p>
      <p v-if="owner.message.value" class="creation-error" role="alert">
        {{ owner.message.value }}
      </p>
    </div>
    <template #footer>
      <UiButton :disabled="owner.blocked.value" @click="owner.close()">{{
        owner.editable.value ? '取消' : '返回项目列表'
      }}</UiButton>
      <UiButton
        v-if="owner.editable.value"
        type="submit"
        :form="formID"
        variant="primary"
        :disabled="owner.blocked.value"
        >确认创建项目</UiButton
      >
      <template v-else-if="progress">
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
            variant="primary"
            :disabled="owner.blocked.value || !progress.canResume"
            @click="owner.act('resume')"
            >继续原创建</UiButton
          >
        </template>
        <RouterLink
          v-if="owner.projectPath.value"
          class="creation-enter"
          :to="owner.projectPath.value"
          >进入项目</RouterLink
        >
      </template>
    </template>
  </UiDialog>
</template>
<style scoped>
.project-create-form,
.creation-status {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.creation-description {
  color: var(--muted);
}
.creation-project-name {
  font-weight: 600;
}
.creation-status {
  padding-block: 8px;
}
.creation-status p,
.creation-error {
  overflow-wrap: anywhere;
}
.creation-error {
  padding: 12px;
  border: 1px solid var(--danger);
  border-radius: var(--radius);
  color: var(--danger);
  background: var(--danger-bg);
}
.creation-enter {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: var(--control-height);
  padding: 4px 12px;
  border: 1px solid transparent;
  border-radius: var(--radius);
  color: var(--action-text);
  background: var(--action-fill);
  font-weight: 500;
}
.creation-enter:hover {
  text-decoration: none;
  filter: brightness(1.08);
}
</style>
