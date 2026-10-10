<script setup lang="ts">
import { computed, onScopeDispose } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import { useProjectSecrets } from '../../composables/useProjectSecrets'
import { installProjectSecretsNavigation } from '../../router/auth'
import ProjectSecretEditor from './ProjectSecretEditor.vue'
const route = useRoute(),
  router = useRouter()
const page = useProjectSecrets(computed(() => route.fullPath))
const { visible, busy, progress, unresolved, canWrite } = page
const unregister = installProjectSecretsNavigation(router, { confirmLeave: page.confirmLeave })
onScopeDispose(unregister)
</script>
<template>
  <section class="project-secrets" aria-labelledby="project-secrets-title">
    <header class="page-heading">
      <h1 id="project-secrets-title">Secrets</h1>
      <p>管理本项目的 Secret 名称、描述和值。已有值不会返回或显示。</p>
    </header>
    <div v-if="!visible" class="notice" role="status">
      <p>当前项目身份尚未确认，Secret 信息已隐藏。</p>
      <UiButton @click="page.readOwner()">重新读取项目</UiButton>
    </div>
    <template v-else>
      <p v-if="!canWrite && !busy && !unresolved" role="status" class="notice">
        当前项目为只读状态，可以查看安全元数据。
      </p>
      <div class="actions">
        <UiButton variant="primary" :disabled="!canWrite" @click="page.open('create')"
          >创建 Secret</UiButton
        ><UiButton :disabled="busy || page.editor.open" @click="page.refresh()"
          >重新读取列表</UiButton
        >
      </div>
      <p v-if="busy" role="status" class="meta">正在等待当前请求完成…</p>
      <p v-if="page.state.message && !page.editor.open" class="notice" role="status">
        {{ page.state.message }}
      </p>
      <section v-if="progress" class="notice" aria-label="命令状态">
        <p v-if="progress.phase === 'confirmed'">命令已确认提交。当前信息以重新读取的结果为准。</p>
        <p v-else-if="progress.phase === 'uncertain'">
          提交结果尚未确认。Secret 值已清除，不会自动重发。
        </p>
        <p v-else-if="progress.phase === 'submitting'">正在提交，Secret 值已从输入框清除。</p>
        <p v-else>本次命令被明确拒绝，值输入已清除。</p>
        <p v-if="progress.observation === 'not_observed'">尚未观察到回执；这不证明回滚或未执行。</p>
        <p v-if="progress.keyConflict">原请求标识冲突，不能自动换标识或重发。</p>
        <UiButton
          v-if="progress.phase === 'uncertain'"
          :disabled="busy || progress.keyConflict"
          @click="page.lookup()"
          >查证原命令</UiButton
        >
        <UiButton v-if="unresolved" @click="page.confirmLeave().then(() => undefined)"
          >放弃本地追踪</UiButton
        >
      </section>
      <div class="secret-layout">
        <section aria-label="Secret 列表">
          <p v-if="page.list.phase === 'loading'" role="status">正在读取安全元数据…</p>
          <p v-if="page.list.message" class="notice" role="status">{{ page.list.message }}</p>
          <p v-if="page.list.phase === 'ready' && page.list.items.length === 0" class="meta">
            此项目尚无 Secret。
          </p>
          <ul class="secret-list">
            <li v-for="row in page.list.items" :key="row.id">
              <button
                type="button"
                :disabled="busy || page.editor.open"
                :aria-pressed="page.detail.value?.id === row.id"
                @click="page.select(row)"
              >
                <strong>{{ row.name }}</strong
                ><span>{{ row.description || '无描述' }}</span
                ><small>版本 {{ row.version }}</small>
              </button>
            </li>
          </ul>
          <UiButton
            v-if="page.list.cursor"
            :disabled="busy || page.editor.open"
            @click="page.more()"
            >加载更多</UiButton
          >
        </section>
        <section aria-label="Secret 详情" class="secret-detail">
          <p v-if="page.detail.phase === 'idle'" class="meta">选择一项查看安全元数据。</p>
          <p v-if="page.detail.phase === 'loading'" role="status">正在读取详情…</p>
          <p v-if="page.detail.message" class="notice" role="status">{{ page.detail.message }}</p>
          <template v-if="page.detail.phase === 'ready' && page.detail.value">
            <h2>{{ page.detail.value.name }}</h2>
            <p class="description">{{ page.detail.value.description || '无描述' }}</p>
            <dl>
              <dt>ID</dt>
              <dd>{{ page.detail.value.id }}</dd>
              <dt>版本</dt>
              <dd>{{ page.detail.value.version }}</dd>
              <dt>创建时间</dt>
              <dd>{{ page.detail.value.created_at }}</dd>
              <dt>更新时间</dt>
              <dd>{{ page.detail.value.updated_at }}</dd>
            </dl>
            <p class="meta">值不可读取。替换时需要重新输入完整值。</p>
            <div class="actions">
              <UiButton :disabled="!canWrite" @click="page.open('update')">编辑 Secret</UiButton
              ><UiButton variant="danger" :disabled="!canWrite" @click="page.open('delete')"
                >删除 Secret</UiButton
              >
            </div>
          </template>
        </section>
      </div>
    </template>
    <ProjectSecretEditor :page="page" />
    <UiDialog
      :open="page.state.leaveOpen"
      title="放弃本地编辑或追踪？"
      :close-on-outside="false"
      @update:open="!$event && page.resolveNavigation(false)"
    >
      <p>未提交的值会被清除。放弃本地追踪不会撤销可能已提交的命令，也不能证明回滚。</p>
      <template #footer
        ><UiButton @click="page.resolveNavigation(false)">继续留在此页</UiButton
        ><UiButton variant="danger" @click="page.resolveNavigation(true)"
          >确认放弃</UiButton
        ></template
      >
    </UiDialog>
  </section>
</template>
<style scoped>
.project-secrets {
  display: grid;
  gap: var(--space);
  min-width: 0;
}
.page-heading h1 {
  margin-top: 0;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.secret-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--space);
}
.secret-list {
  list-style: none;
  margin: 0 0 var(--space);
  padding: 0;
  display: grid;
  gap: calc(var(--space) / 4);
}
.secret-list button {
  width: 100%;
  text-align: start;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  color: var(--text);
  padding: var(--content-gap);
  display: grid;
  gap: calc(var(--space) / 4);
  cursor: pointer;
}
.secret-list button[aria-pressed='true'] {
  border-color: var(--accent);
}
.secret-list button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.secret-list button:disabled {
  opacity: 0.65;
  cursor: default;
}
.secret-detail {
  min-width: 0;
}
.secret-detail dl {
  display: grid;
  gap: calc(var(--space) / 4);
}
.secret-detail dd {
  margin: 0 0 var(--content-gap);
}
.secret-detail dd,
.secret-list button,
.notice,
.description {
  overflow-wrap: anywhere;
}
.description {
  white-space: pre-wrap;
}
@media (max-width: 48rem) {
  .secret-layout {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
