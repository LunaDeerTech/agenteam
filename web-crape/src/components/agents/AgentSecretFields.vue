<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { UiButton, UiField, UiSelect } from '../ui'
import type { ProjectAgents } from '../../composables/useProjectAgents'

const props = defineProps<{ owner: ProjectAgents; editing?: boolean }>()
const choice = ref('')
const fields = ref<HTMLElement | null>(null)
const selected = computed(() =>
  props.editing ? props.owner.edit.secretIDs : props.owner.draft.secretIDs,
)
const disabled = computed(
  () =>
    props.owner.busy.value ||
    props.owner.formInitializing.value ||
    !props.owner.canCreate.value ||
    (props.editing && (!props.owner.canEdit.value || props.owner.editStale.value)),
)
const options = computed(() =>
  props.owner.secretOptions.value.filter((option) => !selected.value.includes(option.value)),
)
const addDisabled = computed(
  () => disabled.value || props.owner.state.secretPhase !== 'ready' || selected.value.length >= 128,
)
const message = computed(() =>
  props.editing ? props.owner.editSecretMessage.value : props.owner.draftSecretMessage.value,
)
async function add() {
  if (addDisabled.value || !options.value.some((option) => option.value === choice.value)) return
  props.owner.changeSecrets(!!props.editing, [...selected.value, choice.value])
  choice.value = ''
  await focusChoice()
}
async function remove(id: string) {
  if (disabled.value) return
  props.owner.changeSecrets(
    !!props.editing,
    selected.value.filter((value) => value !== id),
  )
  await focusChoice()
}
async function focusChoice() {
  await nextTick()
  if (!disabled.value && props.owner.visible.value)
    fields.value
      ?.querySelector<HTMLElement>(
        addDisabled.value ? '[data-read-secrets]' : '[aria-haspopup="listbox"]',
      )
      ?.focus()
}
</script>

<template>
  <section ref="fields" class="agent-secret-fields" aria-label="Secret Variable 白名单">
    <h3>Secret Variable 白名单</h3>
    <p>选择此 Agent 可引用的项目 Secret Variable。这里只显示名称、说明与 ID，不显示变量值。</p>
    <p v-if="!selected.length">白名单为空。</p>
    <ul v-else class="selected-secrets">
      <li v-for="id in selected" :key="id">
        <strong>{{ owner.secretLabel(id) }}</strong>
        <span v-if="owner.secretLabel(id) !== id" class="secret-id">{{ id }}</span>
        <p
          v-if="owner.state.secrets.find((secret) => secret.id === id)?.description"
          class="secret-description"
        >
          {{ owner.state.secrets.find((secret) => secret.id === id)?.description }}
        </p>
        <p v-if="!owner.state.secrets.some((secret) => secret.id === id)">
          未在已读取目录中；保留此 ID，或明确移除。
        </p>
        <UiButton type="button" variant="ghost" :disabled="disabled" @click="remove(id)"
          >移除 {{ owner.secretLabel(id) }}</UiButton
        >
      </li>
    </ul>
    <p v-if="owner.state.secretPhase === 'idle'">
      读取安全目录后可以添加；已有白名单不受目录分页影响。
    </p>
    <p v-else-if="owner.state.secretPhase === 'loading'" role="status">
      正在读取 Secret Variable 安全目录。
    </p>
    <p v-else-if="owner.state.secretPhase === 'ready' && !owner.state.secrets.length" role="status">
      已读取目录为空；已有白名单保持不变。
    </p>
    <p v-if="owner.state.secretMessage" role="alert">{{ owner.state.secretMessage }}</p>
    <div class="secret-actions">
      <UiButton data-read-secrets type="button" :disabled="disabled" @click="owner.loadSecrets()"
        >重新读取 Secret Variable 目录</UiButton
      >
      <UiButton
        v-if="owner.state.secretNext"
        type="button"
        :disabled="disabled"
        @click="owner.loadSecrets(true)"
        >读取更多 Secret Variable</UiButton
      >
    </div>
    <UiField label="添加 Secret Variable" v-slot="field">
      <UiSelect
        :id="field.id"
        v-model="choice"
        label="添加 Secret Variable"
        :options="options"
        :disabled="addDisabled"
      />
    </UiField>
    <p
      v-if="owner.state.secrets.find((secret) => secret.id === choice)?.description"
      class="secret-description"
    >
      {{ owner.state.secrets.find((secret) => secret.id === choice)?.description }}
    </p>
    <UiButton
      type="button"
      :disabled="addDisabled || !options.some((option) => option.value === choice)"
      @click="add"
      >添加到白名单</UiButton
    >
    <p v-if="message" role="alert">{{ message }}</p>
    <p>最多 128 项。保存时会重新校验当前权限和引用；移除白名单项不会删除 Secret Variable。</p>
  </section>
</template>

<style scoped>
.agent-secret-fields,
.selected-secrets,
.selected-secrets li {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
  min-width: 0;
}
.agent-secret-fields h3,
.agent-secret-fields p,
.selected-secrets {
  margin: 0;
  overflow-wrap: anywhere;
}
.selected-secrets {
  padding: 0;
  list-style: none;
}
.selected-secrets li {
  padding: var(--content-gap);
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.secret-id,
.secret-description {
  color: var(--muted);
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.secret-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
