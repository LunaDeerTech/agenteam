import { readonly, ref } from "vue";

export type ThemePreference = "light" | "dark" | "system";
export const THEME_STORAGE_KEY = "agenteam.vue.appearance.v1";

const theme = ref<ThemePreference>("system");
const resolvedTheme = ref<"light" | "dark">("light");
let dispose: (() => void) | undefined;
let media: MediaQueryList | undefined;

function isTheme(value: unknown): value is ThemePreference {
  return value === "light" || value === "dark" || value === "system";
}

function applyTheme() {
  resolvedTheme.value =
    theme.value === "system"
      ? media?.matches
        ? "dark"
        : "light"
      : theme.value;
  if (typeof document !== "undefined") {
    document.documentElement.dataset.theme = resolvedTheme.value;
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute(
        "content",
        resolvedTheme.value === "dark" ? "#161715" : "#f5f5f3",
      );
  }
}

export function initTheme() {
  if (typeof window === "undefined") return () => {};
  if (dispose) return dispose;
  media = window.matchMedia("(prefers-color-scheme: dark)");
  try {
    const saved = window.localStorage.getItem(THEME_STORAGE_KEY);
    theme.value = isTheme(saved) ? saved : "system";
  } catch {
    theme.value = "system";
  }
  const onSystemChange = () => {
    if (theme.value === "system") applyTheme();
  };
  const onStorage = (event: StorageEvent) => {
    if (event.key !== THEME_STORAGE_KEY && event.key !== null) return;
    theme.value = isTheme(event.newValue) ? event.newValue : "system";
    applyTheme();
  };
  media.addEventListener("change", onSystemChange);
  window.addEventListener("storage", onStorage);
  applyTheme();
  dispose = () => {
    media?.removeEventListener("change", onSystemChange);
    window.removeEventListener("storage", onStorage);
    dispose = undefined;
    media = undefined;
  };
  return dispose;
}

function setTheme(value: ThemePreference) {
  if (!isTheme(value)) return;
  if (!dispose) initTheme();
  theme.value = value;
  applyTheme();
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, value);
  } catch {
    // Appearance remains usable when the browser does not allow storage.
  }
}

export function applyAccountTheme(value: ThemePreference) {
  if (!isTheme(value)) return;
  if (!dispose) initTheme();
  theme.value = value;
  applyTheme();
}

export function useTheme() {
  return {
    theme: readonly(theme),
    resolvedTheme: readonly(resolvedTheme),
    setTheme,
  };
}

if (import.meta.hot) import.meta.hot.dispose(() => dispose?.());
