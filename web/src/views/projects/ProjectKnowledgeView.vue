<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute } from 'vue-router'
import KnowledgeDocumentTree from '../../components/knowledge/KnowledgeDocumentTree.vue'
import { UiBadge, UiBreadcrumb, UiButton, UiDrawer, UiState } from '../../components/ui'
import { useKnowledgeOwner } from '../../composables/useKnowledgeOwner'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { projectRoute } from '../../router/auth'

const route = useRoute(),
  workspace = useProjectWorkspace()
const location = computed(() => {
  const address = projectRoute(route.fullPath)
  return {
    projectPath: address ? `/${address.username}/${address.project_name}` : '',
    documentID: typeof route.params.document_id === 'string' ? route.params.document_id : null,
  }
})
const owner = useKnowledgeOwner(undefined, workspace, location),
  state = owner.state
const drawer = ref(false),
  heading = ref<HTMLElement | null>(null),
  documentHeading = ref<HTMLElement | null>(null)
const indexLabels = {
  pending: '索引中',
  processing: '索引中',
  ready: '就绪',
  failed: '索引失败',
} as const
const crumbs = computed(() => [
  { label: '知识库', to: workspace.paths.value.home + '/knowledge' },
  ...state.ancestors.items.map((document) => ({
    label: document.title,
    to: `${workspace.paths.value.home}/knowledge/${document.id}`,
  })),
  { label: state.document?.title ?? '当前文档' },
])
const body = computed(() =>
  state.content.value && 'text' in state.content.value ? state.content.value.text : null,
)
const creator = computed(() => {
  const value = state.document?.created_by
  return !value
    ? ''
    : value.kind === 'human'
      ? value.user_id
      : `${value.agent_id} / ${value.execution_id}`
})
async function select(id: string) {
  owner.select(id)
  drawer.value = false
  await nextTick()
  if (owner.visible.value && state.selected === id) documentHeading.value?.focus()
}
const cancel = () => owner.cancel()
onBeforeRouteLeave(cancel)
onBeforeRouteUpdate((to, from) => {
  if (projectRoute(from.fullPath)?.path !== to.fullPath) cancel()
})
onMounted(async () => {
  await nextTick()
  if (owner.visible.value) heading.value?.focus()
})
onBeforeUnmount(() => owner.dispose())
</script>

<template>
  <section class="knowledge-page" aria-label="项目知识库">
    <header class="knowledge-heading">
      <h1 ref="heading" tabindex="-1">知识库</h1>
      <UiButton class="drawer-trigger" :disabled="!owner.visible.value" @click="drawer = true"
        >文档树</UiButton
      >
      <UiButton :disabled="owner.blocked.value" @click="owner.refreshTree()">重读文档树</UiButton>
      <UiButton v-if="owner.busy.value" @click="cancel">停止本次读取</UiButton>
    </header>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认知识库访问身份" />
    <div v-else class="knowledge-layout">
      <aside class="desktop-tree" aria-label="文档目录">
        <KnowledgeDocumentTree
          :levels="state.levels"
          :expanded="state.expanded"
          :selected="state.selected"
          :busy="owner.busy.value"
          @select="select"
          @expand="owner.setExpanded"
          @read="owner.readLevel"
        />
      </aside>
      <div class="knowledge-document" aria-live="polite">
        <UiState
          v-if="!state.selected"
          kind="empty"
          title="选择一篇文档"
          description="从文档树选择父文档或子文档，查看当前内容。"
        />
        <template v-else>
          <h2 ref="documentHeading" tabindex="-1">{{ state.document?.title ?? '文档' }}</h2>
          <UiState
            v-if="['waiting', 'loading'].includes(state.metadataPhase)"
            kind="loading"
            title="正在读取文档信息"
          />
          <UiState
            v-else-if="['error', 'unavailable', 'deleted'].includes(state.metadataPhase)"
            kind="error"
            :title="state.metadataPhase === 'deleted' ? '文档已删除' : '文档不可用'"
            :description="state.metadataMessage"
          >
            <UiButton :disabled="owner.blocked.value" @click="owner.retryDocument()"
              >重新读取文档</UiButton
            >
          </UiState>
          <template v-if="state.document">
            <div class="document-toolbar">
              <UiBadge
                :tone="state.document.indexing_status === 'failed' ? 'warning' : 'neutral'"
                >{{ indexLabels[state.document.indexing_status] }}</UiBadge
              >
              <UiButton :disabled="owner.blocked.value" @click="owner.retryDocument()"
                >从开头重读</UiButton
              >
              <RouterLink :to="`${workspace.paths.value.home}/knowledge/${state.document.id}`"
                >文档链接</RouterLink
              >
            </div>
            <UiBreadcrumb v-if="state.ancestors.phase === 'ready'" :items="crumbs" />
            <p v-else-if="state.ancestors.phase === 'loading'" role="status">正在读取文档位置</p>
            <div v-else-if="state.ancestors.phase !== 'idle'" class="path-error">
              <p role="status">{{ state.ancestors.message }}</p>
              <UiButton :disabled="owner.blocked.value" @click="owner.retryAncestors()"
                >重新读取路径</UiButton
              >
            </div>
            <dl class="document-facts">
              <div>
                <dt>内容来源</dt>
                <dd>{{ state.document.source_kind === 'text' ? '文本' : '文件' }}</dd>
              </div>
              <div>
                <dt>媒体类型</dt>
                <dd>{{ state.document.media_type }}</dd>
              </div>
              <div>
                <dt>版本</dt>
                <dd>{{ state.document.content_version }}</dd>
              </div>
              <div>
                <dt>创建者</dt>
                <dd>{{ creator }}</dd>
              </div>
              <div>
                <dt>创建时间</dt>
                <dd>
                  <time :datetime="state.document.created_at">{{ state.document.created_at }}</time>
                </dd>
              </div>
              <div>
                <dt>更新时间</dt>
                <dd>
                  <time :datetime="state.document.updated_at">{{ state.document.updated_at }}</time>
                </dd>
              </div>
            </dl>
            <UiState
              v-if="state.content.phase === 'loading'"
              kind="loading"
              title="正在读取当前正文"
            />
            <UiState
              v-else-if="
                ['error', 'changed', 'deleted', 'unavailable'].includes(state.content.phase)
              "
              kind="error"
              title="正文读取未完成"
              :description="state.content.message"
            />
            <template v-else-if="state.content.phase === 'ready'">
              <UiState
                v-if="state.content.value && 'unavailable' in state.content.value"
                kind="empty"
                title="此文档暂不支持正文读取"
              />
              <template v-else-if="body">
                <p class="content-range meta">
                  当前正文段：字节 {{ state.content.offset }}–{{ body.next_byte_offset
                  }}<span v-if="body.truncated">，后续内容尚未读取</span>
                </p>
                <pre
                  v-if="body.text"
                  class="canonical-text"
                  tabindex="0"
                  aria-label="当前文档正文"
                  >{{ body.text }}</pre>
                <UiState v-else kind="empty" title="正文为空" />
                <div class="content-navigation">
                  <UiButton :disabled="!owner.canPrevious.value" @click="owner.previous()"
                    >上一段</UiButton
                  >
                  <UiButton :disabled="!owner.canNext.value" @click="owner.next()">下一段</UiButton>
                </div>
              </template>
            </template>
          </template>
        </template>
      </div>
    </div>
    <UiDrawer v-model:open="drawer" title="文档树">
      <KnowledgeDocumentTree
        v-if="owner.visible.value"
        :levels="state.levels"
        :expanded="state.expanded"
        :selected="state.selected"
        :busy="owner.busy.value"
        @select="select"
        @expand="owner.setExpanded"
        @read="owner.readLevel"
      />
    </UiDrawer>
  </section>
</template>

<style scoped>
.knowledge-page {
  min-width: 0;
  padding: var(--space);
}
.knowledge-heading,
.document-toolbar,
.content-navigation {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.knowledge-heading {
  margin-bottom: var(--space);
}
.knowledge-layout {
  display: grid;
  grid-template-columns: minmax(240px, 30%) minmax(0, 1fr);
  gap: var(--space);
}
.desktop-tree {
  min-width: 0;
  padding-right: var(--content-gap);
  border-right: 1px solid var(--border);
}
.knowledge-document {
  display: grid;
  align-content: start;
  gap: var(--content-gap);
  min-width: 0;
}
.knowledge-document h2,
.document-facts dd {
  overflow-wrap: anywhere;
}
.document-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--content-gap);
  margin: 0;
}
.document-facts dt {
  color: var(--muted);
}
.document-facts dd {
  margin: 0;
}
.canonical-text {
  max-width: 100%;
  max-height: 60vh;
  min-height: 120px;
  overflow: auto;
  margin: 0;
  padding: var(--content-gap);
  background: var(--surface-alt);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  white-space: pre;
}
.drawer-trigger {
  display: none;
}
@media (max-width: 834px) {
  .knowledge-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .desktop-tree {
    display: none;
  }
  .drawer-trigger {
    display: inline-flex;
  }
  .document-facts {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
