<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  type ComponentPublicInstance,
} from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { projectRoute } from '../../router/auth'
import { UiButton, UiField, UiState } from '../../components/ui'
import { auditFilterActions, auditFilterResourceKinds } from '../../api/project-audit'
import {
  projectAuditAssociationRows,
  projectAuditMetadataFields,
  type ProjectAuditActor,
} from '../../api/project-audit-metadata'
import { useProjectAudit, type ProjectAuditFilterDraft } from '../../composables/useProjectAudit'

const audit = useProjectAudit(),
  state = audit.state
const heading = ref<HTMLElement | null>(null),
  listHeading = ref<HTMLElement | null>(null),
  detailHeading = ref<HTMLElement | null>(null)
const filterForm = ref<HTMLFormElement | null>(null)
const rowControls = new Map<string, HTMLButtonElement>()
let alive = true,
  focusGeneration = 0,
  selectedRow: string | null = null
const current = (own: number, stamp: number) =>
  alive && focusGeneration === own && audit.isCurrent(stamp)
const groups = [
  {
    label: '时间与事件',
    fields: [
      {
        key: 'from',
        label: '起始时间（含）',
        hint: '完整时区，例如 2026-10-08T08:00:00.123456+08:00；最多六位小数。',
      },
      { key: 'to', label: '结束时间（不含）', hint: '必须晚于起始时间；留空表示不限制此边界。' },
      { key: 'action', label: '动作' },
      { key: 'outcome', label: '结果' },
    ],
  },
  {
    label: '操作者与资源',
    fields: [
      { key: 'actor_kind', label: '操作者类型' },
      { key: 'actor_id', label: '操作者 ID', hint: '小写 UUIDv7；service 不能同时指定操作者 ID。' },
      { key: 'resource_kind', label: '资源类型' },
      { key: 'resource_id', label: '资源 ID' },
    ],
  },
  {
    label: '关联 ID',
    fields: [
      { key: 'tool_id', label: 'Tool ID' },
      { key: 'execution_id', label: 'Execution ID' },
      { key: 'operation_id', label: 'Operation ID' },
      { key: 'approval_id', label: 'Approval ID' },
      { key: 'runner_id', label: 'Runner ID' },
      { key: 'agent_id', label: 'Agent ID' },
    ],
  },
] satisfies {
  label: string
  fields: { key: keyof ProjectAuditFilterDraft; label: string; hint?: string }[]
}[]
const outcomes = { success: '成功', denied: '拒绝', failed: '失败', unknown: '结果未知' } as const
const choices: Partial<
  Record<keyof ProjectAuditFilterDraft, readonly { value: string; label: string }[]>
> = {
  action: auditFilterActions.map((value) => ({ value, label: value })),
  outcome: Object.entries(outcomes).map(([value, label]) => ({
    value,
    label: `${label}（${value}）`,
  })),
  actor_kind: ['human', 'agent_run', 'service'].map((value) => ({ value, label: value })),
  resource_kind: auditFilterResourceKinds.map((value) => ({ value, label: value })),
}
const actorFields = (actor: ProjectAuditActor) =>
  actor.kind === 'human'
    ? [{ label: '操作者 ID', value: actor.id }]
    : actor.kind === 'agent_run'
      ? [
          { label: '操作者 ID', value: actor.id },
          { label: '操作者项目 ID', value: actor.project_id },
          { label: '执行 ID', value: actor.execution_id },
        ]
      : [
          { label: '服务', value: actor.service },
          { label: 'Cause Ref', value: actor.cause_ref },
          { label: '操作者项目 ID', value: actor.project_id },
        ]
const metadata = computed(() =>
  state.detail.record ? projectAuditMetadataFields(state.detail.record) : [],
)
function update(field: keyof ProjectAuditFilterDraft, event: Event) {
  audit.updateFilter(field, (event.target as HTMLInputElement | HTMLSelectElement).value)
}
function setRow(id: string, element: Element | ComponentPublicInstance | null) {
  const node = element && '$el' in element ? element.$el : element
  if (node instanceof HTMLButtonElement) rowControls.set(id, node)
  else rowControls.delete(id)
}
async function perform(work: () => Promise<void>, event: Event) {
  const own = ++focusGeneration
  const trigger = event instanceof SubmitEvent ? event.submitter : event.currentTarget
  const pending = work(),
    stamp = audit.focusGeneration()
  await pending
  await nextTick()
  if (!current(own, stamp) || state.mode !== 'list') return
  const invalid = filterForm.value?.querySelector<HTMLInputElement | HTMLSelectElement>(
    '[aria-invalid="true"]',
  )
  if (invalid?.isConnected && !invalid.disabled) invalid.focus()
  else if (trigger instanceof HTMLButtonElement && trigger.isConnected && !trigger.disabled)
    trigger.focus()
  else listHeading.value?.focus()
}
async function openDetail(id: string) {
  if (audit.blocked.value) return
  selectedRow = id
  const own = ++focusGeneration,
    pending = audit.openDetail(id),
    stamp = audit.focusGeneration()
  await nextTick()
  if (current(own, stamp) && state.mode === 'detail' && state.detail.target === id)
    detailHeading.value?.focus()
  await pending
}
async function back() {
  const own = ++focusGeneration
  audit.backToList()
  const stamp = audit.focusGeneration()
  await nextTick()
  if (!current(own, stamp) || state.mode !== 'list') return
  const target = selectedRow ? rowControls.get(selectedRow) : null
  if (target?.isConnected && !target.disabled) target.focus()
  else listHeading.value?.focus()
}
async function cancel() {
  const own = ++focusGeneration
  audit.cancel()
  const stamp = audit.focusGeneration()
  await nextTick()
  if (current(own, stamp))
    (state.mode === 'detail' ? detailHeading.value : listHeading.value)?.focus()
}
async function retryDetail() {
  const own = ++focusGeneration,
    pending = audit.retryDetail(),
    stamp = audit.focusGeneration(),
    target = state.detail.target
  await pending
  await nextTick()
  if (current(own, stamp) && state.mode === 'detail' && state.detail.target === target)
    detailHeading.value?.focus()
}
function retireNavigation() {
  ++focusGeneration
  audit.leave()
  return true
}
onBeforeRouteLeave(retireNavigation)
onBeforeRouteUpdate((to, from) => {
  const source = projectRoute(from.fullPath)
  // The workspace canonicalizes only after Get has granted the current scope.
  // A case-only canonical address keeps that scope and its pending native read.
  if (source?.suffix === '/settings/audit' && to.fullPath === source.path) return true
  return retireNavigation()
})
onMounted(async () => {
  const own = focusGeneration,
    stamp = audit.focusGeneration()
  await nextTick()
  if (current(own, stamp)) heading.value?.focus()
})
onBeforeUnmount(() => {
  alive = false
  ++focusGeneration
  audit.dispose()
  rowControls.clear()
})
</script>

<template>
  <div class="project-audit">
    <h1 ref="heading" tabindex="-1">项目审计</h1>
    <p class="meta">只观察当前项目的安全记录。时间显示为 UTC；分页结果仅表示本次已读取的记录。</p>
    <template v-if="state.mode === 'list'">
      <form
        ref="filterForm"
        class="audit-filters"
        aria-label="项目审计筛选"
        novalidate
        @submit.prevent="perform(audit.apply, $event)"
      >
        <fieldset v-for="group in groups" :key="group.label" :disabled="audit.blocked.value">
          <legend>{{ group.label }}</legend>
          <div class="filter-grid">
            <UiField
              v-for="field in group.fields"
              :id="'project-audit-filter-' + field.key"
              :key="field.key"
              :label="field.label"
              :hint="
                'hint' in field
                  ? field.hint
                  : field.key.endsWith('_id')
                    ? '可空的小写 UUIDv7。'
                    : undefined
              "
              :error="state.fieldErrors[field.key]"
              v-slot="slot"
            >
              <select
                v-if="choices[field.key]"
                :id="slot.id"
                class="ui-input"
                :value="audit.draft[field.key]"
                :aria-invalid="slot.invalid || undefined"
                :aria-describedby="slot.describedby"
                @change="update(field.key, $event)"
              >
                <option value="">全部</option>
                <option
                  v-for="choice in choices[field.key]"
                  :key="choice.value"
                  :value="choice.value"
                >
                  {{ choice.label }}
                </option>
              </select>
              <input
                v-else
                :id="slot.id"
                class="ui-input"
                type="text"
                :value="audit.draft[field.key]"
                :aria-invalid="slot.invalid || undefined"
                :aria-describedby="slot.describedby"
                autocomplete="off"
                spellcheck="false"
                @input="update(field.key, $event)"
              />
            </UiField>
          </div>
        </fieldset>
        <UiField
          id="project-audit-filter-limit"
          label="每页数量"
          hint="1–200 的完整整数，默认 50；应用后从第一页读取。"
          :error="state.fieldErrors.limit"
          v-slot="slot"
        >
          <input
            :id="slot.id"
            class="ui-input page-size"
            type="text"
            inputmode="numeric"
            :value="audit.draft.limit"
            :disabled="audit.blocked.value"
            :aria-invalid="slot.invalid || undefined"
            :aria-describedby="slot.describedby"
            autocomplete="off"
            @input="update('limit', $event)"
          />
        </UiField>
        <p v-if="audit.dirty.value" class="meta" role="status">
          筛选已修改，尚未应用。当前结果仍来自上次已应用的条件。
        </p>
        <p v-if="state.filterMessage" class="error-text" role="alert">{{ state.filterMessage }}</p>
        <div class="audit-actions">
          <UiButton type="submit" variant="primary" :disabled="audit.blocked.value"
            >应用筛选</UiButton
          >
          <UiButton :disabled="audit.blocked.value" @click="perform(audit.reset, $event)"
            >重置筛选</UiButton
          >
        </div>
      </form>
      <section class="audit-observation" aria-labelledby="project-audit-list-heading">
        <h2 id="project-audit-list-heading" ref="listHeading" tabindex="-1">审计记录</h2>
        <div class="audit-actions" aria-label="项目审计分页">
          <UiButton :disabled="audit.blocked.value" @click="perform(audit.refresh, $event)"
            >重新读取</UiButton
          >
          <UiButton
            :disabled="audit.blocked.value || !state.list.hasPrevious"
            @click="perform(audit.previous, $event)"
            >上一页</UiButton
          >
          <UiButton
            :disabled="audit.blocked.value || !state.list.hasNext"
            @click="perform(audit.next, $event)"
            >下一页</UiButton
          >
          <UiButton v-if="audit.reading.value" @click="cancel">取消读取</UiButton>
        </div>
        <UiState
          v-if="state.list.phase === 'waiting' || state.list.phase === 'loading'"
          kind="loading"
          :title="state.list.phase === 'waiting' ? '等待当前项目和请求' : '正在读取项目审计'"
        />
        <UiState
          v-else-if="state.list.phase === 'error'"
          kind="error"
          title="项目审计读取失败"
          :description="state.list.message"
        >
          <UiButton
            v-if="state.list.cursorInvalid"
            :disabled="audit.blocked.value"
            @click="perform(audit.refresh, $event)"
            >返回第一页</UiButton
          >
          <UiButton v-else :disabled="audit.blocked.value" @click="perform(audit.retry, $event)"
            >重试读取</UiButton
          >
        </UiState>
        <UiState
          v-else-if="state.list.phase === 'unavailable'"
          kind="error"
          title="项目审计当前不可用"
          :description="state.list.message"
        >
          <UiButton :disabled="audit.blocked.value" @click="perform(audit.retry, $event)"
            >重试读取</UiButton
          >
        </UiState>
        <UiState v-else-if="state.list.phase === 'empty'" kind="empty" title="没有匹配的审计记录" />
        <div
          v-else-if="state.list.phase === 'ready'"
          class="audit-table"
          role="region"
          aria-label="项目审计列表"
          tabindex="0"
        >
          <table>
            <caption class="sr-only">
              当前已读取的项目审计记录
            </caption>
            <thead>
              <tr>
                <th scope="col">时间（UTC）</th>
                <th scope="col">操作者</th>
                <th scope="col">动作</th>
                <th scope="col">结果</th>
                <th scope="col">资源</th>
                <th scope="col">关联 ID</th>
                <th scope="col">详情</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in state.list.items" :key="row.audit_id">
                <td data-label="时间（UTC）">
                  <time :datetime="row.created_at">{{ row.created_at }}</time>
                </td>
                <td data-label="操作者">
                  <span>{{ row.actor.kind }}</span>
                  <dl class="associations">
                    <template v-for="field in actorFields(row.actor)" :key="field.label"
                      ><dt>{{ field.label }}</dt>
                      <dd>{{ field.value }}</dd></template
                    >
                  </dl>
                </td>
                <td data-label="动作">{{ row.action }}</td>
                <td data-label="结果">{{ outcomes[row.outcome] }}（{{ row.outcome }}）</td>
                <td data-label="资源">
                  <span>{{ row.resource.kind }}</span
                  ><span v-if="'id' in row.resource">{{ row.resource.id }}</span>
                </td>
                <td data-label="关联 ID">
                  <dl
                    v-if="projectAuditAssociationRows(row.associations).length"
                    class="associations"
                  >
                    <template
                      v-for="field in projectAuditAssociationRows(row.associations)"
                      :key="field.key"
                      ><dt>{{ field.label }}</dt>
                      <dd>{{ field.value }}</dd></template
                    >
                  </dl>
                  <span v-else>—</span>
                </td>
                <td data-label="详情">
                  <UiButton
                    :ref="(element) => setRow(row.audit_id, element)"
                    :disabled="audit.blocked.value"
                    :aria-label="'查看详情 ' + row.audit_id"
                    @click="openDetail(row.audit_id)"
                    >查看详情</UiButton
                  >
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p
          v-if="state.list.phase === 'ready' || state.list.phase === 'empty'"
          class="meta"
          role="status"
        >
          第 {{ state.list.page }} 页 · 本页 {{ state.list.items.length }} 条 · 仅显示已读取的结果
        </p>
      </section>
    </template>
    <section v-else class="audit-observation" aria-labelledby="project-audit-detail-heading">
      <h2 id="project-audit-detail-heading" ref="detailHeading" tabindex="-1">审计详情</h2>
      <div class="audit-actions">
        <UiButton @click="back">返回列表</UiButton>
        <UiButton :disabled="audit.blocked.value" @click="retryDetail">重新读取详情</UiButton>
        <UiButton v-if="audit.reading.value" @click="cancel">取消读取</UiButton>
      </div>
      <UiState v-if="state.detail.phase === 'loading'" kind="loading" title="正在读取审计详情" />
      <UiState
        v-else-if="state.detail.phase === 'not-found'"
        kind="error"
        title="记录不存在或不可访问"
      />
      <UiState
        v-else-if="state.detail.phase === 'error' || state.detail.phase === 'unavailable'"
        kind="error"
        :title="state.detail.phase === 'unavailable' ? '项目审计当前不可用' : '审计详情读取失败'"
        :description="state.detail.message"
      />
      <template v-else-if="state.detail.phase === 'ready' && state.detail.record">
        <dl class="detail-fields">
          <dt>审计 ID</dt>
          <dd>{{ state.detail.record.audit_id }}</dd>
          <dt>时间（UTC）</dt>
          <dd>
            <time :datetime="state.detail.record.created_at">{{
              state.detail.record.created_at
            }}</time>
          </dd>
          <dt>范围</dt>
          <dd>{{ state.detail.record.scope }}</dd>
          <dt>项目 ID</dt>
          <dd>{{ state.detail.record.project_id }}</dd>
          <dt>操作者类型</dt>
          <dd>{{ state.detail.record.actor.kind }}</dd>
          <template v-for="field in actorFields(state.detail.record.actor)" :key="field.label"
            ><dt>{{ field.label }}</dt>
            <dd>{{ field.value }}</dd></template
          >
          <dt>动作</dt>
          <dd>{{ state.detail.record.action }}</dd>
          <dt>结果</dt>
          <dd>{{ outcomes[state.detail.record.outcome] }}（{{ state.detail.record.outcome }}）</dd>
          <dt>资源类型</dt>
          <dd>{{ state.detail.record.resource.kind }}</dd>
          <template v-if="'id' in state.detail.record.resource"
            ><dt>资源 ID</dt>
            <dd>{{ state.detail.record.resource.id }}</dd></template
          >
          <dt>摘要</dt>
          <dd>{{ state.detail.record.summary }}</dd>
        </dl>
        <h3>结构化证据</h3>
        <dl class="detail-fields">
          <template v-for="field in metadata" :key="field.key"
            ><dt>{{ field.label }}</dt>
            <dd>{{ field.value }}</dd></template
          >
        </dl>
        <h3>安全关联 ID</h3>
        <dl
          v-if="projectAuditAssociationRows(state.detail.record.associations).length"
          class="detail-fields"
        >
          <template
            v-for="field in projectAuditAssociationRows(state.detail.record.associations)"
            :key="field.key"
            ><dt>{{ field.label }}</dt>
            <dd>{{ field.value }}</dd></template
          >
        </dl>
        <p v-else class="meta">无关联 ID</p>
      </template>
    </section>
  </div>
</template>

<style scoped>
.project-audit,
.audit-filters,
.audit-observation {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.audit-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
fieldset {
  min-width: 0;
  margin: 0;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
legend {
  padding: 0 6px;
  font-weight: 600;
}
.filter-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.filter-grid :deep(.ui-field) {
  min-width: 0;
}
input,
select {
  width: 100%;
  min-width: 0;
}
.page-size {
  max-width: 240px;
}
.audit-table {
  position: relative;
  min-width: 0;
  max-width: 100%;
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  clip-path: inset(50%);
}
table {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  text-align: start;
}
th,
td {
  padding: 12px;
  white-space: normal;
  overflow-wrap: anywhere;
  vertical-align: top;
  border-bottom: 1px solid var(--border);
}
th {
  background: var(--surface);
  font-weight: 600;
}
td > span {
  display: block;
}
tbody tr:last-child td {
  border-bottom: 0;
}
time {
  font-variant-numeric: tabular-nums;
}
dl {
  margin: 0;
  min-width: 0;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
  white-space: normal;
}
.associations {
  display: grid;
  gap: 4px;
}
.associations dd + dt {
  margin-top: 8px;
}
.detail-fields {
  display: grid;
  grid-template-columns: minmax(130px, 1fr) minmax(0, 3fr);
  gap: 12px 20px;
}
@media (min-width: 761px) {
  thead th:last-child {
    width: 120px;
  }
}
@media (max-width: 760px) {
  .filter-grid,
  .detail-fields {
    grid-template-columns: minmax(0, 1fr);
  }
  table,
  tbody,
  tr,
  td {
    display: block;
  }
  thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  tr + tr {
    border-top: 1px solid var(--border);
  }
  td {
    padding: 8px 12px;
    border: 0;
  }
  td::before {
    content: attr(data-label);
    display: block;
    margin-bottom: 4px;
    color: var(--muted);
    font-size: 12px;
  }
  .detail-fields {
    gap: 8px;
  }
  .detail-fields dd + dt {
    margin-top: 8px;
  }
}
</style>
