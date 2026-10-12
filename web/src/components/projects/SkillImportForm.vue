<script setup lang="ts">
import { UiButton, UiField } from '../ui'
import type { SkillImportController } from '../../composables/useSkillImport'
defineProps<{ owner: SkillImportController }>()
const emit = defineEmits<{ close: [] }>()
function files(event: Event) {
  return (event.target as HTMLInputElement).files ?? []
}
</script>
<template>
  <section class="skill-import" aria-labelledby="skill-import-heading">
    <h2 id="skill-import-heading" tabindex="-1">导入技能包</h2>
    <p>
      选择磁盘上已有的单文件 SKILL.md 标准包。仅接受 UTF-8、LF 换行和不超过 256 KiB 的文件；不接受
      BOM、CR 或 NUL。名称和描述由服务端校验，文件内容不会被修改。
    </p>
    <p v-if="owner.otherPending.value" role="status">
      另一个项目有尚未清理的导入状态，请返回原项目查看。
    </p>
    <form v-else-if="owner.editable.value" @submit.prevent="owner.act()">
      <UiField v-slot="field" label="SKILL.md 文件" required>
        <input
          :key="owner.fileRevision.value"
          :id="field.id"
          type="file"
          accept=".md"
          :disabled="owner.blocked.value || !owner.canWrite.value"
          @change="owner.select(files($event))"
        />
      </UiField>
      <p v-if="owner.reading.value" role="status">正在读取所选文件。</p>
      <p v-if="owner.selected.value">
        {{ owner.selected.value.name }} · {{ owner.selected.value.size }} 字节
      </p>
      <p v-if="!owner.canWrite.value" role="status">当前项目只读，不能导入技能包。</p>
      <UiButton type="submit" variant="primary" :disabled="!owner.canSubmit.value"
        >确认导入</UiButton
      >
    </form>
    <template v-else-if="owner.progress.value">
      <p v-if="owner.progress.value.phase === 'submitting'" role="status">
        正在导入，请等待原请求返回。
      </p>
      <template v-else-if="owner.progress.value.phase === 'confirmed'">
        <p role="status">技能包已导入。目录和详情将独立读取。</p>
        <div class="import-actions">
          <RouterLink :to="owner.detailPath.value">查看导入的技能</RouterLink>
          <UiButton
            :disabled="owner.blocked.value || !owner.canWrite.value"
            @click="owner.another()"
            >导入另一个包</UiButton
          >
        </div>
      </template>
      <template v-else>
        <p role="status">导入结果尚未确认。只能检查原请求，不会自动重发或创建新的导入。</p>
        <UiButton :disabled="owner.blocked.value" @click="owner.act(true)">检查原请求</UiButton>
      </template>
    </template>
    <p v-if="owner.message.value" role="alert">{{ owner.message.value }}</p>
    <UiButton :disabled="owner.blocked.value" @click="emit('close')">取消</UiButton>
  </section>
</template>
<style scoped>
.skill-import,
form {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
}
.skill-import {
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
}
.skill-import h2 {
  margin: 0;
}
.skill-import p {
  overflow-wrap: anywhere;
}
.skill-import input {
  max-width: 100%;
  min-width: 0;
}
.import-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
  align-items: center;
}
</style>
