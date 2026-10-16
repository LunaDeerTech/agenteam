<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { UiBadge, UiButton, UiIcon, UiSelect, UiState } from '../../components/ui'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { createProjectCreation } from '../../composables/useProjectCreation'
import ProjectCreateForm from '../../components/projects/ProjectCreateForm.vue'

const s = useProjectWorkspace()
const { visible, list, blocked, hasPrevious, hasNext } = s
const creation = createProjectCreation(undefined, () => s.fromFirst())
const heading = ref<HTMLElement | null>(null)
onUnmounted(creation.dispose)
const lifecycleLabels = {
  active: '活跃',
  archiving: '正在归档',
  archived: '已归档',
  deleting: '正在删除',
} as const
const lifecycleOptions = [
  { value: 'all', label: '全部状态' },
  ...Object.entries(lifecycleLabels).map(([value, label]) => ({ value, label })),
]
const limitOptions = ['25', '50', '100'].map((value) => ({ value, label: value + ' 项' }))
const rows = computed(() => list.items.map((item) => ({ item, path: s.rowPath(item) })))
const creating = computed(
  () => creation.progress.value && creation.progress.value.phase !== 'ready',
)
</script>

<template>
  <section
    class="project-list page-layout page-layout--collection"
    aria-labelledby="project-list-heading"
  >
    <header class="page-header">
      <div class="page-heading">
        <h1 id="project-list-heading" ref="heading" tabindex="-1">项目</h1>
        <p>管理你的项目，继续正在进行的工作。</p>
      </div>
      <UiButton
        v-if="visible && list.phase !== 'checking'"
        variant="primary"
        :disabled="blocked"
        @click="creation.show()"
      >
        <svg v-if="!creating" class="ui-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <path
            d="M12 5v14M5 12h14"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
          />
        </svg>
        {{ creating ? '查看创建状态' : '新建项目' }}
      </UiButton>
    </header>
    <UiState v-if="!visible || list.phase === 'checking'" kind="loading" title="正在确认当前身份" />
    <template v-else>
      <ProjectCreateForm :owner="creation" :fallback-focus="heading" />
      <div class="list-toolbar page-toolbar" aria-label="项目筛选">
        <div class="filter-field">
          <label for="project-lifecycle-filter">项目状态</label>
          <UiSelect
            id="project-lifecycle-filter"
            label="项目状态"
            :model-value="list.filter"
            :options="lifecycleOptions"
            :disabled="blocked"
            @update:model-value="s.setFilter"
          />
        </div>
        <UiButton class="list-refresh" variant="ghost" :disabled="blocked" @click="s.fromFirst()">
          <svg class="ui-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              d="M20 11a8 8 0 1 0-2 6M20 4v7h-7"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          刷新列表
        </UiButton>
      </div>
      <div class="project-list-surface page-surface">
        <div class="list-content" :aria-busy="list.phase === 'loading' || undefined">
          <UiState
            v-if="list.phase === 'loading' || list.phase === 'inactive'"
            kind="loading"
            title="正在读取项目列表"
          />
          <UiState
            v-else-if="list.phase === 'read-error'"
            kind="error"
            title="项目列表读取失败"
            :description="list.message"
          >
            <UiButton :disabled="blocked" @click="s.retryList()">重读当前页</UiButton>
          </UiState>
          <UiState
            v-else-if="list.phase === 'unavailable'"
            kind="error"
            title="项目列表不可用"
            :description="list.message"
          >
            <UiButton :disabled="blocked" @click="s.fromFirst()">重新读取项目列表</UiButton>
          </UiState>
          <UiState
            v-else-if="list.phase === 'current' && !list.items.length"
            kind="empty"
            :title="list.filter === 'all' ? '从第一个项目开始' : '没有符合条件的项目'"
            :description="
              list.filter === 'all'
                ? '创建项目，集中管理任务、知识与产物。'
                : '试试其他项目状态，或查看全部项目。'
            "
          >
            <UiButton v-if="list.filter === 'all'" :disabled="blocked" @click="creation.show()">
              {{ creating ? '查看创建状态' : '新建项目' }}
            </UiButton>
            <UiButton v-else :disabled="blocked" @click="s.setFilter('all')">查看全部项目</UiButton>
          </UiState>
          <p v-if="list.stale && rows.length" class="stale-notice" role="status">
            以下是上次完整读取的第 {{ list.page }} 页；本次读取尚未成功。
          </p>
          <template v-if="list.phase !== 'unavailable' && rows.length">
            <div class="project-columns" aria-hidden="true">
              <span>项目名称</span>
              <span>状态</span>
              <span class="project-entry-label">操作</span>
            </div>
            <ul class="project-rows" aria-label="项目列表">
              <li v-for="{ item, path } in rows" :key="item.id" class="project-row">
                <h2 :title="item.name">
                  <RouterLink v-if="path" :to="path">{{ item.name }}</RouterLink>
                  <span v-else>{{ item.name }}</span>
                </h2>
                <div class="project-status">
                  <UiBadge
                    :tone="
                      item.lifecycle === 'active' || item.lifecycle === 'archived'
                        ? 'neutral'
                        : 'warning'
                    "
                    >{{ lifecycleLabels[item.lifecycle] }}</UiBadge
                  >
                </div>
                <p v-if="item.lifecycle === 'deleting'" class="project-description">
                  删除正在处理中，项目内容不可访问。
                </p>
                <p
                  v-else-if="item.description"
                  class="project-description"
                  :title="item.description"
                >
                  {{ item.description }}
                </p>
                <RouterLink
                  v-if="path"
                  class="project-entry"
                  :to="path"
                  :aria-label="`${item.lifecycle === 'archived' ? '查看' : '进入'}项目 ${item.name}`"
                >
                  <span>{{ item.lifecycle === 'archived' ? '查看项目' : '进入项目' }}</span>
                  <UiIcon name="arrow-right" />
                </RouterLink>
                <span v-else class="project-entry-unavailable">暂不可用</span>
              </li>
            </ul>
          </template>
        </div>
        <footer v-if="list.phase !== 'unavailable'" class="list-footer">
          <div class="filter-field page-limit">
            <label for="project-page-limit">每页</label>
            <UiSelect
              id="project-page-limit"
              label="每页数量"
              :model-value="String(list.limit)"
              :options="limitOptions"
              :disabled="blocked"
              @update:model-value="s.setLimit"
            />
          </div>
          <nav class="list-pagination" aria-label="项目分页">
            <span>第 {{ list.page }} 页</span>
            <UiButton
              icon
              :disabled="blocked || !hasPrevious"
              aria-label="上一页"
              @click="s.previous()"
              ><UiIcon name="arrow-left"
            /></UiButton>
            <UiButton icon :disabled="blocked || !hasNext" aria-label="下一页" @click="s.next()"
              ><UiIcon name="arrow-right"
            /></UiButton>
          </nav>
        </footer>
      </div>
    </template>
  </section>
</template>

<style scoped>
.project-list-surface {
  overflow: clip;
}
.filter-field,
.list-pagination {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.filter-field label,
.list-pagination > span {
  color: var(--muted);
  font-size: 12px;
  flex: none;
}
.filter-field :deep(.ui-select) {
  width: 144px;
}
.page-limit :deep(.ui-select) {
  width: 88px;
}
.list-content :deep(.ui-state) {
  min-height: 248px;
  border: 0;
  border-radius: 0;
  background: transparent;
}
.project-columns,
.project-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 112px 112px;
  align-items: center;
  column-gap: 24px;
  padding-inline: 20px;
}
.project-columns {
  padding-block: 10px;
  background: var(--canvas);
  border-bottom: 1px solid var(--border);
  color: var(--muted);
  font-size: 12px;
}
.project-entry-label {
  text-align: right;
}
.project-rows {
  margin: 0;
  padding: 0;
  list-style: none;
}
.project-row {
  grid-template-rows: auto auto;
  row-gap: 4px;
  min-height: 64px;
  padding-block: 12px;
  transition: background var(--motion) var(--ease);
}
.project-row + .project-row {
  border-top: 1px solid var(--border);
}
.project-row:hover,
.project-row:focus-within {
  background: var(--canvas);
}
.project-row h2 {
  grid-column: 1;
  grid-row: 1;
  min-width: 0;
  font-size: 14px;
}
.project-row h2,
.project-description {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.project-row h2 a {
  color: var(--text);
}
.project-description {
  grid-column: 1;
  grid-row: 2;
  color: var(--muted);
  font-size: 12px;
}
.project-status {
  display: flex;
  grid-column: 2;
  grid-row: 1 / 3;
}
.project-entry,
.project-entry-unavailable {
  grid-column: 3;
  grid-row: 1 / 3;
  justify-self: end;
  font-size: 12px;
}
.project-entry {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0 6px 8px;
}
.project-entry .ui-icon {
  width: 14px;
  height: 14px;
}
.project-entry-unavailable {
  color: var(--muted);
}
.stale-notice {
  padding: 12px 20px;
  color: var(--warning);
  background: var(--warning-bg);
}
.list-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 12px 20px;
  border-top: 1px solid var(--border);
  background: var(--canvas);
}
.list-pagination {
  gap: 8px;
}
.list-pagination > span {
  margin-right: 8px;
}
@media (max-width: 650px) {
  .page-heading {
    flex: 1 1 100%;
  }
  .list-toolbar .filter-field {
    gap: 8px;
  }
  .list-toolbar .filter-field :deep(.ui-select) {
    width: 128px;
  }
  .project-columns {
    display: none;
  }
  .project-row {
    grid-template-columns: minmax(0, 1fr) auto;
    grid-template-rows: auto auto auto;
    gap: 8px 12px;
    padding: 16px;
  }
  .project-row h2 {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .project-status {
    grid-row: 1;
    justify-self: end;
  }
  .project-description {
    grid-column: 1 / -1;
  }
  .project-entry,
  .project-entry-unavailable {
    grid-column: 2;
    grid-row: 3;
    min-height: 44px;
  }
  .list-footer {
    padding: 12px;
  }
  .list-pagination :deep(button) {
    min-width: 44px;
    min-height: 44px;
  }
}
</style>
