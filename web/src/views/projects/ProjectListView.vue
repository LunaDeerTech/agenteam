<script setup lang="ts">
import { computed } from 'vue'
import { UiBadge, UiButton, UiSelect, UiState } from '../../components/ui'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'

const s = useProjectWorkspace()
const { visible, list, blocked, hasPrevious, hasNext } = s
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
</script>

<template>
  <section class="project-list" aria-labelledby="project-list-heading">
    <h1 id="project-list-heading" tabindex="-1">项目</h1>
    <UiState v-if="!visible || list.phase === 'checking'" kind="loading" title="正在确认当前身份" />
    <template v-else>
      <p class="meta">查看当前账号拥有的项目。</p>
      <div class="list-toolbar" aria-label="项目筛选">
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
        <div class="filter-field">
          <label for="project-page-limit">每页数量</label>
          <UiSelect
            id="project-page-limit"
            label="每页数量"
            :model-value="String(list.limit)"
            :options="limitOptions"
            :disabled="blocked"
            @update:model-value="s.setLimit"
          />
        </div>
        <UiButton :disabled="blocked" @click="s.fromFirst()">从首页重新读取</UiButton>
      </div>
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
        :title="list.filter === 'all' ? '当前没有项目' : '当前筛选下没有项目'"
        description="本页没有可显示的项目。"
      />
      <p v-if="list.stale && rows.length" class="stale-notice" role="status">
        以下是上次完整读取的第 {{ list.page }} 页；本次读取尚未成功。
      </p>
      <ul
        v-if="list.phase !== 'unavailable' && rows.length"
        class="project-rows"
        aria-label="项目列表"
      >
        <li v-for="{ item, path } in rows" :key="item.id" class="project-row">
          <div class="project-row-heading">
            <h2>
              <RouterLink v-if="path" :to="path">{{ item.name }}</RouterLink>
              <span v-else>{{ item.name }}</span>
            </h2>
            <UiBadge
              :tone="
                item.lifecycle === 'active'
                  ? 'success'
                  : item.lifecycle === 'archived'
                    ? 'neutral'
                    : 'warning'
              "
              >{{ lifecycleLabels[item.lifecycle] }}</UiBadge
            >
          </div>
          <p v-if="item.lifecycle !== 'deleting'" class="project-description">
            {{ item.description || '未填写描述。' }}
          </p>
          <p v-else class="meta">删除正在处理中，项目内容不可访问。</p>
          <dl class="project-row-meta">
            <div>
              <dt>版本</dt>
              <dd>
                <code>{{ item.version }}</code>
              </dd>
            </div>
            <div v-if="item.lifecycle === 'archiving' || item.lifecycle === 'deleting'">
              <dt>{{ item.lifecycle === 'archiving' ? '归档操作' : '删除操作' }}</dt>
              <dd>
                <code>{{ item.operation_id }}</code>
              </dd>
            </div>
          </dl>
        </li>
      </ul>
      <div v-if="list.phase !== 'unavailable'" class="list-pagination" aria-label="项目分页">
        <UiButton :disabled="blocked || !hasPrevious" @click="s.previous()">上一页</UiButton>
        <span>第 {{ list.page }} 页</span>
        <UiButton :disabled="blocked || !hasNext" @click="s.next()">下一页</UiButton>
      </div>
    </template>
  </section>
</template>

<style scoped>
.project-list {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
  padding: var(--space);
}
.list-toolbar,
.list-pagination,
.project-row-heading {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
  align-items: center;
}
.list-toolbar {
  align-items: end;
  margin-block: 4px var(--content-gap);
}
.filter-field {
  display: grid;
  gap: 6px;
  min-width: 140px;
  max-width: 100%;
}
.project-rows {
  display: grid;
  gap: var(--content-gap);
  margin: 0;
  padding: 0;
  list-style: none;
}
.project-row {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
}
.project-row-heading h2 {
  min-width: 0;
  font-size: 16px;
}
.project-row-heading h2 a,
.project-row-heading h2 span {
  overflow-wrap: anywhere;
}
.project-row-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
  margin: 0;
}
.project-row-meta > div {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}
dt,
.meta,
.stale-notice {
  color: var(--muted);
}
dd {
  min-width: 0;
  margin: 0;
}
p,
code {
  overflow-wrap: anywhere;
}
.project-description {
  white-space: pre-wrap;
}
.list-pagination {
  padding-block: var(--content-gap);
}
@media (max-width: 600px) {
  .list-toolbar {
    align-items: stretch;
  }
  .filter-field {
    flex: 1 1 140px;
    min-width: 0;
  }
  .project-row-meta,
  .project-row-meta > div {
    display: grid;
    gap: 4px;
  }
}
</style>
