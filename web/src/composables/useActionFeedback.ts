import { onScopeDispose, ref } from 'vue'
import type { ButtonState } from '../components/ui/types'
export function useActionFeedback() {
  const state = ref<ButtonState>('idle')
  const error = ref('')
  let timer: ReturnType<typeof setTimeout> | undefined
  let disposed = false
  onScopeDispose(() => {
    disposed = true
    clearTimeout(timer)
  })
  async function run(action: () => Promise<void> | void) {
    if (state.value === 'loading') return
    clearTimeout(timer)
    error.value = ''
    state.value = 'loading'
    try {
      await action()
      if (disposed) return
      state.value = 'success'
      timer = setTimeout(() => {
        state.value = 'idle'
      }, 2400)
    } catch (e) {
      if (!disposed) {
        error.value = e instanceof Error ? e.message : '操作失败'
        state.value = 'idle'
      }
    }
  }
  return { state, error, run }
}
