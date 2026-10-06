<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiState from '../../components/ui/UiState.vue'
import { useSystemUserDirectory } from '../../composables/useSystemUserDirectory'
const directory = useSystemUserDirectory()
const state = directory.state
const heading = ref<HTMLElement | null>(null)
const actions = ref<HTMLElement | null>(null)
let waitingControl: HTMLButtonElement | null = null
function restoreControlFocus() {
  if (!waitingControl || directory.blocked.value) return
  const control = waitingControl
  waitingControl = null
  if (control.isConnected && !control.disabled) control.focus()
  else actions.value?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()
}
async function perform(work: () => Promise<void>, event: Event) {
  const control = event.currentTarget as HTMLButtonElement
  await work()
  waitingControl = control
  await nextTick()
  restoreControlFocus()
}
watch(directory.blocked, async (busy) => {
  if (!busy) {
    await nextTick()
    restoreControlFocus()
  }
})
onMounted(async () => {
  await nextTick()
  heading.value?.focus()
})
onUnmounted(directory.dispose)
const visibleTime = (value: string) => value.slice(0, 10) + ' ' + value.slice(11, 19)
</script>
<template>
  <div class="system-users">
    <h1 ref="heading" tabindex="-1">用户</h1>
    <p class="meta">系统账号目录。注册时间使用 UTC。</p>
    <div ref="actions" class="directory-actions" aria-label="用户目录分页">
      <UiButton :disabled="directory.blocked.value" @click="perform(directory.refresh, $event)"
        >刷新</UiButton
      >
      <UiButton
        :disabled="directory.blocked.value || !state.hasPrevious"
        @click="perform(directory.previous, $event)"
        >上一页</UiButton
      >
      <UiButton
        :disabled="directory.blocked.value || !state.hasNext"
        @click="perform(directory.next, $event)"
        >下一页</UiButton
      >
    </div>
    <UiState
      v-if="state.phase === 'waiting' || state.phase === 'loading'"
      kind="loading"
      title="正在读取用户目录"
    />
    <UiState
      v-else-if="state.phase === 'error'"
      kind="error"
      title="用户目录读取失败"
      :description="state.message"
    >
      <UiButton
        v-if="state.cursorInvalid"
        :disabled="directory.blocked.value"
        @click="perform(directory.refresh, $event)"
        >返回首页重新加载</UiButton
      >
      <UiButton v-else :disabled="directory.blocked.value" @click="perform(directory.retry, $event)"
        >重试</UiButton
      >
    </UiState>
    <UiState v-else-if="state.phase === 'empty'" kind="empty" title="暂无用户" />
    <div
      v-else-if="state.phase === 'ready'"
      class="directory-scroll"
      role="region"
      aria-label="用户列表"
      tabindex="0"
    >
      <table>
        <caption class="sr-only">
          系统用户目录
        </caption>
        <thead>
          <tr>
            <th scope="col">邮箱</th>
            <th scope="col">用户名</th>
            <th scope="col">显示名</th>
            <th scope="col">角色</th>
            <th scope="col">注册时间（UTC）</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="user in state.rows" :key="user.id">
            <td data-label="邮箱">{{ user.email }}</td>
            <td data-label="用户名">{{ user.username || '—' }}</td>
            <td data-label="显示名">{{ user.display_name || user.email }}</td>
            <td data-label="角色">{{ user.role === 'admin' ? '管理员' : '普通用户' }}</td>
            <td data-label="注册时间（UTC）">
              <time
                :datetime="user.created_at"
                :aria-label="user.created_at"
                :title="user.created_at"
                >{{ visibleTime(user.created_at) }}</time
              >
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="state.phase === 'ready' || state.phase === 'empty'" class="meta" role="status">
      本页 {{ state.rows.length }} 位用户
    </p>
  </div>
</template>
<style scoped>
.system-users {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
}
.directory-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.directory-scroll {
  overflow-x: auto;
  min-width: 0;
  max-width: 100%;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
table {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  text-align: start;
}
th,
td {
  padding: 12px;
  vertical-align: top;
  white-space: normal;
  overflow-wrap: anywhere;
  border-bottom: 1px solid var(--border);
}
th {
  font-weight: 600;
  background: var(--surface);
}
th:nth-child(1),
th:nth-child(3) {
  width: 25%;
}
th:nth-child(2) {
  width: 18%;
}
th:nth-child(4) {
  width: 12%;
}
th:nth-child(5) {
  width: 20%;
}
tbody tr:last-child td {
  border-bottom: 0;
}
time {
  font-variant-numeric: tabular-nums;
}
@media (max-width: 760px) {
  table {
    display: block;
  }
  thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  tbody,
  tr,
  td {
    display: block;
  }
  tr + tr {
    border-top: 1px solid var(--border);
  }
  td {
    border: 0;
    padding: 8px 12px;
  }
  td::before {
    content: attr(data-label);
    display: block;
    color: var(--muted);
    font-size: 12px;
  }
}
</style>
