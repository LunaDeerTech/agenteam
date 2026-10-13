<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { UiButton, UiField, UiInput, UiSelect, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const form = ref<HTMLFormElement | null>(null)
const adding = computed(() => props.owner.dependency.mode === 'add')
const disabled = computed(
  () =>
    props.owner.busy.value ||
    !props.owner.canManageDependencies.value ||
    props.owner.dependencyStale.value,
)
const label = computed(() => (adding.value ? '添加任务依赖' : '解除任务依赖'))
async function search(more = false) {
  await props.owner.loadDependencyCandidates(more)
  await nextTick()
  if (props.owner.dependency.open && !props.owner.busy.value)
    form.value?.querySelector<HTMLButtonElement>('button[aria-haspopup="listbox"]')?.focus()
}
onMounted(() =>
  form.value?.querySelector<HTMLInputElement | HTMLTextAreaElement>('input,textarea')?.focus(),
)
</script>
<template>
  <form ref="form" class="dependency-form" :aria-label="label" @submit.prevent="emit('submit')">
    <h4>{{ label }}</h4>
    <p>依赖需要明确解除。关联任务已完成或已取消时，也不会自动解除新建依赖。</p>
    <p v-if="!adding && owner.state.task?.state === 'blocked'">受阻任务的最后一项阻塞需要与任务恢复一并处理，不能在这里单独解除。</p>
    <template v-if="adding">
      <UiField
        label="查找关联任务"
        hint="从当前项目查找，可选择其他 Sprint 的任务。"
        v-slot="field"
      >
        <UiInput
          :id="field.id"
          v-model="owner.dependencyCandidates.text"
          :disabled="disabled"
          :aria-describedby="field.describedby"
          @keydown.enter.prevent="search()"
        />
      </UiField>
      <UiButton type="button" :disabled="disabled" @click="search()">查找任务</UiButton>
      <UiField label="关联任务" required v-slot="field">
        <UiSelect
          :id="field.id"
          v-model="owner.dependency.relatedTaskID"
          label="关联任务"
          :options="owner.dependencyOptions.value"
          :disabled="disabled"
        />
      </UiField>
      <p v-if="owner.dependencyCandidates.phase === 'loading'" role="status">正在读取候选任务。</p>
      <p
        v-else-if="
          owner.dependencyCandidates.phase === 'ready' && !owner.dependencyOptions.value.length
        "
      >
        没有符合条件的关联任务。
      </p>
      <p v-if="owner.dependencyCandidates.message" role="alert">
        {{ owner.dependencyCandidates.message }}
      </p>
      <UiButton
        v-if="owner.dependencyCandidates.next"
        type="button"
        :disabled="disabled"
        @click="search(true)"
        >更多候选任务</UiButton
      >
      <UiField label="依赖原因" hint="最多 1024 UTF-8 字节，可以留空。" v-slot="field">
        <UiTextarea
          :id="field.id"
          v-model="owner.dependency.description"
          :disabled="disabled"
          :aria-describedby="field.describedby"
          rows="3"
        />
      </UiField>
    </template>
    <template v-else>
      <p>关联任务：{{ owner.dependency.relatedTaskID }}</p>
      <UiField
        label="解除说明"
        hint="留空表示无备注；填写时须包含非空白字符，最多 1024 UTF-8 字节。"
        v-slot="field"
      >
        <UiTextarea
          :id="field.id"
          v-model="owner.dependency.resolutionComment"
          :disabled="disabled"
          :aria-describedby="field.describedby"
          rows="3"
        />
      </UiField>
    </template>
    <p v-if="owner.dependencyStale.value" role="alert">
      请重新读取任务并核对，取消本次操作后重新填写；不会自动采用新版本。
    </p>
    <p v-if="owner.dependency.message" role="alert">{{ owner.dependency.message }}</p>
    <div class="dependency-actions">
      <UiButton type="submit" :disabled="disabled || (adding && !owner.dependency.relatedTaskID)">{{
        adding ? '添加依赖' : '解除依赖'
      }}</UiButton>
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.pending.value"
        @click="emit('cancel')"
        >取消</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.dependency-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.dependency-form h4,
.dependency-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.dependency-form p {
  color: var(--muted);
}
.dependency-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
