<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import type { useProjectSecrets } from '../../composables/useProjectSecrets'
const props = defineProps<{ page: ReturnType<typeof useProjectSecrets> }>()
const material = ref(''),
  replaceValue = ref(false),
  input = ref<HTMLTextAreaElement | null>(null)
const page = props.page,
  editor = page.editor
const title = computed(() =>
  editor.kind === 'create'
    ? '创建 Secret'
    : editor.kind === 'update'
      ? '编辑 Secret'
      : '删除 Secret',
)
const canSubmit = computed(
  () =>
    page.canWrite.value &&
    !editor.conflict &&
    (editor.kind === 'delete' ||
      (editor.name.length > 0 &&
        (editor.kind === 'create' || replaceValue.value
          ? material.value.length > 0
          : editor.name !== editor.baseline?.name ||
            editor.description !== editor.baseline?.description))),
)
function clear() {
  material.value = ''
  if (input.value) input.value.value = ''
}
watch(() => [editor.clearValue, editor.open, page.visible.value], clear, { flush: 'sync' })
watch(
  () => editor.open,
  () => {
    replaceValue.value = false
  },
  { flush: 'sync' },
)
watch(replaceValue, (enabled) => {
  if (!enabled) clear()
})
onBeforeUnmount(clear)
function submit() {
  if (!canSubmit.value) return
  let value: string | undefined =
    editor.kind === 'create' || replaceValue.value ? material.value : undefined
  clear()
  void page.submit(value)
  value = undefined
}
</script>
<template>
  <UiDialog
    :open="page.visible.value && editor.open"
    :title="title"
    :close-on-outside="false"
    @update:open="!$event && page.closeEditor()"
  >
    <form id="secret-editor" class="secret-form" novalidate @submit.prevent="submit">
      <template v-if="editor.kind !== 'delete'">
        <UiField
          label="名称"
          required
          hint="以字母或下划线开头，只允许字母、数字和下划线，最多 128 个字符；不能使用 AGENTEAM 保留名称。"
          v-slot="field"
        >
          <UiInput
            :id="field.id"
            v-model="editor.name"
            :aria-describedby="field.describedby"
            :disabled="page.busy.value || page.unresolved.value"
            autocomplete="off"
            spellcheck="false"
          />
        </UiField>
        <UiField
          label="描述"
          hint="最多 4096 个 UTF-8 字节。请勿在名称或描述中填写 Secret 值。"
          v-slot="field"
        >
          <textarea
            :id="field.id"
            v-model="editor.description"
            class="ui-input"
            rows="3"
            :aria-describedby="field.describedby"
            :disabled="page.busy.value || page.unresolved.value"
          />
        </UiField>
        <label v-if="editor.kind === 'update'" class="replace-value"
          ><input
            v-model="replaceValue"
            type="checkbox"
            :disabled="page.busy.value || page.unresolved.value || editor.conflict"
          />替换 Secret 值</label
        >
        <UiField
          v-if="editor.kind === 'create' || replaceValue"
          label="新的 Secret 值"
          required
          hint="1–65536 个 UTF-8 字节，不允许 NUL。提交或离开后立即清空；系统不会返回已有值。"
          v-slot="field"
        >
          <textarea
            :id="field.id"
            ref="input"
            v-model="material"
            class="ui-input secret-material"
            rows="4"
            :aria-describedby="field.describedby"
            :disabled="page.busy.value || page.unresolved.value || editor.conflict"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            data-1p-ignore
            data-lpignore="true"
          />
        </UiField>
        <p v-else class="meta">已有值不会读取或显示。仅修改名称和描述时会保留已有值。</p>
      </template>
      <template v-else
        ><p>确认删除 {{ editor.baseline?.name }}？删除后将不能继续使用此 Secret。</p>
        <p class="meta">
          按当前读取的版本 {{ editor.baseline?.version }} 提交；不会自动覆盖后续修改。
        </p></template
      >
      <p v-if="page.state.message" role="status" class="notice">{{ page.state.message }}</p>
      <div v-if="editor.conflict" class="notice">
        <p>先重新读取当前信息并核对，再明确采用新版本。已输入的值已清空。</p>
        <UiButton :disabled="page.busy.value" @click="page.reread()">读取当前版本</UiButton>
        <p v-if="page.detail.phase === 'ready' && page.detail.value">
          当前名称：{{ page.detail.value.name }}；版本：{{ page.detail.value.version }}
        </p>
        <UiButton
          :disabled="
            page.busy.value ||
            page.detail.phase !== 'ready' ||
            page.detail.value === editor.baseline ||
            page.detail.value?.id !== editor.baseline?.id
          "
          @click="page.adopt()"
          >采用当前版本</UiButton
        >
      </div>
      <p v-if="page.unresolved.value" role="status">
        结果尚未确认。值不会保留，可以查证原命令；不要重新提交。
      </p>
      <UiButton
        v-if="page.progress.value?.phase === 'uncertain'"
        :disabled="page.busy.value || page.progress.value.keyConflict"
        @click="page.lookup()"
        >查证原命令</UiButton
      >
    </form>
    <template #footer>
      <UiButton @click="page.closeEditor()">关闭编辑</UiButton>
      <UiButton
        :variant="editor.kind === 'delete' ? 'danger' : 'primary'"
        type="submit"
        form="secret-editor"
        :disabled="!canSubmit"
        >{{ editor.kind === 'delete' ? '确认删除' : '保存' }}</UiButton
      >
    </template>
  </UiDialog>
</template>
<style scoped>
.secret-form {
  display: grid;
  gap: var(--space);
  min-width: 0;
}
.secret-form textarea {
  width: 100%;
  resize: vertical;
  box-sizing: border-box;
}
.replace-value {
  display: flex;
  align-items: center;
  gap: calc(var(--space) / 4);
}
.secret-material {
  font-family: var(--font-mono);
}
.notice {
  overflow-wrap: anywhere;
}
</style>
