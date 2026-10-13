<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiButton, UiField, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const form = ref<HTMLFormElement | null>(null)
const adding = computed(() => props.owner.humanBlocker.mode === 'add')
const label = computed(() => (adding.value ? '添加人工阻塞' : '解除人工阻塞'))
const disabled = computed(
  () =>
    props.owner.busy.value ||
    !props.owner.canManageHumanBlockers.value ||
    props.owner.humanBlockerStale.value,
)
onMounted(() => form.value?.querySelector('textarea')?.focus())
</script>
<template>
  <form ref="form" class="human-blocker-form" :aria-label="label" @submit.prevent="emit('submit')">
    <h4>{{ label }}</h4>
    <p>仅更新人工阻塞记录。任务状态、指派和排序保持不变。</p>
    <p v-if="!adding && owner.state.task?.state === 'blocked'">受阻任务的最后一项阻塞需要与任务恢复一并处理，不能在这里单独解除。</p>
    <UiField v-if="adding" label="阻塞说明" hint="最多 1024 UTF-8 字节，可以留空。" v-slot="field">
      <UiTextarea
        :id="field.id"
        v-model="owner.humanBlocker.description"
        :aria-describedby="field.describedby"
        :disabled="disabled"
        rows="4"
      />
    </UiField>
    <UiField
      v-else
      label="解除说明"
      hint="留空表示无备注；填写时必须包含非空白字符，最多 1024 UTF-8 字节。"
      v-slot="field"
    >
      <UiTextarea
        :id="field.id"
        v-model="owner.humanBlocker.resolutionComment"
        :aria-describedby="field.describedby"
        :disabled="disabled"
        rows="4"
      />
    </UiField>
    <p v-if="owner.humanBlockerStale.value" role="alert">
      任务已有变化。请重新读取并核对，取消本次操作后重新填写；不会自动采用新版本。
    </p>
    <p v-if="owner.humanBlocker.message" role="alert">{{ owner.humanBlocker.message }}</p>
    <div class="human-blocker-actions">
      <UiButton type="submit" :disabled="disabled">{{
        adding ? '确认添加人工阻塞' : '确认解除人工阻塞'
      }}</UiButton>
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.pending.value"
        @click="emit('cancel')"
        >取消人工阻塞操作</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.human-blocker-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.human-blocker-form h4,
.human-blocker-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.human-blocker-form p {
  color: var(--muted);
}
.human-blocker-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
