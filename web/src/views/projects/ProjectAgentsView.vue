<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { UiButton, UiState } from '../../components/ui'
import AgentCreateForm from '../../components/agents/AgentCreateForm.vue'
import { useProjectAgents } from '../../composables/useProjectAgents'
const owner = useProjectAgents(),
  state = owner.state
const createButton = ref<{ $el: HTMLButtonElement } | null>(null)
async function cancelCreation() {
  const p = owner.projectID.value
  await owner.cancelCreation()
  await nextTick()
  if (!owner.draft.open && owner.visible.value && p === owner.projectID.value)
    createButton.value?.$el.focus()
}
const approval = { default: '默认审批', auto: '模型自动审批', allow: '允许工具调用' }
</script>
<template>
  <section class="project-agents" aria-label="项目 Agent 设置">
    <header class="agents-heading">
      <div>
        <h1>Agent</h1>
        <p>当前项目的 Agent 目录与配置。</p>
      </div>
      <UiButton
        ref="createButton"
        v-if="!owner.draft.open && !owner.pending.value"
        :disabled="!owner.canCreate.value || owner.busy.value"
        @click="owner.openCreation()"
        >创建 Agent</UiButton
      >
      <UiButton :disabled="!owner.visible.value || owner.busy.value" @click="owner.refresh()"
        >刷新 Agent 目录</UiButton
      >
    </header>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认项目访问身份" />
    <template v-else>
      <p v-if="!owner.canCreate.value && !owner.pending.value" role="status">
        当前项目为只读，不能创建 Agent。
      </p>
      <p v-if="owner.pending.value && !owner.progress.value" role="status">
        另一项目的 Agent 创建结果待确认，请返回原项目查询原操作。
      </p>
      <p v-if="owner.progress.value?.phase === 'submitting'" role="status">
        正在创建 Agent，请等待原请求结束。
      </p>
      <section
        v-if="owner.progress.value?.phase === 'uncertain'"
        class="agent-recovery"
        aria-label="Agent 创建结果恢复"
      >
        <p role="status">创建结果待确认。请查询原操作结果，不要重复创建。</p>
        <UiButton
          :disabled="owner.busy.value || !owner.progress.value.contextValid"
          @click="owner.lookup()"
          >查询原创建结果</UiButton
        >
      </section>
      <p v-if="state.feedback" role="status">{{ state.feedback }}</p>
      <div class="agents-layout">
        <section class="agent-directory" aria-label="Agent 目录">
          <UiState v-if="state.phase === 'loading'" kind="loading" title="正在读取 Agent 目录" />
          <p v-if="state.message" role="alert">{{ state.message }}</p>
          <p v-if="state.phase === 'ready' && !state.items.length">项目还没有 Agent。</p>
          <ul>
            <li v-for="agent in state.items" :key="agent.id">
              <button
                type="button"
                class="agent-entry"
                :aria-pressed="state.selectedID === agent.id"
                :disabled="owner.busy.value"
                @click="owner.selectAgent(agent.id)"
              >
                <strong>{{ owner.agentLabel(agent) }}</strong
                ><span v-if="agent.display_name">{{ agent.name }}</span
                ><span>{{ agent.description || '暂无描述' }}</span>
              </button>
            </li>
          </ul>
          <UiButton
            v-if="state.next"
            :disabled="owner.busy.value"
            @click="owner.loadDirectory(true)"
            >加载更多 Agent</UiButton
          >
        </section>
        <section class="agent-content" aria-label="Agent 配置">
          <AgentCreateForm
            v-if="owner.draft.open && !owner.pending.value"
            :owner="owner"
            @cancel="cancelCreation"
          />
          <template v-else-if="state.selectedID">
            <UiState
              v-if="state.detailPhase === 'loading'"
              kind="loading"
              title="正在读取 Agent 配置"
            />
            <p v-if="state.detailMessage" role="alert">{{ state.detailMessage }}</p>
            <UiButton
              v-if="state.detailPhase === 'error'"
              :disabled="owner.busy.value"
              @click="owner.loadConfiguration(state.selectedID)"
              >重新读取配置</UiButton
            >
            <template v-if="state.configuration && state.detailPhase === 'ready'">
              <h2>{{ state.configuration.display_name ?? state.configuration.name }}</h2>
              <p>只读配置。目录和配置本身不授予执行权限。</p>
              <dl class="agent-facts">
                <dt>名称</dt>
                <dd>{{ state.configuration.name }}</dd>
                <dt>标准化名称</dt>
                <dd>{{ state.configuration.normalized_name }}</dd>
                <dt>显示名称</dt>
                <dd>{{ state.configuration.display_name ?? '未设置' }}</dd>
                <dt>标签颜色</dt>
                <dd>{{ state.configuration.tag_color ?? '未设置' }}</dd>
                <dt>Agent ID</dt>
                <dd>{{ state.configuration.id }}</dd>
                <dt>Project ID</dt>
                <dd>{{ state.configuration.project_id }}</dd>
                <dt>状态</dt>
                <dd>{{ state.configuration.lifecycle }}</dd>
                <dt>版本</dt>
                <dd>{{ state.configuration.version }}</dd>
                <dt>描述</dt>
                <dd>{{ state.configuration.description || '未设置' }}</dd>
                <dt>指令</dt>
                <dd>{{ state.configuration.instructions || '未设置' }}</dd>
                <dt>AGENTS.md 注入</dt>
                <dd>{{ state.configuration.inject_agents_md ? '启用' : '停用' }}</dd>
                <dt>主模型 ID</dt>
                <dd>{{ state.configuration.model_ref }}</dd>
                <dt>推理强度</dt>
                <dd>{{ state.configuration.reasoning_effort ?? '模型默认值' }}</dd>
                <dt>审批策略</dt>
                <dd>{{ approval[state.configuration.approval_policy] }}</dd>
                <dt>审批模型 ID</dt>
                <dd>{{ state.configuration.approval_model_ref ?? '未设置' }}</dd>
                <dt>Tool 引用</dt>
                <dd>{{ state.configuration.allowed_tool_ids.join('\n') || '无额外引用' }}</dd>
                <dt>Mount 引用</dt>
                <dd>{{ state.configuration.allowed_mount_ids.join('\n') || '无额外引用' }}</dd>
                <dt>Secret Variable 引用</dt>
                <dd>
                  {{ state.configuration.allowed_secret_variable_ids.join('\n') || '无额外引用' }}
                </dd>
                <dt>创建时间</dt>
                <dd>{{ state.configuration.created_at }}</dd>
                <dt>更新时间</dt>
                <dd>{{ state.configuration.updated_at }}</dd>
              </dl>
            </template>
          </template>
          <p v-else-if="!owner.pending.value">选择目录中的 Agent 查看配置，或创建空白 Agent。</p>
        </section>
      </div>
    </template>
  </section>
</template>
<style scoped>
.project-agents {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.agents-heading,
.agent-recovery {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.agents-heading > div {
  flex: 1;
  min-width: 12rem;
}
.agents-heading h1,
.agents-heading p {
  margin: 0;
}
.agents-heading p,
.agent-entry span {
  color: var(--muted);
}
.agents-layout {
  display: grid;
  grid-template-columns: minmax(12rem, 1fr) minmax(0, 2fr);
  gap: var(--space);
  min-width: 0;
}
.agent-directory,
.agent-content {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: var(--space);
  background: var(--surface);
}
.agent-directory ul {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
  padding: 0;
  margin: 0 0 var(--space);
  list-style: none;
}
.agent-entry {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 100%;
  text-align: start;
  padding: var(--content-gap);
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  cursor: pointer;
  overflow-wrap: anywhere;
}
.agent-entry[aria-pressed='true'] {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.agent-entry:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.agent-entry:disabled {
  cursor: default;
  opacity: 0.6;
}
.agent-content h2,
.agent-content p,
.agent-recovery p {
  overflow-wrap: anywhere;
}
.agent-facts {
  display: grid;
  grid-template-columns: minmax(7rem, 1fr) minmax(0, 3fr);
  gap: var(--content-gap);
}
.agent-facts dt {
  color: var(--muted);
}
.agent-facts dd {
  margin: 0;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
@media (max-width: 760px) {
  .agents-layout {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 450px) {
  .agent-facts {
    grid-template-columns: 1fr;
  }
}
</style>
