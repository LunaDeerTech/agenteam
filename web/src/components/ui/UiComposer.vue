<script setup lang="ts">
import { ref, useId } from 'vue'
import UiTextarea from './UiTextarea.vue'
import UiButton from './UiButton.vue'
import type { ButtonState } from './types'
const model = defineModel<string>({ default: '' })
const props = withDefaults(
  defineProps<{ label?: string; state?: ButtonState; disabled?: boolean; error?: string }>(),
  { label: '消息内容', state: 'idle' },
)
const emit = defineEmits<{ send: [text: string] }>()
const localError = ref('')
const id = useId()
function send() {
  if (props.disabled || props.state === 'loading') return
  if (!model.value.trim()) {
    localError.value = '请输入内容'
    return
  }
  localError.value = ''
  emit('send', model.value.trim())
}
</script>
<template>
  <form class="ui-composer" @submit.prevent="send">
    <UiTextarea
      v-model="model"
      class="composer-input"
      :aria-label="label"
      :aria-describedby="error || localError ? id + '-error' : undefined"
      :invalid="!!(error || localError)"
      :disabled="disabled || state === 'loading'"
      placeholder="输入内容…"
      @input="localError = ''"
    />
    <p v-if="error || localError" :id="id + '-error'" class="field-note error-text" role="alert">
      {{ error || localError }}
    </p>
    <div class="composer-actions">
      <slot name="actions" />
      <UiButton
        type="submit"
        variant="primary"
        :state="state"
        :disabled="disabled"
        loading-label="发送中"
        success-label="已发送"
        >发送</UiButton
      >
    </div>
  </form>
</template>
