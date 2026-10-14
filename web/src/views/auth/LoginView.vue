<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { safeReturnTarget, isAccountSwitch } from '../../router/auth'
import { useSession } from '../../composables/useSession'
import UiButton from '../../components/ui/UiButton.vue'
import UiInput from '../../components/ui/UiInput.vue'
import UiField from '../../components/ui/UiField.vue'
import RotateChallenge from '../../components/account/RotateChallenge.vue'
import AppBrand from '../../components/layout/AppBrand.vue'

const auth = useSession(),
  state = auth.state,
  router = useRouter(),
  route = useRoute()
const email = ref(''),
  password = ref('')
const heading = ref<HTMLElement | null>(null),
  form = ref<HTMLFormElement | null>(null)
const submitted = ref(false),
  submitting = ref(false)
const inputLocked = computed(
  () => submitting.value || state.challengeBusy || state.phase === 'uncertain',
)
const submitBusy = computed(() => submitting.value || (submitted.value && state.busy))
const notice = computed(() =>
  submitted.value && !submitting.value && !Object.keys(state.fields).length ? state.notice : '',
)
const switching = computed(() => isAccountSwitch(route.query.switch))
const choosingAccount = computed(
  () => switching.value && ['authenticated', 'signing-out'].includes(state.phase),
)
let navigating = false
let clearingForm = false
function clearForm() {
  clearingForm = true
  email.value = ''
  password.value = ''
  clearingForm = false
}
watch(
  [email, password],
  () => {
    if (!clearingForm) auth.inputChanged()
  },
  { flush: 'sync' },
)
let challengeFocus: { id: string; action: 'login' | 'create' } | null = null
let verificationFocus: string | null = null
function focusedChallenge() {
  return form.value?.querySelector('.rotate-challenge')?.contains(document.activeElement) ?? false
}
function focusMoved(event: FocusEvent) {
  if (event.target !== document.body && !focusedChallenge()) {
    verificationFocus = null
    challengeFocus = null
  }
}
function pointerStarted(event: PointerEvent) {
  const question = state.challenge
  const panel = form.value?.querySelector('.rotate-challenge')
  if (!(event.target instanceof Element) || !panel?.contains(event.target)) {
    verificationFocus = null
    challengeFocus = null
    return
  }
  // The official drag handle is not focusable. Own this question's actual
  // pointer intent before the browser can move focus to the body.
  if (
    question &&
    !state.busy &&
    !state.challengeBusy &&
    event.isPrimary &&
    event.button === 0 &&
    event.target.closest('.gc-drag-block')
  )
    verificationFocus = question.id
}
watch(
  () => [state.challenge, state.challengeBusy, state.passReady] as const,
  async ([question, busy, passed], [previous, wasBusy]) => {
    if (question) {
      challengeFocus = null
      if (question !== previous) verificationFocus = null
      // This pre-flush runs before the child disables the focused control.
      if (busy && !wasBusy && focusedChallenge()) verificationFocus = question.id
    } else if (previous) {
      const ownedFocus =
        focusedChallenge() ||
        (verificationFocus === previous.id && document.activeElement === document.body)
      challengeFocus = ownedFocus ? { id: previous.id, action: passed ? 'login' : 'create' } : null
      verificationFocus = null
    }
    if (busy || !challengeFocus) return
    const target = challengeFocus
    await nextTick()
    if (challengeFocus !== target || state.challenge || state.challengeBusy) return
    challengeFocus = null
    if (document.activeElement === document.body)
      form.value
        ?.querySelector<HTMLButtonElement>(
          target.action === 'login' ? '[data-action="login"]' : '[data-action="challenge"]',
        )
        ?.focus()
  },
  { flush: 'pre' },
)
watch(
  () => [state.phase, state.busy] as const,
  async ([phase, busy]) => {
    if (switching.value && phase === 'anonymous' && !busy && !navigating) {
      navigating = true
      try {
        await router.replace({
          path: '/login',
          query: { return: safeReturnTarget(route.query.return) },
        })
      } finally {
        navigating = false
      }
      return
    }
    if (phase === 'authenticated' && !busy && !navigating && !switching.value) {
      navigating = true
      clearForm()
      try {
        await router.replace(safeReturnTarget(route.query.return))
      } finally {
        navigating = false
      }
    }
  },
  { immediate: true },
)
async function login() {
  if (submitBusy.value || state.challengeBusy) return
  submitted.value = true
  submitting.value = true
  try {
    await auth.login(email.value, password.value)
  } finally {
    submitting.value = false
  }
}
async function refreshChallenge() {
  await auth.createChallenge()
}
async function closeChallenge() {
  auth.dismissChallenge()
  await nextTick()
  form.value?.querySelector<HTMLButtonElement>('[data-action="challenge"]')?.focus()
}
async function switchLogout() {
  clearForm()
  submitted.value = true
  await auth.logout()
  // The state watcher requires confirmed anonymous state; a void return or
  // Unknown never removes the identity choice or establishes a new login.
}
onMounted(() => {
  document.addEventListener('focusin', focusMoved)
  document.addEventListener('pointerdown', pointerStarted, true)
  heading.value?.focus()
})
onUnmounted(() => {
  document.removeEventListener('focusin', focusMoved)
  document.removeEventListener('pointerdown', pointerStarted, true)
  verificationFocus = null
  challengeFocus = null
  clearForm()
  if (state.phase !== 'authenticated' && !router.currentRoute.value.meta.accountEntry) auth.leave()
})
</script>

<template>
  <main id="main-content" class="account-entry">
    <section class="login-panel account-panel" aria-labelledby="login-title">
      <RouterLink class="brand login-brand" to="/" aria-label="agenteam 入口">
        <AppBrand />
      </RouterLink>
      <header class="account-heading">
        <h1 id="login-title" ref="heading" tabindex="-1">登录</h1>
        <p class="login-description">使用邮箱和密码继续。</p>
      </header>
      <div v-if="choosingAccount" class="ui-stack">
        <p class="account-notice">当前账号：{{ state.user?.display_name || state.user?.email }}</p>
        <RouterLink :to="safeReturnTarget(route.query.return)">返回当前账号</RouterLink>
        <p v-if="notice" role="alert" class="account-notice account-feedback is-error">
          {{ notice }}
        </p>
        <UiButton
          :disabled="state.busy"
          :state="state.busy ? 'loading' : 'idle'"
          loading-label="退出中"
          @click="switchLogout"
          >退出当前账号后登录</UiButton
        >
        <p class="login-description">切换账号后，请重新打开原邀请或重置链接。</p>
      </div>
      <form v-if="!choosingAccount" ref="form" class="account-form" @submit.prevent="login">
        <UiField
          v-slot="field"
          label="邮箱"
          id="login-email"
          :error="submitted ? state.fields.email : undefined"
          required
        >
          <UiInput
            :id="field.id"
            v-model="email"
            name="email"
            type="email"
            autocomplete="username"
            required
            :readonly="inputLocked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
          />
        </UiField>
        <UiField
          v-slot="field"
          label="密码"
          id="login-password"
          :error="submitted ? state.fields.password : undefined"
          required
        >
          <UiInput
            :id="field.id"
            v-model="password"
            name="password"
            type="password"
            autocomplete="current-password"
            required
            :readonly="inputLocked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
          />
        </UiField>
        <RotateChallenge
          v-if="state.challenge"
          :challenge="state.challenge"
          :busy="state.challengeBusy"
          @verify="auth.verifyChallenge"
          @refresh="refreshChallenge"
          @close="closeChallenge"
        />
        <UiButton
          v-if="state.challengeRequired && !state.challenge && !state.passReady"
          data-action="challenge"
          :state="state.challengeBusy ? 'loading' : 'idle'"
          :disabled="state.busy || state.phase === 'uncertain'"
          @click="refreshChallenge"
          >开始旋转验证</UiButton
        >
        <p
          v-if="notice"
          id="login-feedback"
          role="alert"
          class="account-notice account-feedback is-error"
        >
          {{ notice }}
        </p>
        <UiButton
          variant="primary"
          type="submit"
          data-action="login"
          :state="submitBusy ? 'loading' : state.phase === 'authenticated' ? 'success' : 'idle'"
          loading-label="登录中"
          success-label="已登录"
          :aria-describedby="notice ? 'login-feedback' : undefined"
          :disabled="
            submitBusy ||
            state.challengeBusy ||
            (state.phase !== 'uncertain' && state.challengeRequired && !state.passReady)
          "
          >登录</UiButton
        >
      </form>
      <div v-if="!switching || state.phase === 'anonymous'" class="account-navigation">
        <RouterLink to="/forgot-password">找回密码</RouterLink>
      </div>
    </section>
  </main>
</template>
