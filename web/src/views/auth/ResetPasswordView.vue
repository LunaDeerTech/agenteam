<script setup lang="ts">
import { useAccountEntry, useAccountEntryForm } from '../../composables/useAccountEntry'
import UiButton from '../../components/ui/UiButton.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import UiState from '../../components/ui/UiState.vue'
const entry = useAccountEntry(),
  state = entry.state,
  draft = entry.draft
const { heading, form, submit } = useAccountEntryForm(entry)
</script>

<template>
  <main id="main-content" class="account-entry">
    <section class="entry-panel" aria-labelledby="reset-password-title">
      <RouterLink class="brand" to="/" aria-label="agenteam 入口"
        ><span class="brand-mark" aria-hidden="true">a</span>agenteam</RouterLink
      >
      <h1 id="reset-password-title" ref="heading" tabindex="-1">重置密码</h1>
      <p class="description">设置新密码后，请重新登录。</p>
      <UiState v-if="state.phase === 'preparing'" kind="loading" title="正在校验重置链接" />
      <form
        v-if="state.expiresAt && ['ready', 'submitting', 'uncertain'].includes(state.phase)"
        ref="form"
        class="ui-stack"
        novalidate
        @submit.prevent="submit"
      >
        <p class="description">链接到期时间：{{ state.expiresAt }}</p>
        <UiField
          v-slot="field"
          id="reset-password"
          label="新密码"
          hint="15–128 个字符，允许中文和空格，不强制字符组合。"
          :error="state.fields.password"
          required
        >
          <UiInput
            :id="field.id"
            v-model="draft.password"
            name="password"
            type="password"
            autocomplete="new-password"
            :readonly="entry.locked.value"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            required
          />
        </UiField>
        <UiField
          v-slot="field"
          id="reset-confirmation"
          label="确认密码"
          :error="state.fields.confirmation"
          required
        >
          <UiInput
            :id="field.id"
            v-model="draft.confirmation"
            name="confirmation"
            type="password"
            autocomplete="new-password"
            :readonly="entry.locked.value"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            required
          />
        </UiField>
        <UiButton
          type="submit"
          variant="primary"
          :disabled="entry.locked.value"
          :state="state.phase === 'submitting' ? 'loading' : 'idle'"
          >重置密码</UiButton
        >
      </form>
      <p v-if="state.notice" role="alert" tabindex="-1">{{ state.notice }}</p>
      <p v-if="state.requestID" class="description">请求编号：{{ state.requestID }}</p>
      <div v-if="state.phase === 'uncertain'" class="actions">
        <UiButton :disabled="!entry.canRetryOriginal" @click="entry.retryOriginal"
          >重试原请求</UiButton
        ><UiButton variant="ghost" @click="entry.abandon">放弃原请求</UiButton>
      </div>
      <UiButton
        v-if="state.phase === 'session-unconfirmed'"
        :disabled="entry.locked.value"
        @click="entry.checkSession"
        >检查当前会话</UiButton
      >
      <UiButton
        v-if="state.phase === 'failed' || (state.phase === 'ready' && state.notice)"
        :disabled="entry.locked.value"
        @click="entry.prepare"
        >重新检查</UiButton
      >
      <RouterLink v-if="['invalid', 'missing'].includes(state.phase)" to="/forgot-password"
        >重新申请找回密码</RouterLink
      >
      <RouterLink :to="entry.loginTarget.value">返回登录</RouterLink>
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
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
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
