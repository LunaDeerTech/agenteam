<script setup lang="ts">
import { UiButton, UiDialog, UiField, UiInput, UiTextarea } from '../../components/ui'
import { useSystemRunners } from '../../composables/useSystemRunners'
const page = useSystemRunners(),
  { editor, draft, locked, canSave, errors } = page
</script>
<template>
  <UiDialog
    :open="editor.mode !== null"
    :title="editor.mode === 'create' ? '创建 Runner' : '编辑 Runner'"
    :close-on-outside="false"
    @update:open="!$event && page.closeEditor()"
  >
    <form class="runner-form" @submit.prevent="page.save">
      <UiField v-slot="field" label="Runner 名称" required hint="1–128 UTF-8 字节。">
        <UiInput
          data-autofocus
          :id="field.id"
          v-model="draft.name"
          :disabled="locked"
          :aria-describedby="field.describedby"
          autocomplete="off"
        />
      </UiField>
      <UiField v-slot="field" label="描述" hint="至多4096字节，不含控制字符。">
        <UiInput
          :id="field.id"
          v-model="draft.description"
          :disabled="locked"
          :aria-describedby="field.describedby"
          autocomplete="off"
        />
      </UiField>
      <UiField v-slot="field" label="标签" hint="每行一项，最多32项；按字节顺序保存，不允许重复。">
        <UiTextarea
          :id="field.id"
          v-model="draft.tags"
          :disabled="locked"
          :aria-describedby="field.describedby"
          autocomplete="off"
        />
      </UiField>
      <UiField
        v-slot="field"
        label="根目录"
        required
        hint="Linux/macOS 绝对规范路径；创建后不可修改。"
      >
        <UiInput
          :id="field.id"
          v-model="draft.root_path"
          :readonly="editor.mode === 'edit'"
          :disabled="locked"
          :aria-describedby="field.describedby"
          autocomplete="off"
          spellcheck="false"
        />
      </UiField>
      <p v-if="errors" role="status">{{ errors }}</p>
      <p v-if="editor.message" role="alert">{{ editor.message }}</p>
      <section v-if="page.progress.value?.phase === 'uncertain'" aria-label="原命令状态未确认">
        <p>只检查原请求，不重新提交；未观察到提交不表示回滚。</p>
        <UiButton
          :disabled="page.blocked.value || !page.progress.value.contextValid"
          @click="page.checkOriginal"
          >检查原请求</UiButton
        >
      </section>
      <UiButton v-if="editor.conflict" :disabled="locked" variant="ghost" @click="page.rebase"
        >读取并采用当前版本，保留草稿</UiButton
      >
      <div class="runner-actions">
        <UiButton variant="ghost" :disabled="page.blocked.value" @click="page.closeEditor"
          >取消</UiButton
        ><UiButton type="submit" :disabled="!canSave">{{
          editor.mode === 'create' ? '创建 Runner' : '保存修改'
        }}</UiButton>
      </div>
    </form>
  </UiDialog>
</template>
<style scoped>
.runner-form {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.runner-form p {
  overflow-wrap: anywhere;
}
.runner-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
</style>
