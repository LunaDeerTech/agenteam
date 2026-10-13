<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { UiButton, UiField, UiSelect, UiState } from '../ui'
import type { AgentSkillsOwner } from '../../composables/useAgentSkillAssignments'
const props = defineProps<{ owner: AgentSkillsOwner; agentId: string | null }>()
const emit = defineEmits<{ close: []; original: [] }>()
const heading = ref<HTMLElement | null>(null)
const confirmation = ref<HTMLElement | null>(null)
let trigger: HTMLElement | null = null
const uncertain = computed(() => props.owner.progress.value?.phase === 'uncertain')
const locked = computed(
  () => props.owner.busy.value || !!props.owner.pending.value || !!props.owner.draft.command,
)
function focus() {
  heading.value?.focus()
}
defineExpose({ focus })
async function choose(event: Event, kind: 'assign' | 'disable' | 'enable' | 'remove', id?: string) {
  trigger = event.currentTarget as HTMLElement
  props.owner.choose(kind, id)
  await nextTick()
  if (props.owner.draft.command) confirmation.value?.focus()
}
async function cancel() {
  if (!props.owner.cancel()) return
  await nextTick()
  if (trigger?.isConnected && !(trigger as HTMLButtonElement).disabled) trigger.focus()
  else focus()
}
async function submit(lookup = false) {
  const accepted = await (lookup ? props.owner.lookup() : props.owner.submit())
  await nextTick()
  if (accepted) focus()
}
function close() {
  if (props.owner.close()) emit('close')
}
</script>
<template>
  <section class="agent-skills" aria-label="Agent Skills">
    <header class="skill-heading">
      <h3 ref="heading" tabindex="-1">Agent Skills</h3>
      <UiButton
        :disabled="owner.busy.value || !!owner.pending.value || owner.dirty.value"
        @click="close"
        >关闭技能管理</UiButton
      >
    </header>
    <p>分配决定此 Agent 可使用哪些项目技能。禁用保留分配记录；撤销分配保留技能包。</p>
    <p>这里不会安装技能或启动执行，也不改变 AddSkills 工具的启用设置。</p>
    <p v-if="owner.state.feedback" role="status">{{ owner.state.feedback }}</p>
    <p v-if="owner.state.message" role="alert">{{ owner.state.message }}</p>
    <p v-if="owner.progress.value?.phase === 'submitting'" role="status">
      正在保存技能分配，请等待原请求结束。
    </p>
    <section v-if="uncertain" aria-label="技能操作结果恢复">
      <p role="status">技能操作结果待确认。请查询原操作，不要重复提交。</p>
      <UiButton
        v-if="owner.progress.value?.agentID === agentId"
        :disabled="owner.busy.value || !owner.progress.value?.contextValid"
        @click="submit(true)"
        >查询原技能操作</UiButton
      >
      <UiButton v-else :disabled="owner.busy.value" @click="emit('original')"
        >查看原 Agent</UiButton
      >
    </section>
    <template v-if="owner.state.open">
      <UiState v-if="owner.state.phase === 'loading'" kind="loading" title="正在读取技能分配" />
      <div class="skill-actions">
        <UiButton :disabled="owner.busy.value || !!owner.pending.value" @click="owner.refresh()"
          >重新读取分配</UiButton
        >
        <UiButton v-if="owner.state.next" :disabled="locked" @click="owner.loadAssignments(true)"
          >更多已分配技能</UiButton
        >
      </div>
      <p v-if="owner.state.phase === 'ready'" role="status">
        已加载 {{ owner.state.items.length }} 项分配{{ owner.state.next ? '，还有更多。' : '。' }}
      </p>
      <p v-if="owner.state.phase === 'ready' && !owner.state.items.length">
        此 Agent 尚未分配技能。
      </p>
      <ul class="skill-list">
        <li v-for="item in owner.state.items" :key="item.id">
          <strong>{{ owner.name(item.skill_id) }}</strong>
          <span>{{ item.enabled ? '已启用' : '已禁用' }}</span>
          <small>Skill ID：{{ item.skill_id }}</small>
          <div class="skill-actions">
            <UiButton
              v-if="!owner.metadata.has(item.skill_id)"
              :disabled="locked"
              :aria-label="`读取技能信息 ${item.skill_id}`"
              @click="owner.loadMetadata(item.skill_id)"
              >读取技能信息</UiButton
            >
            <UiButton
              :disabled="locked || !owner.canManage.value"
              :aria-label="`${item.enabled ? '禁用技能' : '启用技能'} ${owner.name(item.skill_id)}`"
              @click="choose($event, item.enabled ? 'disable' : 'enable', item.id)"
              >{{ item.enabled ? '禁用技能' : '启用技能' }}</UiButton
            >
            <UiButton
              :disabled="locked || !owner.canManage.value"
              :aria-label="`撤销分配 ${owner.name(item.skill_id)}`"
              @click="choose($event, 'remove', item.id)"
              >撤销分配</UiButton
            >
          </div>
        </li>
      </ul>
      <section aria-label="分配项目技能" class="skill-candidates">
        <h4>分配项目技能</h4>
        <p>仅列出已读取的候选；可继续加载项目技能。</p>
        <p v-if="owner.state.candidateMessage" role="alert">{{ owner.state.candidateMessage }}</p>
        <UiField label="选择项目技能" v-slot="field">
          <UiSelect
            :id="field.id"
            v-model="owner.draft.selected"
            label="选择项目技能"
            :options="owner.options.value"
            :disabled="locked || !owner.canManage.value || owner.state.candidatePhase !== 'ready'"
          />
        </UiField>
        <div class="skill-actions">
          <UiButton
            v-if="owner.state.candidatePhase === 'idle' || owner.state.candidatePhase === 'error'"
            :disabled="locked"
            @click="owner.loadCandidates()"
            >读取项目技能</UiButton
          >
          <UiButton
            v-if="owner.state.candidateNext"
            :disabled="locked"
            @click="owner.loadCandidates(true)"
            >更多项目技能</UiButton
          >
          <UiButton
            :disabled="
              locked ||
              !owner.canManage.value ||
              !owner.draft.selected ||
              owner.state.candidatePhase !== 'ready'
            "
            @click="choose($event, 'assign')"
            >分配技能</UiButton
          >
        </div>
      </section>
      <form
        v-if="owner.draft.command && !owner.pending.value"
        aria-label="确认技能分配操作"
        @submit.prevent="submit()"
      >
        <h4 ref="confirmation" tabindex="-1">{{ owner.draft.label }}</h4>
        <p>基于 Agent 版本 {{ owner.draft.command.input.expected_version }} 确认此操作。</p>
        <p v-if="owner.stale.value" role="alert">
          当前读取与原确认版本不一致。请取消本次确认，重新读取后再选择。
        </p>
        <div class="skill-actions">
          <UiButton type="submit" :disabled="!owner.canConfirm.value">确认技能操作</UiButton>
          <UiButton :disabled="owner.busy.value" @click="cancel">保留当前分配</UiButton>
        </div>
      </form>
      <UiButton
        v-else-if="owner.dirty.value && !owner.pending.value"
        :disabled="owner.busy.value"
        @click="cancel"
        >清除技能选择</UiButton
      >
    </template>
  </section>
</template>
<style scoped>
.agent-skills {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  min-width: 0;
}
.skill-heading,
.skill-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
.skill-heading {
  justify-content: space-between;
}
.skill-list {
  display: grid;
  gap: var(--space);
  list-style: none;
  padding: 0;
}
.skill-list li,
.skill-candidates,
form {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
  min-width: 0;
}
.skill-list small,
.skill-list strong {
  overflow-wrap: anywhere;
}
h3,
h4,
p {
  margin: 0;
}
h3:focus-visible,
h4:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
}
</style>
