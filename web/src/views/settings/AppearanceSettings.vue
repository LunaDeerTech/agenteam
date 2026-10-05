<script setup lang="ts">
import { computed } from 'vue'
import { usePersonalSettings } from '../../composables/usePersonalSettings'
import { UiButton, UiRadioGroup, UiState } from '../../components/ui'
import type { Theme } from '../../api/account'
const s = usePersonalSettings()
const options = [
  { value: 'system', label: '跟随系统' },
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
]
const selected = computed({
  get: () => s.appearanceDraft.theme,
  set: (value: string) => s.chooseTheme(value as Theme),
})
</script>
<template>
  <UiState
    v-if="!s.savedPreferences.value"
    :kind="s.sections.appearance.status === 'loading' ? 'loading' : 'error'"
    title="读取界面偏好"
    :description="s.sections.appearance.message"
    ><UiButton :disabled="s.auth.state.busy" @click="s.load('appearance')"
      >重新加载偏好</UiButton
    ></UiState
  >
  <form v-else class="ui-stack" @submit.prevent="s.saveTheme">
    <UiRadioGroup
      v-model="selected"
      :options="options"
      legend="显示主题"
      :disabled="s.locked.value"
    />
    <p class="meta">选择立即预览，点击保存后才写入账号。只有“跟随系统”响应系统配色变化。</p>
    <p v-if="s.appearanceDirty.value" role="status">
      {{ s.unresolved.value === 'appearance' ? '预览未确认保存' : '当前主题为尚未保存的预览' }}
    </p>
    <p v-if="s.appearanceVersionChanged.value" role="status">
      已保存偏好版本已变化；当前预览与原版本仍保留。
    </p>
    <p
      v-if="s.sections.appearance.message"
      :role="s.sections.appearance.status === 'error' ? 'alert' : 'status'"
    >
      {{ s.sections.appearance.message }}
    </p>
    <div class="ui-row">
      <UiButton type="submit" :disabled="s.locked.value || !s.appearanceDirty.value"
        >保存主题</UiButton
      ><UiButton variant="ghost" :disabled="s.locked.value" @click="s.resetAppearance"
        >取消预览</UiButton
      ><UiButton variant="ghost" :disabled="s.auth.state.busy" @click="s.reload('appearance')"
        >重新加载最新偏好</UiButton
      >
    </div>
    <div v-if="s.unresolved.value === 'appearance'" class="ui-row">
      <UiButton :disabled="s.auth.state.busy" @click="s.checkCurrent('appearance')"
        >检查当前资料</UiButton
      ><UiButton :disabled="s.auth.state.busy" @click="s.retryOriginal">重试原请求</UiButton
      ><UiButton variant="ghost" @click="s.abandonOperation">放弃本次页面操作</UiButton>
    </div>
  </form>
</template>
