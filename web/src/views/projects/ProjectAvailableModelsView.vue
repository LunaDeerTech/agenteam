<script setup lang="ts">
import UiButton from '../../components/ui/UiButton.vue'
import UiField from '../../components/ui/UiField.vue'
import UiSelect from '../../components/ui/UiSelect.vue'
import { useProjectModelSettings } from '../../composables/useProjectModelSettings'
const page = useProjectModelSettings()
const { visible, available, blocked } = page
const limits = [25, 50, 100].map((value) => ({ value: String(value), label: String(value) }))
function setLimit(value: string) {
  const limit = Number(value)
  if (limit === 25 || limit === 50 || limit === 100) void available.setLimit(limit)
}
</script>
<template>
  <section class="available-models" aria-labelledby="available-models-title">
    <header>
      <h1 id="available-models-title">可用模型</h1>
      <p class="meta">
        只读查看当前项目可见的 System 与 Project chat
        模型目录。目录中的能力声明不表示已经验证外部调用。
      </p>
    </header>
    <div v-if="!visible" class="notice" role="status">
      <p>当前项目身份或读取状态尚未确认，目录已隐藏。</p>
      <UiButton @click="page.readOwner()">重新读取项目</UiButton>
    </div>
    <template v-else>
      <div class="actions">
        <UiButton :disabled="blocked" @click="available.fromFirst()">刷新可用模型</UiButton
        ><span v-if="blocked" class="meta" role="status">等待当前操作完成。</span>
      </div>
      <div :aria-busy="available.state.phase === 'loading'">
        <p v-if="available.state.phase === 'loading'" role="status">正在读取可用模型…</p>
        <p
          v-else-if="available.state.phase === 'waiting' || available.state.phase === 'inactive'"
          role="status"
        >
          目录需要重新读取。
        </p>
        <div v-if="available.state.phase === 'error'" class="notice" role="alert">
          <p>{{ available.state.message }}</p>
          <UiButton
            :disabled="blocked"
            @click="available.state.cursorInvalid ? available.fromFirst() : available.retry()"
            >{{
              available.state.cursorInvalid ? '从第一页读取可用模型' : '重试读取可用模型'
            }}</UiButton
          >
        </div>
        <p v-if="available.state.stale" class="meta" role="status">
          以下为旧目录观察，请明确刷新后核对当前可用性。
        </p>
        <p v-if="available.state.phase === 'empty'" class="notice">本页没有可用 chat 模型。</p>
        <ul v-if="available.state.items.length" class="records" aria-label="可用模型目录">
          <li v-for="item in available.state.items" :key="item.id" :data-model-id="item.id">
            <h2>{{ item.name }}</h2>
            <dl>
              <dt>来源</dt>
              <dd>{{ item.scope.kind === 'system' ? 'System（只读）' : 'Project（本项目）' }}</dd>
              <dt>Model ID</dt>
              <dd class="identifier">{{ item.id }}</dd>
              <dt>Provider</dt>
              <dd>{{ item.provider_name }}</dd>
              <dt>Provider ID</dt>
              <dd class="identifier">{{ item.provider_id }}</dd>
              <dt>版本</dt>
              <dd>{{ item.version }}</dd>
              <dt>工具调用</dt>
              <dd>{{ item.capabilities.tool_calls ? '支持声明' : '未声明' }}</dd>
              <dt>并行工具</dt>
              <dd>{{ item.capabilities.parallel_tool_calls ? '支持声明' : '未声明' }}</dd>
              <dt>流式</dt>
              <dd>{{ item.capabilities.streaming ? '支持声明' : '未声明' }}</dd>
              <dt>推理</dt>
              <dd>{{ item.capabilities.reasoning ? '支持声明' : '未声明' }}</dd>
              <dt>输入形式</dt>
              <dd>{{ item.capabilities.input_modalities.join('、') || '无' }}</dd>
              <dt>输出形式</dt>
              <dd>{{ item.capabilities.output_modalities.join('、') || '无' }}</dd>
              <dt>结构化输出</dt>
              <dd>{{ item.capabilities.structured_output_modes.join('、') || '无' }}</dd>
              <dt>推理等级</dt>
              <dd>{{ item.capabilities.reasoning_efforts.join('、') || '无' }}</dd>
              <dt>上下文长度</dt>
              <dd>{{ item.capabilities.context_length ?? '未设置' }}</dd>
              <dt>最大输出</dt>
              <dd>{{ item.capabilities.max_output ?? '未设置' }}</dd>
            </dl>
          </li>
        </ul>
      </div>
      <div class="pagination" aria-label="可用模型分页">
        <UiField v-slot="field" label="每页可用模型"
          ><UiSelect
            :id="field.id"
            :model-value="String(available.state.limit)"
            :options="limits"
            label="每页可用模型"
            :disabled="blocked"
            @update:model-value="setLimit" /></UiField
        ><span>第 {{ available.state.page }} 页</span
        ><UiButton :disabled="blocked || !available.state.hasPrevious" @click="available.previous()"
          >上一页可用模型</UiButton
        ><UiButton :disabled="blocked || !available.state.hasNext" @click="available.next()"
          >下一页可用模型</UiButton
        >
      </div>
    </template>
  </section>
</template>
<style scoped>
.available-models {
  min-width: 0;
  display: grid;
  gap: var(--section-gap);
}
h1 {
  margin: 0 0 12px;
  font-size: 26px;
  line-height: 1.35;
}
h2 {
  margin: 0 0 12px;
  font-size: 15px;
  overflow-wrap: anywhere;
}
p {
  margin: 0 0 12px;
}
.meta,
dt {
  color: var(--muted);
}
.actions,
.pagination {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.records {
  display: grid;
  gap: 12px;
  list-style: none;
  padding: 0;
  margin: 0;
}
.records li,
.notice {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  min-width: 0;
}
dl {
  margin: 0;
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 4px 12px;
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.identifier {
  font-family: var(--font-mono);
  font-size: 12px;
}
.pagination :deep(.ui-field) {
  min-width: 110px;
}
@media (max-width: 650px) {
  h1 {
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
