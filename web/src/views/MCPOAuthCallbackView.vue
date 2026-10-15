<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { UiButton, UiState } from '../components/ui'
import { useSession } from '../composables/useSession'

const session = useSession(),
  router = useRouter()
const oauth = session.projectMCP.oauth
const busy = ref(false),
  failed = ref(false),
  heading = ref<HTMLElement | null>(null)
const progress = computed(() => oauth.progress)
const confirmed = computed(
  () => progress.value?.contextValid && progress.value.phase === 'confirmed',
)
const uncertain = computed(
  () => progress.value?.contextValid && progress.value.phase === 'uncertain',
)
const stopped = computed(() => progress.value?.contextValid && progress.value.phase === 'stopped')
const destination = computed(() =>
  progress.value?.contextValid ? progress.value.returnPath : '/projects',
)
// The first router guard normally captures this before restoring the session.
// Keep a synchronous fallback for an already authenticated direct mount.
oauth.captureReturn(window.location.href, (path) =>
  window.history.replaceState(window.history.state, '', path),
)
async function run(lookup: boolean) {
  if (busy.value) return
  busy.value = true
  failed.value = false
  try {
    await (lookup ? oauth.lookup() : oauth.completeReturn())
  } catch {
    failed.value = true
  } finally {
    busy.value = false
  }
}
onMounted(() => {
  heading.value?.focus()
  void run(false)
})
</script>

<template>
  <main id="main-content" class="oauth-callback">
    <section class="oauth-panel" aria-labelledby="mcp-oauth-title">
      <header>
        <h1 id="mcp-oauth-title" ref="heading" tabindex="-1">连接 MCP 服务</h1>
        <p class="description">确认服务授权并更新项目连接。</p>
      </header>
      <UiState v-if="busy" kind="loading" title="正在确认授权" description="请保持此页面打开。" />
      <div v-else-if="confirmed" role="status" class="oauth-result">
        <p>授权已完成，连接已更新。</p>
      </div>
      <UiState
        v-else-if="uncertain"
        kind="error"
        title="授权结果尚未确认"
        :description="
          progress?.observation === 'pending'
            ? '服务仍在处理原操作，请稍后查询。'
            : progress?.observation === 'not_found'
              ? '暂未找到原操作结果，请稍后再次查询。'
              : '查询原操作以确认连接状态。'
        "
      />
      <UiState
        v-else-if="stopped"
        kind="error"
        title="原授权操作已停止"
        description="远端结果无法确认，原操作不会再次发送。请返回 MCP 设置检查连接，再决定是否重新发起授权。"
      />
      <UiState
        v-else-if="failed || progress?.phase === 'rejected'"
        kind="error"
        title="未能完成授权"
        description="请返回项目检查连接，使用发起授权时的账号和会话重新开始。"
      />
      <div class="oauth-actions">
        <UiButton v-if="uncertain" variant="primary" :disabled="busy" @click="run(true)"
          >查询原操作</UiButton
        >
        <UiButton
          :variant="confirmed || stopped ? 'primary' : 'ghost'"
          :disabled="busy"
          @click="router.replace(destination)"
          >{{ confirmed || stopped ? '返回 MCP 设置' : '返回项目' }}</UiButton
        >
      </div>
    </section>
  </main>
</template>

<style scoped>
.oauth-callback {
  display: grid;
  min-height: 65vh;
  place-items: center;
  padding: var(--space);
}
.oauth-panel {
  width: min(100%, 28rem);
  display: grid;
  gap: var(--space);
}
.oauth-panel h1 {
  font-size: 1.25rem;
  font-weight: 600;
  margin: 0 0 8px;
}
.oauth-panel .description {
  color: var(--muted);
  margin: 0;
}
.oauth-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
