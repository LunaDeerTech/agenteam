<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, onUnmounted, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiState from '../../components/ui/UiState.vue'
import SystemModelEditor from './SystemModelEditor.vue'
import SystemModelDeleteDialog from './SystemModelDeleteDialog.vue'
import { useSystemModels } from '../../composables/useSystemModels'
const page = useSystemModels()
const {
  pages,
  provider,
  detail,
  editor,
  deletion,
  state,
  progress,
  blocked,
  locked,
  confirmation,
} = page
const heading = ref<HTMLElement | null>(null),
  actions = ref<HTMLElement | null>(null)
const businessOpen = computed(() => editor.mode !== null || deletion.open)
let alive = true
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let current: HTMLElement | null = node; current; current = current.parentElement)
    if (current.inert || current.hidden || current.getAttribute('aria-hidden') === 'true')
      return false
  return true
}
function restorePageFocus(previous?: EventTarget | null) {
  if (!alive || businessOpen.value || confirmation.open || !heading.value?.isConnected) return
  const target =
    previous instanceof HTMLElement && operable(previous)
      ? previous
      : (actions.value?.querySelector<HTMLElement>('button:not(:disabled)') ?? heading.value)
  if (operable(target)) target.focus()
}
async function perform(work: () => Promise<unknown>, event: Event) {
  const previous = event.currentTarget
  await work()
  if (!alive) return
  await nextTick()
  if (!blocked.value) restorePageFocus(previous)
}
watch(businessOpen, async (open, previous) => {
  if (open || !previous) return
  await nextTick()
  if (!alive || confirmation.open || businessOpen.value) return
  const focused = document.activeElement
  if (focused instanceof HTMLElement && focused !== document.body && operable(focused)) return
  restorePageFocus()
})
onMounted(async () => {
  page.attach()
  await nextTick()
  if (
    alive &&
    !businessOpen.value &&
    !confirmation.open &&
    heading.value &&
    operable(heading.value)
  )
    heading.value.focus()
})
onBeforeUnmount(() => {
  alive = false
})
onUnmounted(page.detach)
</script>
<template>
  <div class="system-models">
    <h1 ref="heading" tabindex="-1">Models</h1>
    <p class="meta">
      先选择 Provider，再管理其 System Models。保存只确认配置，不会调用或验证外部模型服务。
    </p>
    <div ref="actions" class="model-actions" aria-label="Model 页面操作">
      <UiButton
        v-if="provider.phase !== 'inactive'"
        variant="ghost"
        :disabled="blocked"
        @click="perform(page.back, $event)"
        >返回 Providers</UiButton
      >
      <UiButton :disabled="blocked" @click="perform(page.refresh, $event)">刷新列表</UiButton>
      <UiButton v-if="provider.value" :disabled="locked" @click="page.openCreate"
        >创建 Model</UiButton
      >
    </div>
    <p v-if="page.auth.state.busy" class="meta" role="status">正在等待当前请求结束。</p>
    <UiState
      v-if="state.requiresReload"
      kind="error"
      title="请重新读取当前配置"
      description="本地追踪已停止，此前请求仍可能生效。请明确刷新后继续管理。"
    />
    <UiState
      v-if="state.confirmed && state.writeMessage.startsWith('操作已确认，当前配置读取失败')"
      kind="error"
      title="操作已确认，当前配置读取失败"
      :description="state.writeMessage"
    />
    <p v-else-if="state.writeMessage && !businessOpen" role="status">{{ state.writeMessage }}</p>
    <template v-if="provider.phase === 'inactive'">
      <h2>选择 Provider</h2>
      <div class="model-actions" aria-label="主 Provider 分页">
        <UiButton
          :disabled="blocked || !pages.providers.hasPrevious"
          @click="perform(() => page.pageAction('providers', 'previous'), $event)"
          >上一页</UiButton
        >
        <UiButton
          :disabled="blocked || !pages.providers.hasNext"
          @click="perform(() => page.pageAction('providers', 'next'), $event)"
          >下一页</UiButton
        >
      </div>
      <UiState
        v-if="pages.providers.phase === 'loading'"
        kind="loading"
        title="正在读取 Providers"
      />
      <UiState
        v-else-if="pages.providers.phase === 'error'"
        kind="error"
        title="Provider 列表读取失败"
        :description="pages.providers.message"
      >
        <UiButton
          :disabled="blocked"
          @click="perform(() => page.pageAction('providers', 'retry'), $event)"
          >{{ pages.providers.cursorInvalid ? '返回首页重新加载' : '重新读取 Providers' }}</UiButton
        >
      </UiState>
      <UiState
        v-else-if="pages.providers.phase === 'empty'"
        kind="empty"
        title="当前页没有 Provider"
        description="请先配置 Provider，或明确返回上一页。"
      >
        <RouterLink to="/system/providers">前往 Providers 配置</RouterLink>
      </UiState>
      <div
        v-else-if="pages.providers.phase === 'ready'"
        class="model-table"
        role="region"
        aria-label="选择 Provider 列表"
        tabindex="0"
      >
        <table>
          <caption class="sr-only">
            可管理配置的 Providers
          </caption>
          <thead>
            <tr>
              <th scope="col">名称</th>
              <th scope="col">协议</th>
              <th scope="col">启用状态</th>
              <th scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in pages.providers.rows" :key="row.id">
              <td data-label="名称">{{ row.input.name }}</td>
              <td data-label="协议">{{ row.input.protocol }}</td>
              <td data-label="启用状态">{{ row.input.enabled ? '启用' : '禁用' }}</td>
              <td data-label="操作">
                <UiButton
                  variant="ghost"
                  :disabled="blocked"
                  @click="perform(() => page.selectProvider(row), $event)"
                  >选择 Provider</UiButton
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
    <UiState
      v-else-if="provider.phase === 'loading'"
      kind="loading"
      title="正在读取当前 Provider"
    />
    <UiState
      v-else-if="provider.phase === 'error'"
      kind="error"
      title="当前 Provider 读取失败"
      :description="provider.message"
    >
      <UiButton :disabled="blocked" @click="perform(page.retryProvider, $event)"
        >重新读取当前 Provider</UiButton
      >
    </UiState>
    <template v-else-if="provider.value">
      <h2>{{ provider.value.input.name }} 的 Models</h2>
      <p class="meta">
        协议：{{ provider.value.input.protocol }}；Provider
        {{ provider.value.input.enabled ? '启用' : '禁用' }}。禁用 Provider 的配置仍可管理。
      </p>
      <div class="model-actions" aria-label="主 Model 分页">
        <UiButton
          :disabled="blocked || !pages.models.hasPrevious"
          @click="perform(() => page.pageAction('models', 'previous'), $event)"
          >上一页</UiButton
        >
        <UiButton
          :disabled="blocked || !pages.models.hasNext"
          @click="perform(() => page.pageAction('models', 'next'), $event)"
          >下一页</UiButton
        >
      </div>
      <UiState v-if="pages.models.phase === 'loading'" kind="loading" title="正在读取 Models" />
      <UiState
        v-else-if="pages.models.phase === 'error'"
        kind="error"
        title="Model 列表读取失败"
        :description="pages.models.message"
      >
        <UiButton
          :disabled="blocked"
          @click="perform(() => page.pageAction('models', 'retry'), $event)"
          >{{ pages.models.cursorInvalid ? '返回首页重新加载' : '重新读取 Models' }}</UiButton
        >
      </UiState>
      <UiState
        v-else-if="pages.models.phase === 'empty'"
        kind="empty"
        title="当前页没有 Model"
        description="可在当前 Provider 下创建 Model，或返回上一页。"
      >
        <UiButton :disabled="locked" @click="page.openCreate">创建 Model</UiButton>
      </UiState>
      <div
        v-else-if="pages.models.phase === 'ready'"
        class="model-table"
        role="region"
        aria-label="Model 配置列表"
        tabindex="0"
      >
        <table>
          <caption class="sr-only">
            当前 Provider 的 Models
          </caption>
          <thead>
            <tr>
              <th scope="col">名称</th>
              <th scope="col">原生 Model ID</th>
              <th scope="col">类型</th>
              <th scope="col">启用状态</th>
              <th scope="col">Provider</th>
              <th scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in pages.models.rows" :key="row.id">
              <td data-label="名称">{{ row.input.name }}</td>
              <td data-label="原生 Model ID">{{ row.input.provider_model_id }}</td>
              <td data-label="类型">{{ row.input.type }}</td>
              <td data-label="启用状态">{{ row.input.enabled ? '启用' : '禁用' }}</td>
              <td data-label="Provider">{{ provider.value.input.name }}</td>
              <td data-label="操作">
                <UiButton
                  variant="ghost"
                  :disabled="blocked"
                  @click="perform(() => page.selectModel(row), $event)"
                  >查看 Model</UiButton
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <UiState v-if="detail.phase === 'loading'" kind="loading" title="正在读取 Model 详情" />
      <UiState
        v-else-if="detail.phase === 'error'"
        kind="error"
        title="Model 详情读取失败"
        :description="detail.message"
      >
        <UiButton :disabled="blocked" @click="perform(page.retryDetail, $event)"
          >重新读取 Model 详情</UiButton
        >
      </UiState>
      <section v-else-if="detail.value" class="model-detail" aria-label="Model 安全详情">
        <h2>Model 详情：{{ detail.value.input.name }}</h2>
        <dl>
          <dt>ID</dt>
          <dd>{{ detail.value.id }}</dd>
          <dt>Provider</dt>
          <dd>
            {{ provider.value.input.name }} · {{ provider.value.input.protocol }} ·
            {{ provider.value.input.enabled ? '启用' : '禁用' }}
          </dd>
          <dt>版本</dt>
          <dd>{{ detail.value.version }}</dd>
          <dt>创建时间（UTC）</dt>
          <dd>{{ detail.value.created_at }}</dd>
          <dt>更新时间（UTC）</dt>
          <dd>{{ detail.value.updated_at }}</dd>
        </dl>
        <div class="model-actions">
          <UiButton :disabled="locked" @click="page.openEdit">编辑 Model</UiButton>
          <UiButton variant="danger" :disabled="locked" @click="page.openDelete"
            >删除 Model</UiButton
          >
        </div>
        <h3>完整安全配置</h3>
        <pre>{{ JSON.stringify(detail.value.input, null, 2) }}</pre>
      </section>
    </template>
    <SystemModelEditor />
    <SystemModelDeleteDialog />
    <!-- All Model dialogs leave/check/restore together; confirmation is last. -->
    <UiDialog
      :open="confirmation.open"
      :title="confirmation.title"
      @update:open="!$event && page.finishConfirmation(false)"
    >
      <p>{{ confirmation.message }}</p>
      <template #footer>
        <UiButton variant="ghost" @click="page.finishConfirmation(false)">继续编辑</UiButton>
        <UiButton @click="page.finishConfirmation(true)">放弃修改</UiButton>
      </template>
    </UiDialog>
  </div>
</template>
<style scoped>
.system-models {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.system-models p,
.system-models h2,
.system-models h3 {
  overflow-wrap: anywhere;
}
.model-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.model-actions :deep(button),
.model-table :deep(button) {
  white-space: normal;
  max-width: 100%;
}
.model-table {
  position: relative;
  min-width: 0;
  width: 100%;
}
.model-table table {
  width: 100%;
  table-layout: fixed;
}
.model-table th,
.model-table td {
  white-space: normal;
  overflow-wrap: anywhere;
  vertical-align: top;
}
.model-detail {
  min-width: 0;
  display: grid;
  gap: 16px;
}
.model-detail dl {
  display: grid;
  grid-template-columns: minmax(120px, 180px) minmax(0, 1fr);
  gap: 8px 14px;
}
.model-detail dd {
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
}
.model-detail pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  min-width: 0;
  max-width: 100%;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}
@media (max-width: 700px) {
  .model-table table,
  .model-table tbody,
  .model-table tr {
    display: block;
  }
  .model-table thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .model-table tr {
    padding: 8px 0;
  }
  .model-table td {
    display: grid;
    grid-template-columns: minmax(90px, 32%) minmax(0, 1fr);
    gap: 10px;
    min-width: 0;
    width: auto;
  }
  .model-table td::before {
    content: attr(data-label);
    font-weight: 600;
  }
  .model-detail dl {
    grid-template-columns: minmax(0, 1fr);
  }
  .model-detail dd {
    margin-bottom: 10px;
  }
}
</style>
