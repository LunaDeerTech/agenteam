import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { Rotate } from 'go-captcha-vue'
import 'go-captcha-vue/dist/style.css'
import './style.css'

// Only this isolated test harness is served. There are no product account
// pages or production identity routes in B02.
type Challenge = { id: string; mode: 'rotate'; master: string; thumb: string }
createApp(defineComponent({
  setup() {
    const email = ref('admin@mail.com')
    const password = ref('')
    const key = ref(crypto.randomUUID())
    const csrf = ref('')
    const status = ref('Loading browser context')
    const challenge = ref<Challenge | null>(null)
    const angle = ref(0)
    const pass = ref('')
    const busy = ref(false)
    const loginButton = ref<HTMLButtonElement | null>(null)
    const refreshButton = ref<HTMLButtonElement | null>(null)
    async function request(path: string, body?: unknown) {
      const response = await fetch(`/fixture/${path}`, { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.value }, body: body === undefined ? undefined : JSON.stringify(body) })
      const value = await response.json()
      if (!response.ok) throw new Error(value.code || 'REQUEST_FAILED')
      return value
    }
    request('bootstrap').then((v) => { csrf.value = v.csrf; status.value = 'Ready' }).catch(() => { status.value = 'Browser context unavailable' })
    async function makeChallenge() {
      if (busy.value) return
      busy.value = true
      try {
        challenge.value = await request('challenge', { email: email.value, login_key: key.value })
        angle.value = 0; pass.value = ''; status.value = 'Align the inner image with the outer image'
      } catch (e) { status.value = String(e) } finally { busy.value = false }
    }
    async function verify(value: number) {
      if (busy.value || !challenge.value) return
      busy.value = true
      try {
        const result = await request('verify', { challenge_id: challenge.value.id, angle: Math.round(value), email: email.value, login_key: key.value })
        pass.value = result.pass; challenge.value = null; status.value = 'Challenge verified'
        busy.value = false; await nextTick(); loginButton.value?.focus()
      } catch (e) {
        challenge.value = null; pass.value = ''; status.value = String(e)
        busy.value = false; await nextTick(); refreshButton.value?.focus()
      } finally { busy.value = false }
    }
    async function login() {
      if (busy.value) return
      busy.value = true
      try {
        await request('login', { email: email.value, password: password.value, login_key: key.value, challenge_pass: pass.value })
        status.value = 'Logged in'; password.value = ''; pass.value = ''
      } catch (e) {
        status.value = String(e); key.value = crypto.randomUUID(); pass.value = ''; challenge.value = null
      } finally { busy.value = false }
    }
    const input = (label: string, type: string, value: typeof email) => h('label', [label, h('input', { type, value: value.value, autocomplete: 'off', onInput: (e: Event) => { value.value = (e.target as HTMLInputElement).value } })])
    return () => h('main', [
      h('h1', 'Account challenge integration fixture'),
      h('p', 'Aligning the images requires visual judgement. Keyboard controls change the same angle and use the same verification.'),
      input('Email', 'email', email), input('Password', 'password', password),
      h('div', { class: 'actions' }, [
        h('button', { ref: loginButton, disabled: busy.value || !csrf.value, onClick: login }, 'Log in'),
        h('button', { ref: refreshButton, disabled: busy.value || !csrf.value, onClick: makeChallenge }, 'Create challenge'),
      ]),
      challenge.value ? h('section', { 'aria-label': 'Rotate challenge' }, [
        h(Rotate, { data: { image: challenge.value.master, thumb: challenge.value.thumb, angle: angle.value, thumbSize: 160 }, config: { width: 240, height: 220, size: 220, horizontalPadding: 8, title: 'Rotate to align', showTheme: true }, events: { confirm: verify, rotate: () => {}, refresh: makeChallenge, close: () => { challenge.value = null; nextTick(() => refreshButton.value?.focus()) } } }),
        h('label', ['Angle', h('input', { type: 'range', min: 0, max: 360, step: 1, value: angle.value, 'aria-label': 'Rotation angle', 'aria-valuetext': `${angle.value} degrees`, onInput: (e: Event) => { angle.value = Number((e.target as HTMLInputElement).value) } })]),
        h('output', { 'data-testid': 'angle' }, `${angle.value}°`),
        h('button', { disabled: busy.value, onClick: () => verify(angle.value) }, 'Verify angle'),
      ]) : null,
      h('p', { role: 'status', 'aria-live': 'polite', 'aria-label': 'Authentication status' }, status.value),
    ])
  },
})).mount('#app')
