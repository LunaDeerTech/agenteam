<script setup lang="ts">
import { onMounted } from 'vue'
import type { MeetingReference } from '../../api/meetings'
import { taskLabels, taskStates } from '../../api/work-review'
import {
  useMeetingReferenceChoices,
  type MeetingReferenceChoice,
} from '../../composables/useMeetingReferenceChoices'
import { UiButton, UiInput, UiSelect } from '../ui'

const props = defineProps<{
  meetingId: string | null
  initialKind?: 'task' | 'knowledge' | 'file'
  disabled: boolean
  selected: readonly MeetingReference[]
}>()
const emit = defineEmits<{ choose: [reference: MeetingReference] }>()
const choices = useMeetingReferenceChoices(() => props.meetingId)
const { state } = choices
onMounted(() => {
  if (props.initialKind && !props.disabled) void choices.changeKind(props.initialKind)
})
const types = [
  { kind: 'task' as const, label: 'Task' },
  { kind: 'knowledge' as const, label: 'Knowledge' },
  { kind: 'file' as const, label: '文件' },
]
const states = taskStates.map((value) => ({ value, label: taskLabels[value] }))
const pinned = (item: MeetingReferenceChoice) =>
  props.selected.some((r) => r.kind === item.kind && r.id === item.id)
function select(item: MeetingReferenceChoice) {
  if (props.disabled || pinned(item)) return
  const reference = choices.reference(item)
  if (reference) emit('choose', reference)
}
</script>
<template>
  <section class="resource-picker" aria-label="添加项目资源引用">
    <div class="resource-types" role="group" aria-label="选择引用类型">
      <UiButton
        v-for="type in types"
        :key="type.kind"
        :aria-pressed="state.kind === type.kind"
        :disabled="disabled || state.phase === 'loading'"
        @click="choices.changeKind(type.kind)"
        >{{ type.label }}</UiButton
      >
    </div>
    <template v-if="state.kind">
      <div class="resource-context">
        <UiButton
          v-if="state.milestone || state.parents.length"
          variant="ghost"
          :disabled="disabled || state.phase === 'loading'"
          @click="choices.back"
          >返回上一级</UiButton
        >
        <span v-if="state.kind === 'task'">{{
          state.sprint?.title || state.milestone?.title || '选择里程碑'
        }}</span>
        <span v-else-if="state.kind === 'knowledge'">{{
          state.parents.at(-1)?.title || '项目文档'
        }}</span>
      </div>
      <label v-if="state.kind === 'task' && state.sprint" class="resource-filter"
        >Task 状态<UiSelect
          :model-value="state.taskState"
          label="筛选 Task 状态"
          :options="states"
          :disabled="disabled || state.phase === 'loading'"
          @update:model-value="choices.changeTaskState"
      /></label>
      <form v-if="state.kind === 'file'" class="resource-filter" @submit.prevent="choices.load()">
        <label
          >文件名称<UiInput
            v-model="state.fileName"
            maxlength="255"
            :disabled="disabled || state.phase === 'loading'"
        /></label>
        <UiButton type="submit" :disabled="disabled || state.phase === 'loading'"
          >查找文件</UiButton
        >
      </form>
      <p v-if="state.phase === 'loading'" class="muted" role="status">正在读取资源…</p>
      <p v-if="state.message" class="notice" role="status">{{ state.message }}</p>
      <UiButton v-if="state.phase === 'error'" :disabled="disabled" @click="choices.load()"
        >重新读取资源</UiButton
      >
      <p v-if="state.phase === 'ready' && !state.items.length" class="muted">此范围内暂无资源。</p>
      <ul class="resource-items">
        <li v-for="item in state.items" :key="item.kind + ':' + item.id">
          <span>{{ item.title }}</span>
          <UiButton
            v-if="item.kind === 'milestone' || item.kind === 'sprint'"
            variant="ghost"
            :aria-label="'打开 ' + item.title"
            :disabled="disabled || state.phase !== 'ready'"
            @click="choices.browse(item)"
            >打开</UiButton
          >
          <template v-else>
            <UiButton
              v-if="item.kind === 'knowledge'"
              variant="ghost"
              :aria-label="'查看子文档 ' + item.title"
              :disabled="disabled || state.phase !== 'ready'"
              @click="choices.browse(item)"
              >子文档</UiButton
            >
            <UiButton
              variant="ghost"
              :aria-label="(pinned(item) ? '已选择 ' : '引用 ') + item.title"
              :disabled="disabled || state.phase !== 'ready' || pinned(item)"
              @click="select(item)"
              >{{ pinned(item) ? '已选择' : '引用' }}</UiButton
            >
          </template>
        </li>
      </ul>
      <UiButton
        v-if="state.next"
        :disabled="disabled || state.phase !== 'ready'"
        @click="choices.load(true)"
        >更多资源</UiButton
      >
    </template>
  </section>
</template>
<style scoped>
.resource-picker {
  margin: 16px 0;
  padding: 12px 0;
  border-block: 1px solid var(--border);
}
.resource-types,
.resource-context,
.resource-filter {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.resource-context,
.resource-filter {
  margin-top: 8px;
}
.resource-filter label {
  display: grid;
  gap: 6px;
}
.resource-items {
  display: grid;
  gap: 6px;
  padding: 0;
  list-style: none;
  max-height: 240px;
  overflow-y: auto;
}
.resource-items li {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}
.resource-items li > span {
  min-width: 0;
  flex: 1 1 140px;
  overflow-wrap: anywhere;
}
.muted {
  color: var(--muted);
}
.notice {
  color: var(--warning);
}
</style>
