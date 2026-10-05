import { readonly, ref } from 'vue'
export type ThemeMode = 'light' | 'dark' | 'system'
const mode = ref<ThemeMode>('system')
const resolved = ref<'light' | 'dark'>('light')
let media: MediaQueryList | undefined
function apply() {
  resolved.value = mode.value === 'system' ? (media?.matches ? 'dark' : 'light') : mode.value
  document.documentElement.dataset.theme = resolved.value
}
export function initializeTheme() {
  if (media) return
  media = window.matchMedia('(prefers-color-scheme: dark)')
  media.addEventListener('change', apply)
  apply()
}
export function useTheme() {
  return {
    mode: readonly(mode),
    resolved: readonly(resolved),
    setTheme(value: ThemeMode) {
      mode.value = value
      apply()
    },
  }
}
