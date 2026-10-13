<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { UiButton, UiCard, UiField, UiInput, UiSelect, UiState } from '../../components/ui'
import { useArtifactCreation } from '../../composables/useArtifactCreation'
import { useProjectArtifacts } from '../../composables/useProjectArtifacts'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { skillsNavigationReadReady } from '../../router/auth'

const route = useRoute(),
  navigationReady = skillsNavigationReadReady(useRouter())
const location = computed(() => (navigationReady.value ? route.fullPath : ''))
const owner = useProjectArtifacts(undefined, useProjectWorkspace(), location)
const creation = useArtifactCreation()
const page = owner.page,
  detail = owner.detail,
  selected = owner.selected
const heading = ref<HTMLElement | null>(null),
  detailHeading = ref<HTMLElement | null>(null)
const detachReader = creation.attachReader(owner, () => heading.value)
const kinds = [
  { value: '', label: '全部' },
  { value: 'generated', label: '生成产物' },
  { value: 'user_upload', label: '用户上传' },
]
let trigger: HTMLElement | null = null
async function select(id: string, event: MouseEvent) {
  const current = event.currentTarget as HTMLElement
  if (!owner.select(id)) return
  trigger = current
  await nextTick()
  if (owner.visible.value && selected.value?.artifact_ref.artifact_id === id && trigger === current)
    detailHeading.value?.focus()
}
async function close() {
  const current = trigger
  trigger = null
  owner.closeDetail()
  await nextTick()
  if (!owner.visible.value) return
  if (current?.isConnected && !current.matches(':disabled')) current.focus()
  else heading.value?.focus()
}
async function apply(clear = false) {
  if (!(clear ? owner.clearFilter() : owner.apply())) return
  trigger = null
  await nextTick()
  if (owner.visible.value) heading.value?.focus()
}
function cancel() {
  trigger = null
  owner.cancel()
}
onBeforeRouteLeave(cancel)
onBeforeRouteUpdate(cancel)
onBeforeUnmount(() => {
  trigger = null
  detachReader()
  owner.dispose()
})
</script>

<template>
  <section class="artifact-page" aria-label="项目产物">
    <header class="artifact-header">
      <div>
        <h1 ref="heading" tabindex="-1">产物</h1>
        <p class="meta">浏览当前项目的产物与内容。</p>
      </div>
      <div class="artifact-actions">
        <UiButton variant="primary" :disabled="!creation.canOpen.value || owner.blocked.value" @click="creation.openCreate()">新建文本产物</UiButton>
        <UiButton :disabled="owner.blocked.value" @click="owner.refresh()">刷新产物</UiButton>
      </div>
    </header>
    <p v-if="creation.state.denied" role="alert">{{ creation.state.message }}</p>
    <template v-if="owner.visible.value">
      <UiCard>
        <form class="artifact-filters" aria-label="筛选产物" @submit.prevent="apply()">
          <UiField v-slot="field" label="产物名称" hint="按名称包含的文字筛选，区分大小写。">
            <UiInput
              :id="field.id"
              v-model="owner.draft.name"
              :aria-describedby="field.describedby"
              :disabled="owner.blocked.value"
            />
          </UiField>
          <UiField v-slot="field" label="产物类型">
            <UiSelect
              :id="field.id"
              v-model="owner.draft.kind"
              label="产物类型"
              :options="kinds"
              :disabled="owner.blocked.value"
            />
          </UiField>
          <div class="artifact-actions">
            <UiButton type="submit" variant="primary" :disabled="owner.blocked.value"
              >应用筛选</UiButton
            >
            <UiButton :disabled="owner.blocked.value" @click="apply(true)">清除筛选</UiButton>
          </div>
        </form>
        <p
          v-if="owner.applied.value.name_query || owner.applied.value.kind"
          class="meta"
          aria-live="polite"
        >
          当前筛选：{{ owner.applied.value.name_query || '全部名称' }} ·
          {{
            owner.applied.value.kind === 'generated'
              ? '生成产物'
              : owner.applied.value.kind === 'user_upload'
                ? '用户上传'
                : '全部类型'
          }}
        </p>
      </UiCard>
      <UiState v-if="page.phase === 'loading'" kind="loading" title="正在读取产物" />
      <UiState
        v-else-if="page.phase === 'error' || page.phase === 'unavailable'"
        kind="error"
        :title="page.message"
      />
      <p v-else-if="page.message" role="alert">{{ page.message }}</p>
      <UiState
        v-if="page.phase === 'ready' && page.items.length === 0"
        kind="empty"
        title="暂无匹配的产物"
        description="可清除筛选或稍后刷新。"
      />
      <div v-if="page.items.length" class="artifact-list" aria-label="产物列表">
        <UiCard
          v-for="item in page.items"
          :key="item.artifact_ref.artifact_id"
          class="artifact-item"
        >
          <h2>{{ item.name }}</h2>
          <p class="meta">
            {{ item.kind === 'generated' ? '生成产物' : '用户上传' }} ·
            {{ item.object.media_type }} · {{ item.object.byte_size }} 字节
          </p>
          <p class="meta">
            <time :datetime="item.created_at">{{ item.created_at }}</time>
          </p>
          <UiButton
            :disabled="owner.blocked.value"
            :aria-label="`查看产物 ${item.name}`"
            @click="select(item.artifact_ref.artifact_id, $event)"
            >查看产物</UiButton
          >
        </UiCard>
      </div>
      <UiButton v-if="page.nextCursor" :disabled="owner.blocked.value" @click="owner.more()"
        >更多产物</UiButton
      >
      <UiCard v-if="selected" class="artifact-detail" aria-labelledby="artifact-detail-heading">
        <header class="artifact-header">
          <h2 id="artifact-detail-heading" ref="detailHeading" tabindex="-1">产物详情</h2>
          <UiButton @click="close">返回产物列表</UiButton>
        </header>
        <h3>{{ selected.name }}</h3>
        <p v-if="selected.description" class="artifact-description">{{ selected.description }}</p>
        <dl>
          <div>
            <dt>类型</dt>
            <dd>{{ selected.kind === 'generated' ? '生成产物' : '用户上传' }}</dd>
          </div>
          <div>
            <dt>文件格式</dt>
            <dd>{{ selected.object.media_type }}</dd>
          </div>
          <div>
            <dt>大小</dt>
            <dd>{{ selected.object.byte_size }} 字节</dd>
          </div>
          <div>
            <dt>创建时间</dt>
            <dd>{{ selected.created_at }}</dd>
          </div>
          <div>
            <dt>SHA-256</dt>
            <dd>{{ selected.object.sha256 }}</dd>
          </div>
        </dl>
        <UiState v-if="detail.phase === 'loading'" kind="loading" title="正在读取产物内容" />
        <UiState v-else-if="detail.phase === 'error'" kind="error" :title="detail.message" />
        <template v-else-if="detail.content">
          <template v-if="detail.content.projection === 'text'">
            <p class="meta">
              内容范围 {{ detail.content.offset }}–{{ detail.content.next_offset }} 字节<span
                v-if="detail.content.truncated"
              >
                · 后续还有内容</span
              >
            </p>
            <pre tabindex="0" aria-label="产物文本内容">{{ detail.content.text }}</pre>
            <p v-if="detail.message" role="status">{{ detail.message }}</p>
          </template>
          <UiState
            v-else
            kind="empty"
            title="此类型暂不支持在线预览。"
            :description="
              detail.content.invalid_utf8
                ? '文件内容不是有效的 UTF-8 文本，已保留文件信息。'
                : '可以查看此产物的文件信息。'
            "
          />
        </template>
        <div class="artifact-actions">
          <UiButton
            v-if="detail.content?.projection === 'text'"
            :disabled="owner.blocked.value || detail.index === 0"
            @click="owner.previous()"
            >上一段</UiButton
          >
          <UiButton
            v-if="detail.content?.projection === 'text'"
            :disabled="!owner.canNext.value"
            @click="owner.next()"
            >下一段</UiButton
          >
          <UiButton :disabled="owner.blocked.value" @click="owner.reread()">重新读取</UiButton>
        </div>
      </UiCard>
      <UiButton v-if="owner.reading.value" @click="cancel">停止读取</UiButton>
    </template>
    <UiState v-else kind="loading" title="正在确认项目访问权限" />
  </section>
</template>

<style scoped>
.artifact-page {
  display: grid;
  gap: var(--space);
  min-width: 0;
}
.artifact-header,
.artifact-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.artifact-header > div {
  min-width: 0;
}
.artifact-header h1,
.artifact-header h2 {
  margin: 0;
}
.artifact-filters {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.artifact-filters .artifact-actions {
  grid-column: 1 / -1;
  justify-content: flex-start;
}
.artifact-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 1fr));
  gap: 12px;
}
.artifact-item,
.artifact-detail,
.artifact-page p,
.artifact-page h2,
.artifact-page h3 {
  min-width: 0;
  overflow-wrap: anywhere;
}
.artifact-item h2 {
  font-size: 1rem;
  margin-top: 0;
}
.artifact-description,
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
dl {
  display: grid;
  gap: 10px;
}
dl > div {
  display: grid;
  grid-template-columns: 100px minmax(0, 1fr);
  gap: 12px;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
pre {
  max-height: 360px;
  overflow: auto;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface-alt);
  color: var(--text);
}
.artifact-detail .artifact-actions {
  justify-content: flex-start;
}
@media (max-width: 600px) {
  .artifact-filters {
    grid-template-columns: minmax(0, 1fr);
  }
  dl > div {
    grid-template-columns: minmax(0, 1fr);
    gap: 3px;
  }
}
</style>
