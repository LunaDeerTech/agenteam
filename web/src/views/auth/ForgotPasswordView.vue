<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useAccountEntry, useAccountEntryForm } from '../../composables/useAccountEntry'
import UiButton from '../../components/ui/UiButton.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import AppBrand from '../../components/layout/AppBrand.vue'
const entry = useAccountEntry(),
  state = entry.state,
  draft = entry.draft
const { heading, form, submit: submitForm } = useAccountEntryForm(entry)
const submitted = ref(false),
  submitting = ref(false),
  accepted = ref(false)
const submitBusy = computed(() => submitting.value || (submitted.value && entry.busy.value))
const notice = computed(() =>
  submitted.value && !submitting.value && !Object.keys(state.fields).length ? state.notice : '',
)
let feedbackTimer: ReturnType<typeof setTimeout> | undefined
let alive = true
async function submit() {
  if (submitBusy.value) return
  clearTimeout(feedbackTimer)
  accepted.value = false
  submitted.value = true
  submitting.value = true
  try {
    await submitForm()
  } finally {
    if (alive) {
      submitting.value = false
      if (state.phase === 'success') {
        accepted.value = true
        feedbackTimer = setTimeout(() => {
          accepted.value = false
        }, 2400)
      }
    }
  }
}
onUnmounted(() => {
  alive = false
  clearTimeout(feedbackTimer)
})
</script>

<template>
  <main id="main-content" class="account-entry">
    <section class="entry-panel" aria-labelledby="forgot-password-title">
      <RouterLink class="brand" to="/" aria-label="agenteam 入口"><AppBrand /></RouterLink>
      <h1 id="forgot-password-title" ref="heading" tabindex="-1">找回密码</h1>
      <p class="description">填写账号邮箱，申请恢复方式。</p>
      <form ref="form" class="ui-stack" novalidate @submit.prevent="submit">
        <UiField v-slot="field" id="forgot-email" label="邮箱" :error="state.fields.email" required>
          <UiInput
            :id="field.id"
            v-model="draft.email"
            name="email"
            type="email"
            autocomplete="email"
            :readonly="entry.locked.value"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            required
          />
        </UiField>
        <p
          v-if="notice"
          id="forgot-feedback"
          :role="state.phase === 'success' ? 'status' : 'alert'"
          tabindex="-1"
          class="entry-feedback"
        >
          {{ notice }}
        </p>
        <UiButton
          type="submit"
          variant="primary"
          :disabled="submitBusy"
          :state="submitBusy ? 'loading' : accepted ? 'success' : 'idle'"
          loading-label="正在申请"
          success-label="已受理"
          :aria-describedby="notice ? 'forgot-feedback' : undefined"
          >{{ state.phase === 'success' ? '再次申请' : '申请恢复方式' }}</UiButton
        >
      </form>
      <p v-if="state.phase === 'success' && state.channel" class="description">
        {{
          state.channel === 'smtp'
            ? '若该邮箱可用于恢复，请留意恢复邮件。申请受理不代表邮件已送达。'
            : '请有权限访问受限后端日志的人协助转交恢复方式。'
        }}
      </p>
      <div class="entry-navigation">
        <RouterLink :to="entry.loginTarget.value">返回登录</RouterLink>
      </div>
    </section>
  </main>
</template>

<style scoped>
.account-entry {
  min-height: 100dvh;
  padding: 32px 16px;
  display: grid;
  place-items: center;
}
.entry-panel {
  width: min(100%, 440px);
  min-width: 0;
  padding: 28px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
  box-shadow: var(--panel-shadow);
  display: grid;
  gap: 20px;
  overflow-wrap: anywhere;
}
.brand {
  justify-self: start;
}
.description {
  color: var(--muted);
}
.entry-navigation {
  text-align: center;
}
@media (max-width: 450px) {
  .entry-panel {
    padding: 20px;
  }
}
@media (max-width: 280px) {
  .account-entry {
    padding-inline: 8px;
  }
  .entry-panel {
    padding: 12px;
  }
}
</style>
