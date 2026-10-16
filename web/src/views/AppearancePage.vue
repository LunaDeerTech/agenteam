<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from "vue";
import { UiButton } from "../components/ui";
import CommandFeedback from "../components/account/CommandFeedback.vue";
import { useLeaveProtection } from "../account/leave";
import { authenticatedRequest, sessionCommand, updateSessionUser, useSession } from "../account/session";
import { AccountError, accountEnabled, accountErrorMessage } from "../account/transport";
import { isPreferences, type PreferencesResponse, type ThemePreference } from "../account/types";
import { useCommand } from "../account/useCommand";
import { applyAccountTheme } from "../composables/useTheme";

const session = useSession();
const selected = ref<ThemePreference>("system");
const baseline = shallowRef<PreferencesResponse>();
const latest = shallowRef<PreferencesResponse>();
const loading = ref(false);
const loadError = shallowRef<unknown>();
const saved = ref(false);
const latestRead = ref(false);
const save = useCommand<PreferencesResponse>();
let alive = true;
let readGeneration = 0;
const options = [
  { value: "light", label: "浅色", description: "明亮、清晰的工作界面" },
  { value: "dark", label: "深色", description: "适合较暗的使用环境" },
  { value: "system", label: "跟随系统", description: "随设备的外观设置切换" },
] as const;
const dirty = computed(() => !!baseline.value && selected.value !== baseline.value.theme);
const locked = computed(() => !accountEnabled || !baseline.value || loading.value || save.busy.value || save.uncertain.value);
const conflict = computed(() => save.error.value instanceof AccountError && save.error.value.code === "VERSION_CONFLICT");
function select(value: ThemePreference) {
  if (locked.value) return;
  selected.value = value;
  saved.value = false;
  applyAccountTheme(value);
}
function cancel() {
  if (!latest.value) return;
  baseline.value = latest.value;
  selected.value = latest.value.theme;
  applyAccountTheme(selected.value);
  save.abandon();
  latestRead.value = false;
  saved.value = false;
}
async function load() {
  if (!accountEnabled || loading.value || save.uncertain.value) return;
  const owner = ++readGeneration;
  loading.value = true;
  loadError.value = undefined;
  try {
    const value = await authenticatedRequest("/me/preferences", { current: () => alive && owner === readGeneration }, isPreferences);
    if (!alive || owner !== readGeneration) return;
    const preserve = dirty.value;
    latest.value = value;
    if (session.user.value) updateSessionUser({ ...session.user.value, ...value });
    if (!preserve) {
      baseline.value = value;
      selected.value = value.theme;
      applyAccountTheme(value.theme);
    }
    latestRead.value = conflict.value;
  } catch (error) {
    if (alive && owner === readGeneration) loadError.value = error;
  } finally {
    if (alive && owner === readGeneration) loading.value = false;
  }
}
function useLatestVersion() {
  if (!latestRead.value || !latest.value) return;
  baseline.value = latest.value;
  save.abandon();
  latestRead.value = false;
}
async function submit() {
  if (locked.value || !dirty.value || !baseline.value || conflict.value) return;
  const body = { version: baseline.value.version, theme: selected.value };
  await save.submit(() => sessionCommand("/me/preferences", { method: "PUT", body: JSON.stringify(body) }, isPreferences), (value) => {
    baseline.value = value;
    latest.value = value;
    selected.value = value.theme;
    if (session.user.value) updateSessionUser({ ...session.user.value, ...value });
    applyAccountTheme(value.theme);
    saved.value = true;
  });
}
function abandonSave() { save.abandon(); cancel(); void load(); }
watch(session.user, (user) => {
  if (!user) return;
  latest.value = { version: user.version, theme: user.theme };
  if (!dirty.value && baseline.value) {
    baseline.value = latest.value;
    selected.value = user.theme;
    applyAccountTheme(user.theme);
  }
});
watch(session.generation, () => {
  readGeneration++;
  baseline.value = undefined;
  latest.value = undefined;
  selected.value = "system";
  loading.value = false;
  saved.value = false;
});
useLeaveProtection(() => dirty.value || save.busy.value || save.uncertain.value);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  readGeneration++;
  applyAccountTheme(session.hasIdentity.value ? (session.user.value?.theme ?? latest.value?.theme ?? "system") : "system");
});
</script>

<template>
  <form class="account-form appearance-form" @submit.prevent="submit">
    <p v-if="!accountEnabled" class="form-feedback">当前为界面预览。账号服务连接后可保存外观偏好。</p>
    <p v-else-if="loading && !baseline" class="form-note" role="status">正在读取外观偏好…</p>
    <div v-if="loadError" class="form-feedback" role="alert">
      <p>{{ accountErrorMessage(loadError) }}</p>
      <UiButton :loading="loading" @click="load">重新读取外观偏好</UiButton>
    </div>
    <fieldset :disabled="locked">
      <legend class="appearance-legend">界面主题</legend>
      <div class="theme-options">
        <label v-for="option in options" :key="option.value" class="theme-option" :class="{ 'is-selected': selected === option.value }">
          <input type="radio" name="account-theme" :value="option.value" :checked="selected === option.value" @change="select(option.value)" />
          <span class="theme-preview" :class="`theme-preview--${option.value}`" aria-hidden="true">
            <span class="preview-sidebar"><i /><i /><i /></span>
            <span class="preview-content"><i /><i /><i /></span>
          </span>
          <span class="theme-label">{{ option.label }}</span>
          <span class="form-note">{{ option.description }}</span>
        </label>
      </div>
    </fieldset>
    <p class="form-note">选择后立即预览，保存后应用到你的账号。</p>
    <CommandFeedback :error="save.error.value" :uncertain="save.uncertain.value" :replayable="save.replayable.value" :busy="save.busy.value" @retry="save.retry" @abandon="abandonSave" />
    <div v-if="conflict" class="form-feedback">
      <p>你的主题选择仍保留。请读取并核对最新偏好。</p>
      <UiButton :loading="loading" @click="load">读取最新偏好</UiButton>
      <template v-if="latestRead && latest">
        <p>当前已保存：{{ options.find((option) => option.value === latest?.theme)?.label }}</p>
        <UiButton @click="useLatestVersion">基于最新偏好继续编辑</UiButton>
      </template>
    </div>
    <div class="form-actions">
      <UiButton type="submit" variant="primary" :loading="save.busy.value" :disabled="locked || !dirty || conflict">保存偏好</UiButton>
      <UiButton :disabled="locked || !dirty" @click="cancel">取消</UiButton>
      <span v-if="saved" class="form-note form-success" role="status">外观偏好已保存。</span>
    </div>
  </form>
</template>

<style scoped>
.appearance-form { max-width: 620px; }
.appearance-legend { margin-bottom: 16px; color: var(--text-primary); font-size: 13px; font-weight: 600; }
.theme-options { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; }
.theme-option { display: grid; position: relative; gap: 7px; min-width: 0; cursor: pointer; }
.theme-option input { position: absolute; inset-inline-start: 10px; top: 10px; margin: 0; accent-color: var(--text-primary); z-index: 1; }
.theme-preview { display: flex; height: 110px; overflow: hidden; border: 1px solid var(--border-default); border-radius: 7px; background: #fff; }
.theme-option.is-selected .theme-preview { outline: 2px solid var(--text-primary); outline-offset: 2px; }
.theme-option:focus-within .theme-preview { outline: 2px solid var(--focus-ring); outline-offset: 3px; }
.preview-sidebar { display: grid; align-content: center; width: 29%; padding: 6px; gap: 7px; background: #f0f1ee; }
.preview-content { display: grid; flex: 1; align-content: center; gap: 10px; padding: 15px; }
.preview-sidebar i, .preview-content i { display: block; height: 4px; border-radius: 2px; background: #d5d7d2; }
.preview-content i:first-child { width: 65%; height: 6px; background: #858b82; }
.theme-preview--dark { background: #1d1f1c; }
.theme-preview--dark .preview-sidebar { background: #252823; }
.theme-preview--dark i { background: #50574d; }
.theme-preview--system { background: linear-gradient(90deg, #fff 50%, #1d1f1c 50%); }
.theme-preview--system .preview-content i { background: #93998f; }
.theme-label { margin-top: 4px; font-size: 13px; font-weight: 600; }
fieldset:disabled .theme-option { cursor: default; }
@media (max-width: 520px) { .theme-options { grid-template-columns: 1fr; gap: 20px; } .theme-preview { height: 120px; } }
</style>
