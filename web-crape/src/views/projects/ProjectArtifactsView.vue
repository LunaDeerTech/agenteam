<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { UiButton, UiField, UiInput, UiSelect, UiState } from '../../components/ui'
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
  <section
    class="artifact-page page-workspace"
    :class="{ 'has-detail': selected }"
    aria-label="项目产物"
  >
    <header class="artifact-header">
      <div class="artifact-heading-copy">
        <h1 ref="heading" tabindex="-1">产物</h1>
        <p class="meta">浏览当前项目的产物与内容。</p>
      </div>
      <div class="artifact-actions">
        <UiButton v-if="owner.reading.value" @click="cancel">停止读取</UiButton>
        <UiButton :disabled="owner.blocked.value" @click="owner.refresh()">刷新产物</UiButton>
        <UiButton
          variant="primary"
          :disabled="!creation.canOpen.value || owner.blocked.value"
          @click="creation.openCreate()"
          >新建文本产物</UiButton
        >
      </div>
    </header>
    <p v-if="creation.state.denied" role="alert">{{ creation.state.message }}</p>
    <template v-if="owner.visible.value">
      <div class="artifact-filter-region">
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
            <UiButton type="submit" :disabled="owner.blocked.value">应用筛选</UiButton>
            <UiButton :disabled="owner.blocked.value" @click="apply(true)">清除筛选</UiButton>
          </div>
        </form>
        <p
          v-if="owner.applied.value.name_query || owner.applied.value.kind"
          class="meta applied-filters"
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
      </div>
      <div class="artifact-workspace">
        <div class="artifact-catalogue" aria-label="产物目录">
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
            :title="
              owner.applied.value.name_query || owner.applied.value.kind
                ? '暂无匹配的产物'
                : '暂无产物'
            "
            :description="
              owner.applied.value.name_query || owner.applied.value.kind
                ? '可清除筛选或稍后刷新。'
                : '可以新建文本产物，或稍后刷新。'
            "
          />
          <ul v-if="page.items.length" class="artifact-list" aria-label="产物列表">
            <li
              v-for="item in page.items"
              :key="item.artifact_ref.artifact_id"
              class="artifact-item"
              :class="{
                selected: selected?.artifact_ref.artifact_id === item.artifact_ref.artifact_id,
              }"
            >
              <div class="artifact-item-name">
                <h2>{{ item.name }}</h2>
                <p class="meta">{{ item.kind === 'generated' ? '生成产物' : '用户上传' }}</p>
              </div>
              <p class="meta artifact-file-summary">
                {{ item.object.media_type }}<span>{{ item.object.byte_size }} 字节</span>
              </p>
              <p class="meta artifact-created">
                <time :datetime="item.created_at">{{ item.created_at }}</time>
              </p>
              <UiButton
                :disabled="owner.blocked.value"
                :aria-label="`查看产物 ${item.name}`"
                :aria-current="
                  selected?.artifact_ref.artifact_id === item.artifact_ref.artifact_id
                    ? 'true'
                    : undefined
                "
                @click="select(item.artifact_ref.artifact_id, $event)"
                >查看产物</UiButton
              >
            </li>
          </ul>
          <div v-if="page.nextCursor" class="artifact-pagination">
            <UiButton :disabled="owner.blocked.value" @click="owner.more()">更多产物</UiButton>
          </div>
        </div>
        <section v-if="selected" class="artifact-detail" aria-labelledby="artifact-detail-heading">
          <header class="artifact-detail-header">
            <h2 id="artifact-detail-heading" ref="detailHeading" tabindex="-1">产物详情</h2>
            <UiButton @click="close">返回产物列表</UiButton>
          </header>
          <div class="artifact-detail-body">
            <div class="artifact-reading-column">
              <div class="artifact-title">
                <h3>{{ selected.name }}</h3>
                <p class="meta">
                  {{ selected.kind === 'generated' ? '生成产物' : '用户上传' }} ·
                  {{ selected.object.media_type }} · {{ selected.object.byte_size }} 字节
                </p>
              </div>
              <p v-if="selected.description" class="artifact-description">
                {{ selected.description }}
              </p>
              <details class="artifact-information">
                <summary>文件信息与校验</summary>
                <dl>
                  <div>
                    <dt>创建时间</dt>
                    <dd>
                      <time :datetime="selected.created_at">{{ selected.created_at }}</time>
                    </dd>
                  </div>
                  <div>
                    <dt>SHA-256</dt>
                    <dd class="artifact-checksum">{{ selected.object.sha256 }}</dd>
                  </div>
                </dl>
              </details>
              <div class="artifact-content">
                <UiState
                  v-if="detail.phase === 'loading'"
                  kind="loading"
                  title="正在读取产物内容"
                />
                <UiState
                  v-else-if="detail.phase === 'error'"
                  kind="error"
                  :title="detail.message"
                />
                <template v-else-if="detail.content">
                  <template v-if="detail.content.projection === 'text'">
                    <pre v-if="detail.content.text" tabindex="0" aria-label="产物文本内容">{{
                      detail.content.text
                    }}</pre>
                    <UiState v-else kind="empty" title="正文为空" />
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
              </div>
              <footer class="artifact-content-footer">
                <p
                  v-if="
                    detail.content?.projection === 'text' &&
                    (detail.content.truncated || detail.content.offset !== '0')
                  "
                  class="meta"
                >
                  内容范围 {{ detail.content.offset }}–{{ detail.content.next_offset }} 字节<span
                    v-if="detail.content.truncated"
                  >
                    · 后续还有内容</span
                  >
                </p>
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
                  <UiButton :disabled="owner.blocked.value" @click="owner.reread()"
                    >重新读取</UiButton
                  >
                </div>
              </footer>
            </div>
          </div>
        </section>
      </div>
    </template>
    <UiState v-else kind="loading" title="正在确认项目访问权限" />
  </section>
</template>

<style scoped>
.artifact-page {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: var(--section-gap);
  width: 100%;
  max-width: 1120px;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  margin-inline: auto;
  padding: var(--space);
}
.artifact-page.has-detail {
  max-width: none;
}
.artifact-header,
.artifact-detail-header,
.artifact-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.artifact-header,
.artifact-detail-header {
  flex: none;
  justify-content: space-between;
  gap: 16px;
}
.artifact-heading-copy,
.artifact-title {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.artifact-filter-region {
  flex: none;
}
.artifact-filters {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(160px, 1fr) auto;
  gap: 16px;
  align-items: start;
}
.artifact-filters .artifact-actions {
  padding-top: 27px;
}
.applied-filters {
  margin-top: var(--content-gap);
}
.artifact-workspace {
  display: grid;
  flex: 1;
  grid-template-columns: minmax(0, 1fr);
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}
.has-detail .artifact-workspace {
  grid-template-columns: 280px minmax(0, 1fr);
}
.artifact-catalogue {
  min-width: 0;
  min-height: 0;
  overflow: auto;
  scrollbar-gutter: stable;
}
.artifact-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.artifact-item {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1fr) auto;
  align-items: center;
  gap: 16px;
  min-height: 64px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
  border-inline-start: 2px solid transparent;
}
.artifact-item.selected {
  background: var(--accent-soft);
  border-inline-start-color: var(--accent);
}
.artifact-item h2 {
  font-size: 14px;
  font-weight: 600;
}
.artifact-item-name,
.artifact-file-summary {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.artifact-item > .ui-button {
  justify-self: start;
}
.has-detail .artifact-item {
  grid-template-columns: minmax(0, 1fr);
  gap: 8px;
}
.has-detail .artifact-created {
  display: none;
}
.artifact-pagination {
  padding: 16px;
}
.artifact-detail {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  border-left: 1px solid var(--border);
  background: var(--canvas);
}
.artifact-detail-header {
  padding: 16px 24px;
  border-bottom: 1px solid var(--border);
}
.artifact-detail-body {
  min-width: 0;
  min-height: 0;
  overflow: auto;
  padding: var(--space);
  scrollbar-gutter: stable;
}
.artifact-reading-column {
  display: grid;
  align-content: start;
  gap: var(--content-gap);
  width: 100%;
  max-width: 800px;
  min-width: 0;
  margin-inline: auto;
}
.artifact-title h3 {
  font-size: 20px;
}
.artifact-item,
.artifact-page p,
.artifact-page h2,
.artifact-page h3 {
  min-width: 0;
  overflow-wrap: anywhere;
}
.artifact-description,
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.artifact-information {
  color: var(--muted);
  font-size: 12px;
}
.artifact-information summary {
  width: fit-content;
  cursor: pointer;
}
dl {
  display: grid;
  gap: var(--content-gap);
  margin: var(--content-gap) 0 0;
}
dl > div {
  display: grid;
  grid-template-columns: 80px minmax(0, 1fr);
  gap: var(--content-gap);
}
dd {
  margin: 0;
  color: var(--text);
  overflow-wrap: anywhere;
}
.artifact-checksum {
  font-family: var(--font-mono);
}
.artifact-content {
  min-width: 0;
  padding-block: var(--content-gap);
}
pre {
  max-width: 100%;
  margin: 0;
  font: inherit;
  tab-size: 4;
}
.artifact-content-footer {
  display: grid;
  gap: var(--content-gap);
  padding-top: var(--content-gap);
  border-top: 1px solid var(--border);
}
@media (max-width: 834px) {
  .artifact-page {
    padding: 16px;
  }
  .artifact-filters {
    grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  }
  .artifact-filters .artifact-actions {
    grid-column: 1 / -1;
    padding-top: 0;
  }
  .has-detail .artifact-workspace {
    grid-template-columns: minmax(0, 1fr);
  }
  .has-detail .artifact-catalogue,
  .has-detail .artifact-filter-region {
    display: none;
  }
  .artifact-detail {
    border-left: 0;
  }
  .artifact-detail-header,
  .artifact-detail-body {
    padding: 16px;
  }
  .artifact-item {
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 8px 16px;
  }
  .artifact-file-summary {
    grid-column: 1;
  }
  .artifact-created {
    grid-column: 1;
  }
  .artifact-item > .ui-button {
    grid-column: 2;
    grid-row: 1 / 4;
  }
}
@media (max-width: 600px) {
  .artifact-filters {
    grid-template-columns: minmax(0, 1fr);
  }
  dl > div {
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
  }
}
</style>
