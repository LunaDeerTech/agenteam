<script setup lang="ts">
import { computed, ref } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiField from '../../components/ui/UiField.vue'
import UiSelect from '../../components/ui/UiSelect.vue'
import { useProjectModelSettings } from '../../composables/useProjectModelSettings'
import ProjectModelsPanel from './ProjectModelsPanel.vue'
import ProjectProviderEditor from './ProjectProviderEditor.vue'
import ProjectModelEditor from './ProjectModelEditor.vue'
import ProjectModelCredentialEditor from './ProjectModelCredentialEditor.vue'
import ProjectModelDeleteDialog from './ProjectModelDeleteDialog.vue'

const page = useProjectModelSettings()
const heading = ref<HTMLElement | null>(null)
const {
  visible,
  currentProject,
  canMutate,
  blocked,
  pending,
  providers,
  modelsOpen,
  message,
  progress,
  preparedCredential,
  canLookup,
  canReplay,
} = page
const limits = [25, 50, 100].map((value) => ({ value: String(value), label: String(value) }))
const modalOpen = computed(
  () => page.provider.open || page.model.open || page.credential.open || page.deletion.open,
)
function setLimit(value: string) {
  const limit = Number(value)
  if (limit === 25 || limit === 50 || limit === 100) void providers.setLimit(limit)
}
</script>

<template>
  <section class="project-providers" aria-label="项目模型设置">
    <header class="page-heading">
      <h1 id="project-providers-title" ref="heading" tabindex="-1">Providers</h1>
      <p>管理本项目的 chat Provider 与 Model 配置。保存配置不代表已连接或调用外部服务。</p>
    </header>
    <div v-if="!visible" class="notice" role="status">
      <p>当前项目身份或读取状态尚未确认，配置内容已隐藏。</p>
      <UiButton @click="page.readOwner()">重新读取项目</UiButton>
    </div>
    <template v-else>
      <p v-if="!canMutate" class="notice" role="status">
        项目当前为只读状态（{{
          currentProject?.lifecycle
        }}）。可以读取信息；原请求恢复遵循其各自的当前条件。
      </p>
      <div class="actions" aria-label="项目模型操作">
        <UiButton
          variant="primary"
          :disabled="blocked || pending || !canMutate"
          @click="page.newProvider()"
          >创建 Provider</UiButton
        >
        <UiButton
          :disabled="blocked || pending || !canMutate"
          @click="page.openCredential('create')"
          >创建凭据</UiButton
        >
        <UiButton :disabled="blocked || pending" @click="page.openCredential('manage')"
          >管理凭据</UiButton
        >
        <UiButton
          :disabled="blocked"
          :aria-expanded="modelsOpen"
          aria-controls="project-models-panel"
          @click="page.showModels()"
          >项目 Models（全部 Providers）</UiButton
        >
      </div>
      <p v-if="blocked" class="meta" role="status">正在等待当前操作完成，暂不能发起另一项请求。</p>
      <p v-else-if="pending" class="meta" role="status">
        有原请求尚未确认，请先查证、原样重放或明确放弃本地追踪。
      </p>
      <p v-if="message && !modalOpen" class="notice" role="status">{{ message }}</p>

      <section v-if="preparedCredential" class="notice" aria-label="已创建凭据">
        <h2>已创建凭据</h2>
        <p>
          {{
            preparedCredential.bound
              ? '已用于本地确认的 Provider 保存。'
              : '尚未绑定。请选择到 Provider 草稿，再单独保存 Provider。'
          }}
        </p>
        <dl>
          <dt>Credential ID</dt>
          <dd class="identifier">{{ preparedCredential.credential_id }}</dd>
          <dt>版本</dt>
          <dd>{{ preparedCredential.version }}</dd>
        </dl>
        <UiButton
          :disabled="blocked || pending"
          @click="page.openCredential('manage', preparedCredential.credential_id)"
          >读取凭据信息</UiButton
        >
      </section>

      <section v-if="progress && !modalOpen" class="notice" aria-label="原请求与历史观察">
        <h2>原请求与历史观察</h2>
        <p>
          操作：{{ progress.kind }}；{{
            progress.phase === 'confirmed'
              ? '原执行已严格确认'
              : progress.phase === 'uncertain'
                ? '结果尚未确认'
                : progress.phase === 'submitting'
                  ? '正在提交'
                  : '本次请求被明确拒绝'
          }}。
        </p>
        <p v-if="progress.keyConflict">原请求标识发生冲突，不能重放或自动换标识。</p>
        <p v-if="progress.observation === 'observed'">
          已观察到历史回执；这不确认本次完整输入，也不会自动推进操作。
        </p>
        <p v-else-if="progress.observation === 'not_observed'">
          本次未观察到历史回执；这不是回滚或未执行证明。
        </p>
        <p v-else-if="progress.observation === 'failed'">本次查证失败，原请求状态保持。</p>
        <pre v-if="progress.receipt" aria-label="严格执行回执">{{
          JSON.stringify(progress.receipt, null, 2)
        }}</pre>
        <pre v-if="progress.observedResult" aria-label="历史观察">{{
          JSON.stringify(progress.observedResult, null, 2)
        }}</pre>
        <p v-if="progress.domain === 'credential' && !canMutate">
          只读项目不能重放凭据写入；仍可明确查证历史。
        </p>
        <div class="actions">
          <UiButton :disabled="!canLookup" @click="page.lookupOriginal()">查证原请求</UiButton>
          <UiButton :disabled="!canReplay" @click="page.replayOriginal()">按原请求重放</UiButton>
          <UiButton :disabled="blocked" @click="page.abandonPending()">放弃本地追踪</UiButton>
        </div>
      </section>

      <section
        aria-labelledby="provider-list-title"
        :aria-busy="providers.state.phase === 'loading'"
      >
        <div class="section-heading">
          <h2 id="provider-list-title">Providers</h2>
          <UiButton :disabled="blocked" @click="providers.fromFirst()">刷新 Providers</UiButton>
        </div>
        <p v-if="providers.state.phase === 'loading'" role="status">正在读取 Providers…</p>
        <p
          v-else-if="providers.state.phase === 'waiting' || providers.state.phase === 'inactive'"
          role="status"
        >
          列表需要重新读取。
        </p>
        <div v-if="providers.state.phase === 'error'" class="notice" role="alert">
          <p>{{ providers.state.message }}</p>
          <UiButton
            :disabled="blocked"
            @click="providers.state.cursorInvalid ? providers.fromFirst() : providers.retry()"
            >{{
              providers.state.cursorInvalid ? '从第一页读取 Providers' : '重试读取 Providers'
            }}</UiButton
          >
        </div>
        <p v-if="providers.state.stale" class="meta" role="status">
          以下为上次读取的旧观察，请明确刷新后核对当前配置。
        </p>
        <p v-if="providers.state.phase === 'empty'" class="notice">
          本页没有 Provider。可以在当前可编辑项目中创建 Provider。
        </p>
        <ul v-if="providers.state.items.length" class="records" aria-label="Providers 列表">
          <li v-for="item in providers.state.items" :key="item.id" :data-provider-id="item.id">
            <h3>{{ item.input.name }}</h3>
            <dl>
              <dt>协议</dt>
              <dd>{{ item.input.protocol }}</dd>
              <dt>状态</dt>
              <dd>{{ item.input.enabled ? '启用' : '禁用' }}</dd>
              <dt>Provider ID</dt>
              <dd class="identifier">{{ item.id }}</dd>
            </dl>
            <div class="actions">
              <UiButton
                :aria-label="`读取 Provider ${item.id}`"
                :disabled="blocked || pending"
                @click="page.readProvider(item.id)"
                >读取 Provider</UiButton
              >
              <UiButton
                :aria-label="`为 Provider ${item.id} 创建 Model`"
                :disabled="blocked || pending || !canMutate"
                @click="page.newModel(item.id)"
                >创建 Model</UiButton
              >
            </div>
          </li>
        </ul>
        <div class="pagination" aria-label="Providers 分页">
          <UiField v-slot="field" label="每页 Providers"
            ><UiSelect
              :id="field.id"
              :model-value="String(providers.state.limit)"
              :options="limits"
              label="每页 Providers"
              :disabled="blocked"
              @update:model-value="setLimit"
          /></UiField>
          <span>第 {{ providers.state.page }} 页</span>
          <UiButton
            :disabled="blocked || !providers.state.hasPrevious"
            @click="providers.previous()"
            >上一页 Providers</UiButton
          >
          <UiButton :disabled="blocked || !providers.state.hasNext" @click="providers.next()"
            >下一页 Providers</UiButton
          >
        </div>
      </section>
      <ProjectModelsPanel v-if="modelsOpen" />
    </template>
    <ProjectProviderEditor />
    <ProjectModelEditor />
    <ProjectModelCredentialEditor />
    <ProjectModelDeleteDialog :fallback-focus="heading" />
  </section>
</template>

<style scoped>
.project-providers {
  display: grid;
  gap: var(--section-gap);
  min-width: 0;
}
.page-heading h1 {
  margin: 0;
  font-size: 26px;
  line-height: 1.35;
}
.page-heading p,
.meta {
  color: var(--muted);
}
h2 {
  font-size: 15px;
  margin: 0 0 12px;
}
h3 {
  font-size: 14px;
  margin: 0;
  overflow-wrap: anywhere;
}
p {
  margin: 0 0 12px;
}
.actions,
.section-heading,
.pagination {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.section-heading {
  justify-content: space-between;
  margin-bottom: 12px;
}
.section-heading h2 {
  margin: 0;
}
.notice {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}
.records {
  display: grid;
  gap: 12px;
  margin: 12px 0;
  padding: 0;
  list-style: none;
}
.records li {
  display: grid;
  gap: 12px;
  padding: 16px;
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
dl {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 4px 12px;
  margin: 0 0 12px;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.identifier,
pre {
  font-family: var(--font-mono);
  font-size: 12px;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 240px;
  overflow: auto;
}
.pagination {
  margin-top: 16px;
}
.pagination :deep(.ui-field) {
  min-width: 110px;
}
@media (max-width: 650px) {
  .page-heading h1 {
    font-size: 23px;
  }
  dl {
    grid-template-columns: minmax(0, 1fr);
  }
  dt {
    margin-top: 4px;
  }
  .records li {
    padding: 12px;
  }
}
</style>
