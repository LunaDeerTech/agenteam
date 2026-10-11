<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { UiButton, UiCard, UiState } from '../../components/ui'
import ProjectSecretEditor from '../../components/projects/ProjectSecretEditor.vue'
import { useProjectSecretEditor } from '../../composables/useProjectSecretEditor'
import { useProjectSecrets } from '../../composables/useProjectSecrets'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { skillsNavigationReadReady } from '../../router/auth'

const route = useRoute(),
  navigationReady = skillsNavigationReadReady(useRouter())
const location = computed(() => (navigationReady.value ? route.fullPath : ''))
const owner = useProjectSecrets(undefined, useProjectWorkspace(), location)
const editor = useProjectSecretEditor(owner)
const page = owner.page,
  detail = owner.detail
const detailHeading = ref<HTMLElement | null>(null)
let selectionTrigger: HTMLElement | null = null
let editorTrigger: HTMLElement | null = null
function openEditor(mode: 'create' | 'metadata' | 'replace', event: MouseEvent) {
  editorTrigger = event.currentTarget as HTMLElement
  editor.open(mode)
}
async function closeEditor() {
  await nextTick()
  if (editorTrigger?.isConnected) editorTrigger.focus()
  editorTrigger = null
}
async function select(id: string, event: MouseEvent) {
  if (owner.blocked.value) return
  const trigger = event.currentTarget as HTMLElement
  editor.close()
  selectionTrigger = trigger
  owner.select(id)
  await nextTick()
  if (owner.visible.value && owner.selected.value === id && selectionTrigger === trigger)
    detailHeading.value?.focus()
}
async function closeDetail() {
  editor.close()
  owner.closeDetail()
  await nextTick()
  if (selectionTrigger?.isConnected) selectionTrigger.focus()
  selectionTrigger = null
}
onBeforeRouteLeave(() => {
  editor.close()
  owner.cancel()
})
onBeforeRouteUpdate((to, from) => {
  if (to.fullPath !== from.fullPath) {
    editor.close()
    owner.cancel()
  }
})
onBeforeUnmount(() => {
  editor.dispose()
  owner.dispose()
})
</script>

<template>
  <section class="secrets-page" aria-label="项目 Secrets">
    <header class="secrets-header">
      <div>
        <h1>Secrets</h1>
        <p>管理名称、描述与 Secret 材料。已有值不会返回；输入的材料仅用于本次提交。</p>
      </div>
      <div class="secrets-actions">
        <UiButton :disabled="!editor.canOpen.value" @click="openEditor('create', $event)"
          >创建 Secret</UiButton
        >
        <UiButton :disabled="owner.blocked.value" @click="owner.refresh()">重新读取目录</UiButton>
        <UiButton v-if="owner.reading.value" variant="ghost" @click="owner.cancel()"
          >停止读取</UiButton
        >
      </div>
    </header>
    <ProjectSecretEditor :owner="editor" @close="closeEditor" />
    <p v-if="editor.visible.value && editor.state.message" role="status">
      {{ editor.state.message }}
    </p>
    <div
      v-if="editor.visible.value && editor.progress.value?.phase === 'uncertain'"
      class="secrets-recovery"
    >
      <p>原请求尚未确认。材料已清除；检查原请求不会再次提交材料。</p>
      <UiButton :disabled="!editor.canLookup.value" @click="editor.lookup()">检查原请求</UiButton>
    </div>
    <div v-if="editor.visible.value && editor.progress.value?.phase === 'confirmed'">
      <p>原请求已确认。请独立读取当前记录。</p>
      <UiButton :disabled="editor.blocked.value" @click="editor.readConfirmed()"
        >读取已确认的记录</UiButton
      >
    </div>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认 Secrets 访问身份" />
    <UiState
      v-else-if="page.phase === 'waiting' || page.phase === 'loading'"
      kind="loading"
      title="正在读取 Secrets 目录"
    />
    <UiState
      v-else-if="page.phase === 'error' || page.phase === 'unavailable'"
      kind="error"
      title="Secrets 目录暂不可用"
      :description="page.message"
    />
    <template v-else>
      <p v-if="page.items.length === 0">项目 Secrets 目录暂无条目。</p>
      <div class="secrets-content">
        <div>
          <ul class="secrets-list" aria-label="Secrets 目录" :aria-busy="page.loadingMore">
            <li v-for="secret in page.items" :key="secret.id">
              <UiButton
                class="secret-select"
                :aria-pressed="owner.selected.value === secret.id"
                :disabled="owner.blocked.value"
                @click="select(secret.id, $event)"
                >{{ secret.name }}</UiButton
              >
              <p class="secret-description">{{ secret.description || '暂无描述' }}</p>
            </li>
          </ul>
          <p v-if="page.message" role="alert">{{ page.message }}</p>
          <UiButton
            v-if="page.nextCursor"
            :disabled="owner.blocked.value"
            @click="owner.loadMore()"
            >{{ page.loadingMore ? '正在加载' : '加载更多 Secrets' }}</UiButton
          >
        </div>
        <UiCard v-if="owner.selected.value" class="secret-detail">
          <h2 ref="detailHeading" tabindex="-1">{{ detail.value?.name ?? 'Secret 详情' }}</h2>
          <div class="secrets-actions">
            <UiButton variant="ghost" :disabled="owner.blocked.value" @click="closeDetail"
              >返回目录</UiButton
            >
            <UiButton :disabled="owner.blocked.value" @click="owner.refreshDetail()"
              >重新读取详情</UiButton
            >
          </div>
          <UiState v-if="detail.phase === 'loading'" kind="loading" title="正在读取 Secret 详情" />
          <UiState
            v-else-if="detail.phase === 'error'"
            kind="error"
            title="Secret 详情读取未完成"
            :description="detail.message"
          />
          <template v-else-if="detail.value">
            <div class="secrets-actions">
              <UiButton :disabled="!editor.canOpen.value" @click="openEditor('metadata', $event)"
                >编辑名称与描述</UiButton
              >
              <UiButton :disabled="!editor.canOpen.value" @click="openEditor('replace', $event)"
                >替换 Secret 值</UiButton
              >
            </div>
            <p class="secret-description">{{ detail.value.description || '暂无描述' }}</p>
            <dl class="secret-facts">
              <div>
                <dt>记录版本</dt>
                <dd>{{ detail.value.version }}</dd>
              </div>
              <div>
                <dt>创建时间</dt>
                <dd>
                  <time :datetime="detail.value.created_at">{{ detail.value.created_at }}</time>
                </dd>
              </div>
              <div>
                <dt>更新时间</dt>
                <dd>
                  <time :datetime="detail.value.updated_at">{{ detail.value.updated_at }}</time>
                </dd>
              </div>
              <div>
                <dt>Secret ID</dt>
                <dd>{{ detail.value.id }}</dd>
              </div>
              <div>
                <dt>项目 ID</dt>
                <dd>{{ detail.value.project_id }}</dd>
              </div>
            </dl>
          </template>
        </UiCard>
      </div>
    </template>
  </section>
</template>

<style scoped>
.secrets-page {
  min-width: 0;
  display: grid;
  gap: var(--content-gap);
}
.secrets-header,
.secrets-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.secrets-header {
  justify-content: space-between;
}
.secrets-header p,
dt {
  color: var(--muted);
}
.secrets-content {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--content-gap);
}
.secrets-content > *,
.secret-detail {
  min-width: 0;
}
.secrets-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: var(--content-gap);
}
.secrets-list li {
  min-width: 0;
}
.secret-select {
  max-width: 100%;
  height: auto;
  overflow-wrap: anywhere;
  white-space: normal;
}
.secret-select[aria-pressed='true'] {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.secret-description {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.secret-facts {
  display: grid;
  gap: var(--content-gap);
}
.secret-facts dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.secret-detail h2 {
  overflow-wrap: anywhere;
}
@media (max-width: 834px) {
  .secrets-content {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
