<script setup lang="ts">
import KnowledgeMoveDialog from '../../components/knowledge/KnowledgeMoveDialog.vue'
import { useKnowledgeMove } from '../../composables/useKnowledgeMove'
import KnowledgeDeleteDialog from '../../components/knowledge/KnowledgeDeleteDialog.vue'
import { useKnowledgeDelete } from '../../composables/useKnowledgeDelete'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useKnowledgeText } from '../../composables/useKnowledgeText'
import { useSession } from '../../composables/useSession'
import { useKnowledgeTitleSearch } from '../../composables/useKnowledgeTitleSearch'
import KnowledgeTitleSearch from '../../components/knowledge/KnowledgeTitleSearch.vue'
import type { KnowledgeDocument } from '../../api/knowledge-owner'
import KnowledgeRenameDialog from '../../components/knowledge/KnowledgeRenameDialog.vue'
import { useKnowledgeRename } from '../../composables/useKnowledgeRename'
import KnowledgeDocumentTree from '../../components/knowledge/KnowledgeDocumentTree.vue'
import { UiBadge, UiBreadcrumb, UiButton, UiDrawer, UiState } from '../../components/ui'
import { useKnowledgeOwner } from '../../composables/useKnowledgeOwner'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { installKnowledgeNavigation, projectRoute } from '../../router/auth'

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
const rename = useKnowledgeRename(owner, workspace)
const deletion = useKnowledgeDelete(owner, workspace)
const move = useKnowledgeMove(owner, workspace)
const textEditor = useKnowledgeText()
const detachText = textEditor.attachReader(owner)
const titleSearch = useKnowledgeTitleSearch(
  useSession().knowledge,
  computed(() => (owner.visible.value ? workspace.currentReadContext.value : null)),
  owner.blocked,
)
watch(
  () => move.progress.value?.phase,
  (phase) => {
    if (phase === 'confirmed') titleSearch.clear()
  },
)
async function createText(parent: KnowledgeDocument | null = null) {
  if (
    (await move.permitNavigation()) &&
    (await deletion.permitNavigation()) &&
    (await rename.permitNavigation())
  )
    await textEditor.openCreate(parent)
}
async function editText() {
  if (
    state.document &&
    (await move.permitNavigation()) &&
    (await deletion.permitNavigation()) &&
    (await rename.permitNavigation())
  )
    await textEditor.openEdit(state.document)
}
async function deleteTree() {
  const selected = state.selected,
    context = workspace.currentReadContext.value
  if (
    !(await move.permitNavigation()) ||
    !(await rename.permitNavigation()) ||
    !(await textEditor.permitNavigation())
  )
    return
  if (state.selected !== selected || workspace.currentReadContext.value !== context) return
  titleSearch.clear()
  await deletion.open()
}
async function renameText() {
  if (
    (await move.permitNavigation()) &&
    (await deletion.permitNavigation()) &&
    (await textEditor.permitNavigation())
  )
    rename.open()
}
async function moveDocument() {
  const context = workspace.currentReadContext.value,
    document = state.document
  if (
    !(await move.permitNavigation()) ||
    !(await deletion.permitNavigation()) ||
    !(await rename.permitNavigation()) ||
    !(await textEditor.permitNavigation())
  )
    return
  if (context !== workspace.currentReadContext.value || document !== state.document) return
  titleSearch.cancel()
  move.open()
}
const stopNavigation = installKnowledgeNavigation(useRouter(), {
  confirmLeave: async () =>
    (await move.permitNavigation()) &&
    (await deletion.permitNavigation()) &&
    (await rename.permitNavigation()),
})
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
async function select(id: string, current: () => boolean = () => true) {
  if (
    id !== state.selected &&
    (!(await move.permitNavigation()) ||
      !(await deletion.permitNavigation()) ||
      !(await rename.permitNavigation()) ||
      !(await textEditor.permitNavigation()))
  )
    return
  if (!current()) return
  const fromDrawer = drawer.value
  owner.select(id)
  drawer.value = false
  await nextTick()
  if (!fromDrawer && owner.visible.value && state.selected === id) documentHeading.value?.focus()
}
function selectSearchResult(id: string) {
  const current = titleSearch.selection(id)
  if (current) void select(id, current)
}
const cancel = () => {
  titleSearch.cancel()
  owner.cancel()
}
async function leave() {
  if (
    !(await move.permitNavigation()) ||
    !(await deletion.permitNavigation()) ||
    !(await rename.permitNavigation()) ||
    !(await textEditor.permitNavigation())
  )
    return false
  cancel()
  return true
}
onBeforeRouteLeave(leave)
onBeforeRouteUpdate((to, from) => (to.fullPath === from.fullPath ? true : leave()))
onMounted(async () => {
  await nextTick()
  if (owner.visible.value) heading.value?.focus()
})
onBeforeUnmount(() => {
  detachText()
  stopNavigation()
  move.dispose()
  deletion.dispose()
  rename.dispose()
  titleSearch.dispose()
  owner.dispose()
})
</script>

<template>
  <section class="knowledge-page page-workspace" aria-label="项目知识库">
    <header class="knowledge-heading">
      <div class="knowledge-heading-copy">
        <h1 ref="heading" tabindex="-1">知识库</h1>
        <p class="meta">从目录选择文档，阅读或维护项目知识。</p>
      </div>
      <div class="knowledge-actions">
        <UiButton class="drawer-trigger" :disabled="!owner.visible.value" @click="drawer = true"
          >文档树</UiButton
        >
        <UiButton
          :disabled="owner.blocked.value || textEditor.state.open"
          @click="owner.refreshTree()"
          >重读文档树</UiButton
        >
        <UiButton v-if="owner.busy.value" @click="cancel">停止本次读取</UiButton>
        <UiButton variant="primary" :disabled="!textEditor.canOpen.value" @click="createText()"
          >新建文本</UiButton
        >
      </div>
    </header>
    <p
      v-if="!textEditor.state.open && textEditor.state.denied"
      class="knowledge-notice"
      role="status"
    >
      {{ textEditor.state.message }}
    </p>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认知识库访问身份" />
    <div v-else class="knowledge-layout">
      <aside class="desktop-tree" aria-label="文档目录">
        <p class="directory-title">文档目录</p>
        <KnowledgeTitleSearch
          :search="titleSearch"
          :selected="state.selected"
          @select="selectSearchResult"
        />
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
        <div class="document-reading-column">
          <UiState
            v-if="!state.selected"
            kind="empty"
            title="选择一篇文档"
            description="从文档树选择父文档或子文档，查看当前内容。"
          />
          <template v-else>
            <header class="document-heading">
              <h2 ref="documentHeading" tabindex="-1">{{ state.document?.title ?? '文档' }}</h2>
              <UiBadge
                v-if="state.document"
                :tone="state.document.indexing_status === 'failed' ? 'warning' : 'neutral'"
                >{{ indexLabels[state.document.indexing_status] }}</UiBadge
              >
            </header>
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
              <UiBreadcrumb v-if="state.ancestors.phase === 'ready'" :items="crumbs" />
              <p v-else-if="state.ancestors.phase === 'loading'" role="status">正在读取文档位置</p>
              <div v-else-if="state.ancestors.phase !== 'idle'" class="path-error">
                <p role="status">{{ state.ancestors.message }}</p>
                <UiButton :disabled="owner.blocked.value" @click="owner.retryAncestors()"
                  >重新读取路径</UiButton
                >
              </div>
              <div class="document-toolbar" aria-label="文档操作">
                <div class="knowledge-actions">
                  <UiButton
                    v-if="state.document.source_kind === 'text'"
                    :disabled="!textEditor.canOpen.value"
                    @click="editText()"
                    >编辑正文</UiButton
                  >
                  <UiButton
                    :disabled="!textEditor.canOpen.value"
                    @click="createText(state.document)"
                    >新建子文档</UiButton
                  >
                  <UiButton
                    :disabled="!rename.canOpen.value || textEditor.pending.value"
                    @click="renameText()"
                    >改名</UiButton
                  >
                  <UiButton
                    :disabled="!move.canOpen.value || textEditor.pending.value"
                    @click="moveDocument()"
                    >移动</UiButton
                  >
                </div>
                <UiButton
                  variant="ghost"
                  :disabled="!deletion.canOpen.value || textEditor.pending.value"
                  @click="deleteTree()"
                  >删除子树</UiButton
                >
              </div>
              <details class="document-information">
                <summary>文档信息</summary>
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
                    <dt>内容版本</dt>
                    <dd>{{ state.document.content_version }}</dd>
                  </div>
                  <div>
                    <dt>创建者标识</dt>
                    <dd>{{ creator }}</dd>
                  </div>
                  <div>
                    <dt>创建时间</dt>
                    <dd>
                      <time :datetime="state.document.created_at">{{
                        state.document.created_at
                      }}</time>
                    </dd>
                  </div>
                  <div>
                    <dt>更新时间</dt>
                    <dd>
                      <time :datetime="state.document.updated_at">{{
                        state.document.updated_at
                      }}</time>
                    </dd>
                  </div>
                </dl>
                <RouterLink :to="`${workspace.paths.value.home}/knowledge/${state.document.id}`"
                  >文档链接</RouterLink
                >
              </details>
              <div class="document-content">
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
                    <pre
                      v-if="body.text"
                      class="canonical-text"
                      tabindex="0"
                      aria-label="当前文档正文"
                      >{{ body.text }}</pre>
                    <UiState v-else kind="empty" title="正文为空" />
                  </template>
                </template>
              </div>
              <footer class="document-footer">
                <p v-if="body && (body.truncated || state.content.offset !== '0')" class="meta">
                  当前正文段：字节 {{ state.content.offset }}–{{ body.next_byte_offset
                  }}<span v-if="body.truncated">，后续内容尚未读取</span>
                </p>
                <div class="content-navigation">
                  <div v-if="body" class="knowledge-actions">
                    <UiButton :disabled="!owner.canPrevious.value" @click="owner.previous()"
                      >上一段</UiButton
                    >
                    <UiButton :disabled="!owner.canNext.value" @click="owner.next()"
                      >下一段</UiButton
                    >
                  </div>
                  <UiButton :disabled="owner.blocked.value" @click="owner.retryDocument()"
                    >从开头重读</UiButton
                  >
                </div>
              </footer>
            </template>
          </template>
        </div>
      </div>
    </div>
    <KnowledgeMoveDialog :editor="move" :fallback-focus="documentHeading ?? heading" />
    <KnowledgeDeleteDialog :deletion="deletion" :fallback-focus="documentHeading ?? heading" />
    <KnowledgeRenameDialog :editor="rename" :fallback-focus="documentHeading ?? heading" />
    <UiDrawer v-model:open="drawer" title="文档树">
      <div class="drawer-directory">
        <KnowledgeTitleSearch
          v-if="owner.visible.value"
          :search="titleSearch"
          :selected="state.selected"
          @select="selectSearchResult"
        />
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
      </div>
    </UiDrawer>
  </section>
</template>

<style scoped>
.knowledge-page {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  padding: var(--space);
}
.knowledge-heading,
.document-heading,
.document-toolbar,
.content-navigation,
.knowledge-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.knowledge-heading,
.document-toolbar,
.content-navigation {
  justify-content: space-between;
}
.knowledge-heading {
  flex: none;
  gap: 16px;
  margin-bottom: var(--section-gap);
}
.knowledge-heading-copy {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.knowledge-notice {
  margin-bottom: var(--content-gap);
}
.knowledge-layout {
  display: grid;
  flex: 1;
  grid-template-columns: 280px minmax(0, 1fr);
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--canvas);
}
.desktop-tree {
  display: flex;
  flex-direction: column;
  gap: var(--section-gap);
  min-width: 0;
  min-height: 0;
  padding: 24px 16px;
  overflow: auto;
  border-right: 1px solid var(--border);
  background: var(--surface);
  scrollbar-gutter: stable;
}
.directory-title {
  font-size: 15px;
  font-weight: 600;
}
.knowledge-document {
  min-width: 0;
  min-height: 0;
  overflow: auto;
  padding: var(--space);
  scrollbar-gutter: stable;
}
.document-reading-column {
  display: grid;
  align-content: start;
  gap: var(--content-gap);
  width: 100%;
  max-width: 800px;
  min-width: 0;
  margin-inline: auto;
}
.document-heading h2 {
  flex: 1;
  min-width: 0;
  font-size: 20px;
}
.document-heading h2,
.document-facts dd {
  overflow-wrap: anywhere;
}
.document-toolbar {
  gap: 12px;
  padding-bottom: var(--content-gap);
  border-bottom: 1px solid var(--border);
}
.document-information {
  color: var(--muted);
  font-size: 12px;
}
.document-information summary {
  width: fit-content;
  cursor: pointer;
}
.document-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--content-gap);
  margin: var(--content-gap) 0;
}
.document-facts dd {
  margin: 4px 0 0;
  color: var(--text);
}
.document-content {
  min-width: 0;
  padding-block: var(--content-gap);
}
.canonical-text {
  max-width: 100%;
  min-height: 120px;
  margin: 0;
  font: inherit;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  tab-size: 4;
}
.document-footer {
  display: grid;
  gap: var(--content-gap);
  padding-top: var(--content-gap);
  border-top: 1px solid var(--border);
}
.drawer-directory {
  display: grid;
  gap: var(--section-gap);
  min-width: 0;
}
.drawer-trigger {
  display: none;
}
@media (max-width: 834px) {
  .knowledge-page {
    padding: 16px;
  }
  .knowledge-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .desktop-tree {
    display: none;
  }
  .drawer-trigger {
    display: inline-flex;
  }
  .knowledge-document {
    padding: 16px;
  }
  .document-facts {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
