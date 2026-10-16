<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { UiButton, UiField, UiTextarea } from '../ui'
import type { TaskExecutionCancellation } from '../../composables/useTaskExecutionCancellation'
const props = defineProps<{ owner: TaskExecutionCancellation }>()
const confirm = ref<{ $el: HTMLButtonElement } | null>(null)
const trigger = ref<{ $el: HTMLButtonElement } | null>(null)
async function open() {
  if (!props.owner.open()) return
  await nextTick()
  confirm.value?.$el.focus()
}
async function keep() {
  if (!props.owner.keep()) return
  await nextTick()
  trigger.value?.$el.focus()
}
</script>
<template>
  <section
    v-if="owner.visible.value"
    class="execution-cancellation"
    aria-label="执行取消"
    :aria-busy="owner.state.active"
  >
    <p v-if="owner.progress.value?.phase === 'confirmed'" role="status">
      原取消请求已接受。当前任务和执行状态以刷新结果为准。
    </p>
    <template v-else-if="owner.progress.value?.phase === 'uncertain'">
      <p role="status">原执行取消结果尚未确认，请核对原请求。</p>
      <UiButton
        :disabled="owner.blocked.value || !owner.progress.value.contextValid"
        @click="owner.lookup()"
        >核对原执行取消</UiButton
      >
    </template>
    <template v-else-if="owner.progress.value?.phase === 'rejected'">
      <p>取消请求未提交，请重新读取后再决定。</p>
      <UiButton :disabled="owner.blocked.value" @click="owner.reread()"
        >重新读取任务和执行</UiButton
      >
    </template>
    <form v-else-if="owner.state.open" aria-label="确认取消执行" @submit.prevent="owner.submit()">
      <p>确认取消当前选中的执行？任务将受阻，执行完全停止后显示已取消。</p>
      <UiField label="执行取消说明" hint="可选，说明将随取消请求保留。" v-slot="field">
        <UiTextarea
          :id="field.id"
          v-model="owner.state.reason"
          :aria-describedby="field.describedby"
          :disabled="owner.blocked.value"
          rows="3"
        />
      </UiField>
      <p v-if="!owner.reasonValid.value" role="alert">取消说明过长、只含空白或包含无效字符。</p>
      <p v-if="owner.stale.value" role="status">任务或执行已更新，请关闭此确认并重新读取。</p>
      <div class="cancellation-actions">
        <UiButton ref="confirm" type="submit" variant="danger" :disabled="!owner.canSubmit.value"
          >确认取消执行</UiButton
        >
        <UiButton :disabled="owner.blocked.value" @click="keep">保留执行</UiButton>
      </div>
    </form>
    <UiButton v-else ref="trigger" variant="danger" :disabled="!owner.canStart.value" @click="open"
      >取消当前执行</UiButton
    >
    <p v-if="owner.state.message" role="status">{{ owner.state.message }}</p>
  </section>
</template>
<style scoped>
.execution-cancellation,
form {
  display: grid;
  gap: var(--content-gap);
}
.execution-cancellation {
  border-top: 1px solid var(--border);
  padding-top: var(--space);
}
.cancellation-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
