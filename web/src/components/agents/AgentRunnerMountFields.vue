<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { UiButton, UiField, UiInput, UiSelect } from '../ui'
import type { ProjectAgents } from '../../composables/useProjectAgents'
const props = defineProps<{ owner: ProjectAgents; editing?: boolean }>()
const fields = ref<HTMLElement | null>(null)
const addButton = ref<{ $el: HTMLButtonElement } | null>(null)
const rows = computed(() =>
  props.editing ? props.owner.edit.runnerMounts : props.owner.draft.runnerMounts,
)
const disabled = computed(
  () =>
    props.owner.busy.value ||
    !props.owner.canCreate.value ||
    (props.editing && (!props.owner.editMountsReady.value || props.owner.editStale.value)),
)
const message = computed(() =>
  props.editing ? props.owner.editMountMessage.value : props.owner.draftMountMessage.value,
)
function options(id: string) {
  const choices = props.owner.runnerOptions.value
  return id && !choices.some((choice) => choice.value === id)
    ? [{ value: id, label: `已配置 Runner · ${id}` }, ...choices]
    : choices
}
function change(index: number, key: 'runner_id' | 'workspace', value: string) {
  props.owner.changeMounts(
    !!props.editing,
    rows.value.map((row, n) => (n === index ? { ...row, [key]: value } : row)),
  )
}
async function add() {
  if (disabled.value || rows.value.length >= 128) return
  props.owner.changeMounts(!!props.editing, [...rows.value, { runner_id: '', workspace: '' }])
  await nextTick()
  if (!disabled.value)
    fields.value
      ?.querySelectorAll<HTMLElement>('button[aria-haspopup="listbox"]')
      .item(rows.value.length - 1)
      ?.focus()
}
async function remove(index: number) {
  if (disabled.value) return
  props.owner.changeMounts(
    !!props.editing,
    rows.value.filter((_, n) => n !== index),
  )
  await nextTick()
  if (!disabled.value) addButton.value?.$el.focus()
}
</script>
<template>
  <section ref="fields" class="agent-runner-fields" aria-label="Runner 与资源目录">
    <h3>Runner 与资源目录</h3>
    <p>选择此 Agent 可用的系统 Runner，并为每项填写资源目录名。移除配置不会删除目录。</p>
    <p v-if="editing && !owner.editMountsReady.value" role="status">
      {{ owner.state.mountsMessage || '正在核对当前 Runner 配置与 Agent 版本。' }}
    </p>
    <template v-else>
      <p v-if="!rows.length">未配置 Runner。</p>
      <fieldset v-for="(row, n) in rows" :key="n" :disabled="disabled" class="runner-row">
        <legend>资源目录 {{ n + 1 }}</legend>
        <UiField :label="`Runner ${n + 1}`" required v-slot="field">
          <UiSelect
            :id="field.id"
            :label="`Runner ${n + 1}`"
            :model-value="row.runner_id"
            :options="options(row.runner_id)"
            :disabled="disabled"
            @update:model-value="change(n, 'runner_id', $event)"
          />
        </UiField>
        <p
          v-if="owner.state.runners.find((r) => r.id === row.runner_id)?.description"
          class="runner-description"
        >
          {{ owner.state.runners.find((r) => r.id === row.runner_id)?.description }}
        </p>
        <UiField
          :label="`资源目录名 ${n + 1}`"
          required
          hint="单段英文目录名，例如 workspace-1；不填写路径。"
          v-slot="field"
        >
          <UiInput
            :id="field.id"
            :model-value="row.workspace"
            :disabled="disabled"
            :aria-describedby="field.describedby"
            autocomplete="off"
            spellcheck="false"
            @update:model-value="change(n, 'workspace', $event)"
          />
        </UiField>
        <UiButton type="button" variant="ghost" :disabled="disabled" @click="remove(n)"
          >移除资源目录 {{ n + 1 }}</UiButton
        >
      </fieldset>
      <UiButton
        ref="addButton"
        type="button"
        :disabled="disabled || rows.length >= 128"
        @click="add"
        >添加资源目录</UiButton
      >
      <p v-if="message" role="alert">{{ message }}</p>
    </template>
    <p v-if="owner.state.runnerPhase === 'loading'" role="status">正在读取系统 Runner。</p>
    <p v-else-if="owner.state.runnerPhase === 'ready' && !owner.state.runners.length" role="status">
      当前没有可选 Runner，可保留空配置。
    </p>
    <p>离线或协议不兼容的 Runner 仍可保存为配置；当前状态不保证任务可立即执行。</p>
    <p v-if="owner.state.runnerMessage" role="alert">{{ owner.state.runnerMessage }}</p>
    <div class="runner-actions">
      <UiButton
        type="button"
        :disabled="owner.busy.value || !owner.canCreate.value"
        @click="owner.loadRunners()"
        >刷新 Runner</UiButton
      >
      <UiButton
        v-if="owner.state.runnerNext"
        type="button"
        :disabled="owner.busy.value || !owner.canCreate.value"
        @click="owner.loadRunners(true)"
        >加载更多 Runner</UiButton
      >
      <UiButton
        v-if="editing && owner.state.selectedID && owner.state.mountsPhase === 'error'"
        type="button"
        :disabled="owner.busy.value || !owner.visible.value"
        @click="owner.loadConfiguration(owner.state.selectedID)"
        >重新读取 Runner 配置</UiButton
      >
    </div>
  </section>
</template>
<style scoped>
.agent-runner-fields,
.runner-row {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
  min-width: 0;
}
.agent-runner-fields h3,
.agent-runner-fields p {
  margin: 0;
  overflow-wrap: anywhere;
}
.runner-row {
  margin: 0;
  padding: var(--content-gap);
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.runner-description {
  white-space: pre-wrap;
  color: var(--muted);
}
.runner-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
