<script setup lang="ts">
import UiButton from '../../components/ui/UiButton.vue'
import UiField from '../../components/ui/UiField.vue'
import UiSelect from '../../components/ui/UiSelect.vue'
import { useProjectModelSettings } from '../../composables/useProjectModelSettings'
const page = useProjectModelSettings()
const { visible, models, providers, blocked, pending } = page
const limits = [25, 50, 100].map((value) => ({ value: String(value), label: String(value) }))
function providerName(id: string) {
  return providers.state.items.find((provider) => provider.id === id)?.input.name
}
function setLimit(value: string) {
  const limit = Number(value)
  if (limit === 25 || limit === 50 || limit === 100) void models.setLimit(limit)
}
</script>
<template>
  <section
    v-if="visible"
    id="project-models-panel"
    class="models-panel"
    aria-labelledby="project-models-title"
    :aria-busy="models.state.phase === 'loading'"
  >
    <div class="heading">
      <h2 id="project-models-title">项目 Models（全部 Providers）</h2>
      <UiButton :disabled="blocked" @click="models.fromFirst()">刷新 Models</UiButton>
    </div>
    <p class="meta">
      列表包含本项目全部 Providers 的 Models。创建时请在上方 Provider 列表明确选择所属
      Provider；禁用配置仍可读取。
    </p>
    <p v-if="models.state.phase === 'loading'" role="status">正在读取 Models…</p>
    <p
      v-else-if="models.state.phase === 'waiting' || models.state.phase === 'inactive'"
      role="status"
    >
      列表需要重新读取。
    </p>
    <div v-if="models.state.phase === 'error'" class="notice" role="alert">
      <p>{{ models.state.message }}</p>
      <UiButton
        :disabled="blocked"
        @click="models.state.cursorInvalid ? models.fromFirst() : models.retry()"
        >{{ models.state.cursorInvalid ? '从第一页读取 Models' : '重试读取 Models' }}</UiButton
      >
    </div>
    <p v-if="models.state.stale" class="meta" role="status">
      以下为上次读取的旧观察，请明确刷新后核对。
    </p>
    <p v-if="models.state.phase === 'empty'" class="notice">本页没有 Model。</p>
    <ul v-if="models.state.items.length" class="records" aria-label="项目 Models 列表">
      <li v-for="item in models.state.items" :key="item.id" :data-model-id="item.id">
        <h3>{{ item.input.name }}</h3>
        <dl>
          <dt>Model ID</dt>
          <dd class="identifier">{{ item.id }}</dd>
          <dt>Provider ID</dt>
          <dd class="identifier">{{ item.provider_id }}</dd>
          <template v-if="providerName(item.provider_id)"
            ><dt>已读 Provider 名称</dt>
            <dd>{{ providerName(item.provider_id) }}</dd></template
          >
          <dt>原生型号</dt>
          <dd>{{ item.input.provider_model_id }}</dd>
          <dt>类型</dt>
          <dd>{{ item.input.type }}</dd>
          <dt>状态</dt>
          <dd>{{ item.input.enabled ? '启用' : '禁用' }}</dd>
          <dt>版本</dt>
          <dd>{{ item.version }}</dd>
        </dl>
        <UiButton
          :aria-label="`读取 Model ${item.id}`"
          :disabled="blocked || pending"
          @click="page.readModel(item.id)"
          >读取 Model</UiButton
        >
      </li>
    </ul>
    <div class="pagination" aria-label="Models 分页">
      <UiField v-slot="field" label="每页 Models"
        ><UiSelect
          :id="field.id"
          :model-value="String(models.state.limit)"
          :options="limits"
          label="每页 Models"
          :disabled="blocked"
          @update:model-value="setLimit"
      /></UiField>
      <span>第 {{ models.state.page }} 页</span>
      <UiButton :disabled="blocked || !models.state.hasPrevious" @click="models.previous()"
        >上一页 Models</UiButton
      >
      <UiButton :disabled="blocked || !models.state.hasNext" @click="models.next()"
        >下一页 Models</UiButton
      >
    </div>
  </section>
</template>
<style scoped>
.models-panel {
  min-width: 0;
  border-top: 1px solid var(--border);
  padding-top: var(--section-gap);
}
.heading,
.pagination {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.heading {
  justify-content: space-between;
  margin-bottom: 12px;
}
h2 {
  margin: 0;
  font-size: 15px;
}
h3 {
  margin: 0;
  font-size: 14px;
  overflow-wrap: anywhere;
}
.meta,
dt {
  color: var(--muted);
}
p {
  margin: 0 0 12px;
}
.records {
  list-style: none;
  padding: 0;
  display: grid;
  gap: 12px;
}
.records li,
.notice {
  min-width: 0;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.records li {
  display: grid;
  gap: 12px;
  justify-items: start;
}
dl {
  display: grid;
  grid-template-columns: 140px minmax(0, 1fr);
  gap: 4px 12px;
  margin: 0;
  width: 100%;
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.identifier {
  font-family: var(--font-mono);
  font-size: 12px;
}
.pagination {
  margin-top: 16px;
}
.pagination :deep(.ui-field) {
  min-width: 100px;
}
@media (max-width: 650px) {
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
