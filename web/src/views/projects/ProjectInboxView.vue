<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { UiBadge, UiButton, UiRadioGroup, UiState } from '../../components/ui'
import { useProjectInbox } from '../../composables/useProjectInbox'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { skillsNavigationReadReady } from '../../router/auth'

const route = useRoute(),
  ready = skillsNavigationReadReady(useRouter()),
  workspace = useProjectWorkspace()
const owner = useProjectInbox(
  undefined,
  workspace,
  computed(() => (ready.value ? route.fullPath : '')),
)
const heading = ref<HTMLElement | null>(null)
const filters = [
  { value: 'open', label: '待处理' },
  { value: 'resolved', label: '已处理' },
]
const filter = computed({
  get: () => owner.status.value,
  set: (value: string) => {
    if ((value === 'open' || value === 'resolved') && owner.filter(value))
      void nextTick(() => {
        if (owner.visible.value) heading.value?.focus()
      })
  },
})
const page = owner.page
onBeforeRouteLeave(() => owner.cancel())
onBeforeRouteUpdate(() => owner.cancel())
onBeforeUnmount(() => owner.dispose())
</script>

<template>
  <section class="page-layout page-layout--collection inbox-page" aria-label="项目收件箱">
    <header class="page-header">
      <div class="page-heading">
        <h1 ref="heading" tabindex="-1">收件箱</h1>
        <p>处理当前项目的会议提议与待答决议，打开会议查看详情。</p>
      </div>
      <div class="page-actions">
        <UiButton v-if="owner.reading.value" @click="owner.cancel()">停止读取</UiButton>
        <UiButton :disabled="owner.blocked.value" @click="owner.refresh()">刷新收件箱</UiButton>
      </div>
    </header>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认项目访问身份" />
    <template v-else>
      <div class="page-toolbar">
        <UiRadioGroup
          v-model="filter"
          :options="filters"
          legend="处理状态"
          :disabled="owner.blocked.value"
        />
        <RouterLink :to="workspace.paths.value.home">返回项目</RouterLink>
      </div>
      <UiState
        v-if="page.phase === 'loading' || page.phase === 'waiting'"
        kind="loading"
        title="正在读取收件箱"
      />
      <UiState
        v-else-if="page.phase === 'error' || page.phase === 'unavailable'"
        kind="error"
        :title="page.phase === 'unavailable' ? '收件箱不可用' : '收件箱读取失败'"
        :description="page.message"
      >
        <UiButton :disabled="owner.blocked.value" @click="owner.refresh()">重新读取</UiButton>
      </UiState>
      <UiState
        v-else-if="page.items.length === 0"
        kind="empty"
        :title="owner.status.value === 'open' ? '暂无待处理事项' : '暂无已处理事项'"
        :description="
          owner.status.value === 'open'
            ? 'Agent 提出会议或请求决议后，可在这里查看并处理。'
            : '处理过的会议提议与决议会保留在这里。'
        "
      />
      <ul v-else class="inbox-list page-surface" aria-label="会议待办事项">
        <li v-for="item in page.items" :key="item.id" class="inbox-row">
          <div class="inbox-copy">
            <RouterLink class="inbox-title" :to="owner.meetingPath(item)">{{
              item.title || (item.source_type === 'decision_request' ? '待答决议' : '会议提议')
            }}</RouterLink>
            <p v-if="item.summary" class="inbox-summary">{{ item.summary }}</p>
          </div>
          <UiBadge class="inbox-status" :tone="item.status === 'open' ? 'warning' : 'neutral'">{{
            item.status === 'open' ? '待处理' : '已处理'
          }}</UiBadge>
          <RouterLink
            class="inbox-open"
            :to="owner.meetingPath(item)"
            :aria-label="
              (item.source_type === 'decision_request' ? '打开决议：' : '打开会议提议：') +
              (item.title || (item.source_type === 'decision_request' ? '待答决议' : '会议提议'))
            "
            >{{
              item.source_type === 'decision_request'
                ? item.status === 'open'
                  ? '回答决议'
                  : '查看决议'
                : '打开提议'
            }}</RouterLink
          >
        </li>
      </ul>
      <footer v-if="page.phase === 'ready' && page.items.length > 0" class="page-footer">
        <p class="inbox-count" role="status">
          已显示 {{ page.items.length }} 条{{
            owner.status.value === 'open' ? '待处理' : '已处理'
          }}事项
        </p>
        <UiButton v-if="page.nextBefore" :disabled="owner.blocked.value" @click="owner.more()"
          >加载更多</UiButton
        >
      </footer>
    </template>
  </section>
</template>

<style scoped>
.inbox-list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.inbox-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 16px;
  min-height: 64px;
  padding: 12px 16px;
}
.inbox-row + .inbox-row {
  border-top: 1px solid var(--border);
}
.inbox-copy {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.inbox-title {
  color: var(--text);
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.inbox-summary {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  overflow-wrap: anywhere;
  color: var(--muted);
}
.inbox-count {
  color: var(--muted);
}
.inbox-status {
  justify-self: start;
}
.inbox-open {
  white-space: nowrap;
}
@media (max-width: 650px) {
  .inbox-row {
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 8px 12px;
    padding: 12px;
  }
  .inbox-copy {
    grid-column: 1 / -1;
  }
  .inbox-open {
    justify-self: end;
    min-height: 44px;
    display: inline-flex;
    align-items: center;
  }
}
</style>
