<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { UiButton, UiState } from '../../components/ui'
import ProjectVariableEditor from './ProjectVariableEditor.vue'
import { useProjectVariables } from '../../composables/useProjectVariables'
const owner = useProjectVariables(),
  detailHeading = ref<HTMLElement | null>(null),
  createButton = ref<{ $el: HTMLButtonElement } | null>(null),
  editButton = ref<{ $el: HTMLButtonElement } | null>(null),
  deleteButton = ref<{ $el: HTMLButtonElement } | null>(null)
watch(
  () => [owner.detail.phase, owner.detail.variable?.id, owner.detail.variable?.version] as const,
  async () => {
    await nextTick()
    if (owner.visible.value && owner.detail.phase === 'ready' && !owner.editor.open)
      detailHeading.value?.focus()
  },
)
async function cancel() {
  const create = owner.editor.mode === 'create'
  if (await owner.cancelEdit()) {
    await nextTick()
    if (owner.visible.value) (create ? createButton : editButton).value?.$el.focus()
  }
}
async function confirmDelete() {
  const original = owner.detail.variable,
    trigger = deleteButton.value?.$el
  await owner.deleteVariable()
  // Closing the dialog restores focus before Vue re-enables this sibling button.
  await nextTick()
  if (
    owner.visible.value &&
    owner.canDelete.value &&
    owner.detail.variable === original &&
    trigger === deleteButton.value?.$el &&
    trigger?.isConnected &&
    !trigger.disabled &&
    !trigger.closest('[inert]')
  )
    trigger.focus({ preventScroll: true })
}
watch(
  () => owner.message.value,
  async (message) => {
    if (message === '已确认删除，并重新读取当前目录。') {
      await nextTick()
      if (owner.visible.value) createButton.value?.$el.focus()
    }
  },
)
</script>
<template>
  <section class="variables-page" aria-label="项目普通变量">
    <header class="variables-header">
      <h1>普通变量</h1>
      <div class="variable-actions">
        <UiButton
          :disabled="!owner.visible.value || owner.busy.value || owner.pending.value"
          @click="owner.refresh()"
          >重新读取</UiButton
        ><UiButton
          ref="createButton"
          :disabled="
            !owner.canMutate.value ||
            owner.busy.value ||
            owner.pending.value ||
            owner.needsRead.value
          "
          @click="owner.openCreate()"
          >新建变量</UiButton
        >
      </div>
    </header>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认项目访问身份" />
    <template v-else>
      <p v-if="owner.message.value" role="status">{{ owner.message.value }}</p>
      <section v-if="owner.pending.value" aria-label="原变量操作">
        <h2>操作结果尚未确认</h2>
        <p>仅查询原请求；不会再次发送创建、修改或删除。</p>
        <UiButton
          :disabled="
            owner.busy.value ||
            !owner.progress.value?.contextValid ||
            owner.progress.value.phase !== 'uncertain'
          "
          @click="owner.lookupOriginal()"
          >查询原操作</UiButton
        >
      </section>
      <UiButton
        v-if="owner.needsRead.value"
        :disabled="owner.busy.value"
        @click="owner.rereadCommitted()"
        >重新读取已确认结果</UiButton
      >
      <ProjectVariableEditor
        v-if="owner.editor.open && !owner.pending.value"
        :owner="owner"
        @cancel="cancel"
        @submit="owner.submit()"
      />
      <section aria-label="变量目录" :aria-busy="owner.page.phase === 'loading'">
        <h2>变量目录</h2>
        <UiState v-if="owner.page.phase === 'loading'" kind="loading" title="正在读取目录" />
        <UiState
          v-else-if="owner.page.phase === 'error'"
          kind="error"
          title="目录读取未完成"
          :description="owner.page.message"
        />
        <template v-else-if="owner.page.phase === 'ready'">
          <p v-if="!owner.page.items.length">项目暂无普通变量。</p>
          <ul class="variable-list">
            <li v-for="item in owner.page.items" :key="item.id">
              <UiButton
                variant="ghost"
                :disabled="owner.busy.value || owner.pending.value || owner.needsRead.value"
                @click="owner.select(item.id)"
                >{{ item.name }}</UiButton
              >
              <p>{{ item.description || '无描述' }}</p>
              <span>版本 {{ item.version }}</span>
            </li>
          </ul>
          <UiButton
            v-if="owner.page.nextCursor"
            :disabled="owner.busy.value || owner.pending.value || owner.page.stale"
            @click="owner.nextPage()"
            >下一页变量</UiButton
          >
        </template>
      </section>
      <section
        v-if="owner.detail.phase !== 'inactive'"
        class="variable-detail"
        aria-label="变量详情"
        :aria-busy="owner.detail.phase === 'loading'"
      >
        <UiState v-if="owner.detail.phase === 'loading'" kind="loading" title="正在读取变量" />
        <UiState
          v-else-if="owner.detail.phase === 'error'"
          kind="error"
          title="变量读取未完成"
          :description="owner.detail.message"
        />
        <template v-else-if="owner.detail.variable">
          <h2 ref="detailHeading" tabindex="-1">{{ owner.detail.variable.name }}</h2>
          <dl>
            <div>
              <dt>变量 ID</dt>
              <dd>{{ owner.detail.variable.id }}</dd>
            </div>
            <div>
              <dt>版本</dt>
              <dd>{{ owner.detail.variable.version }}</dd>
            </div>
            <div>
              <dt>描述</dt>
              <dd class="variable-description">
                {{ owner.detail.variable.description || '无描述' }}
              </dd>
            </div>
            <div>
              <dt>值</dt>
              <dd class="variable-value">
                {{ owner.detail.variable.value === '' ? '（空值）' : owner.detail.variable.value }}
              </dd>
            </div>
          </dl>
          <UiButton
            ref="editButton"
            :disabled="
              !owner.canMutate.value ||
              owner.busy.value ||
              owner.pending.value ||
              owner.needsRead.value
            "
            @click="owner.openEdit()"
            >编辑变量</UiButton
          >
          <UiButton
            ref="deleteButton"
            variant="danger"
            :disabled="!owner.canDelete.value"
            @click="confirmDelete"
            >删除变量</UiButton
          >
        </template>
      </section>
    </template>
  </section>
</template>
<style scoped>
.variables-page {
  display: flex;
  flex-direction: column;
  gap: var(--section-gap);
  min-width: 0;
}
.variables-header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space);
}
.variable-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.variables-page h1,
.variables-page h2,
.variables-page p {
  margin: 0;
  overflow-wrap: anywhere;
}
.variables-page section {
  min-width: 0;
}
.variable-list {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  list-style: none;
  margin: var(--space) 0;
  padding: 0;
}
.variable-list li {
  min-width: 0;
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.variable-list span,
dt {
  color: var(--muted);
}
.variable-detail {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space);
}
.variable-detail dl {
  width: 100%;
  min-width: 0;
  margin: 0;
  display: grid;
  gap: var(--space);
}
.variable-detail dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.variable-value,
.variable-description {
  white-space: pre-wrap;
}
</style>
