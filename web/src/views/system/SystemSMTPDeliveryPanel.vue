<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import UiState from '../../components/ui/UiState.vue'
import {
  useSystemSMTPDelivery,
  useSMTPSections,
  smtpJobKinds,
  smtpJobPhases,
  smtpAttemptResults,
  smtpJobReasons,
  smtpChannelLabel,
} from '../../composables/useSystemSMTPDelivery'
const page = useSystemSMTPDelivery(),
  sections = useSMTPSections()
const {
  list,
  detail,
  mode,
  draft,
  state,
  progress,
  feedback,
  blocked,
  locked,
  recipientError,
  canTest,
  available,
  retryReady,
  retryReason,
} = page
const actions = ref<HTMLElement | null>(null)
let alive = true
const identity = page.auth.personalContext.identity
function current() {
  const value = page.auth.personalContext.identity
  return (
    alive &&
    sections.active.value === 'delivery' &&
    page.auth.state.phase === 'authenticated' &&
    page.auth.personalContext.phase === 'current' &&
    identity &&
    value &&
    identity.userID === value.userID &&
    identity.sessionID === value.sessionID &&
    identity.epoch === value.epoch &&
    page.auth.state.user?.role === 'admin' &&
    !page.auth.system.denied
  )
}
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let a: HTMLElement | null = node; a; a = a.parentElement)
    if (a.inert || a.hidden || a.getAttribute('aria-hidden') === 'true') return false
  return true
}
async function perform(work: () => Promise<unknown>, event: Event) {
  const generation = sections.generation,
    previous = event instanceof SubmitEvent ? event.submitter : event.currentTarget
  await work()
  if (!current() || generation !== sections.generation) return
  await nextTick()
  if (
    !current() ||
    generation !== sections.generation ||
    blocked.value ||
    page.confirmation.open ||
    page.retryConfirmation.open
  )
    return
  const focused = document.activeElement
  if (focused instanceof HTMLElement && focused !== document.body && operable(focused)) return
  const target =
    previous instanceof HTMLElement && operable(previous)
      ? previous
      : actions.value?.querySelector<HTMLElement>('button:not(:disabled)')
  if (target && operable(target)) target.focus()
}
onBeforeUnmount(() => {
  alive = false
})
</script>
<template>
  <section class="smtp-delivery" aria-label="SMTP 测试与任务">
    <p class="meta">
      测试使用服务端已保存配置；请求接受、任务当前状态和实际尝试结果分别记录。已报告接受不保证收件箱送达。
    </p>
    <form class="test-form" @submit.prevent="perform(page.requestTest, $event)">
      <h2>请求一次测试</h2>
      <UiField
        v-slot="control"
        label="测试收件邮箱"
        hint="仅为本次请求输入，不是任何历史任务的收件人字段。"
        :error="recipientError"
        required
      >
        <UiInput
          :id="control.id"
          :model-value="draft.recipient"
          :invalid="control.invalid"
          :aria-describedby="control.describedby"
          autocomplete="off"
          inputmode="email"
          :disabled="locked"
          @update:model-value="page.updateRecipient($event)"
        />
      </UiField>
      <div class="actions">
        <UiButton
          type="submit"
          variant="primary"
          :disabled="!canTest"
          :state="feedback === 'loading' && progress?.kind === 'test' ? 'loading' : 'idle'"
          >请求测试发送</UiButton
        >
      </div>
      <p v-if="!available.ready" class="notice">
        尚无本会话下完整的配置观察。请返回配置区明确读取，再请求测试。
      </p>
      <p v-else-if="!available.configured" class="notice">
        当前观察为 SMTP 未配置，不能开始新的测试请求。
      </p>
      <div v-if="!available.ready || !available.configured" class="actions">
        <UiButton variant="ghost" @click="sections.switchTo('configuration')">返回配置区</UiButton>
      </div>
    </form>
    <p v-if="page.auth.state.busy" class="meta" role="status">正在等待当前请求结束。</p>
    <p v-if="state.writeMessage" class="notice" role="status">{{ state.writeMessage }}</p>
    <section v-if="state.confirmed" class="confirmation-fact" aria-label="本次请求的接受确认">
      <h2>{{ state.confirmed.kind === 'test' ? '测试请求已接受' : '新重试周期已接受' }}</h2>
      <p>
        新 JobID：<code>{{ state.confirmed.jobID }}</code
        ><span v-if="state.confirmed.version"
          >；响应时新周期版本：<code>{{ state.confirmed.version }}</code></span
        >。
      </p>
      <p v-if="state.confirmed.sourceID">
        本次申请的源 JobID：<code>{{ state.confirmed.sourceID }}</code
        >。只表示本次操作的关联，不是完整投递历史。
      </p>
      <div class="actions">
        <UiButton
          :disabled="blocked"
          @click="perform(() => page.openDetail(state.confirmed!.jobID), $event)"
          >重新读取已确认任务</UiButton
        >
      </div>
    </section>
    <UiState
      v-if="progress?.phase === 'uncertain'"
      kind="error"
      title="请求结果未确认"
      description="没有匹配的完整接受确认。任务列表、详情和会话都不能证明原请求是否接受；明确放弃只停止客户端追踪。"
    >
      <div class="actions">
        <UiButton :disabled="blocked" @click="perform(page.checkOriginal, $event)"
          >检查当前状态</UiButton
        >
        <UiButton
          :disabled="blocked || !progress.canRetryOriginal"
          @click="perform(page.retryOriginal, $event)"
          >重试原请求</UiButton
        >
        <UiButton variant="ghost" @click="page.abandonOperation">放弃追踪本次操作</UiButton>
      </div>
    </UiState>
    <p v-if="state.requiresRead" class="notice">请先明确重新读取并核对任务，再开始新操作。</p>
    <div ref="actions" class="actions" aria-label="投递任务读取操作">
      <template v-if="mode === 'list'">
        <UiButton :disabled="blocked" @click="perform(page.refresh, $event)">刷新任务列表</UiButton>
        <UiButton v-if="list.phase === 'loading'" variant="ghost" @click="page.cancelList"
          >取消列表读取</UiButton
        >
      </template>
      <template v-else>
        <UiButton variant="ghost" @click="page.backToList">返回任务列表</UiButton>
        <UiButton :disabled="blocked" @click="perform(page.rereadDetail, $event)"
          >重新读取任务详情</UiButton
        >
        <UiButton v-if="detail.phase === 'loading'" variant="ghost" @click="page.cancelDetail"
          >取消详情读取</UiButton
        >
      </template>
    </div>
    <section v-if="mode === 'list'" aria-label="投递任务列表">
      <h2>投递任务</h2>
      <UiState v-if="list.phase === 'waiting'" kind="loading" title="等待当前请求结束后读取任务" />
      <UiState v-else-if="list.phase === 'loading'" kind="loading" title="正在读取任务列表" />
      <UiState
        v-else-if="list.phase === 'error'"
        kind="error"
        title="任务列表读取失败"
        :description="list.message"
      />
      <UiState v-else-if="list.phase === 'empty'" kind="empty" title="当前页没有投递任务" />
      <div v-else-if="list.phase === 'ready'" class="table-container">
        <table>
          <caption class="sr-only">
            当前页投递任务的完整安全观察
          </caption>
          <thead>
            <tr>
              <th scope="col">任务</th>
              <th scope="col">阶段</th>
              <th scope="col">实际尝试</th>
              <th scope="col">次数与版本</th>
              <th scope="col">创建时间</th>
              <th scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in list.rows" :key="row.job_id">
              <td data-label="任务">
                <span>{{ smtpJobKinds[row.kind] }}</span
                ><code>{{ row.job_id }}</code>
              </td>
              <td data-label="阶段">{{ smtpJobPhases[row.phase] }}</td>
              <td data-label="实际尝试">
                <span>{{ smtpChannelLabel(row.attempt_channel) }}</span
                ><span>{{
                  row.attempt_result === null
                    ? '尚无已报告终局结果'
                    : smtpAttemptResults[row.attempt_result]
                }}</span>
              </td>
              <td data-label="次数与版本">
                <span>尝试 {{ row.attempts }} 次</span><span>版本 {{ row.version }}</span>
              </td>
              <td data-label="创建时间">
                <time :datetime="row.created_at">{{ row.created_at }}</time>
              </td>
              <td data-label="操作">
                <UiButton
                  :disabled="blocked"
                  :aria-label="'查看任务 ' + row.job_id"
                  @click="perform(() => page.openDetail(row.job_id), $event)"
                  >查看详情</UiButton
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="actions" aria-label="任务分页">
        <UiButton
          :disabled="blocked || !list.hasPrevious"
          @click="perform(page.previousPage, $event)"
          >上一页</UiButton
        >
        <span>第 {{ list.page }} 页，每页最多 25 条</span>
        <UiButton :disabled="blocked || !list.hasNext" @click="perform(page.nextPage, $event)"
          >下一页</UiButton
        >
        <UiButton
          v-if="list.phase === 'error'"
          variant="ghost"
          :disabled="blocked"
          @click="perform(page.firstPage, $event)"
          >重新读取第一页</UiButton
        >
      </div>
    </section>
    <section v-else aria-label="投递任务详情">
      <h2>任务详情</h2>
      <p>
        精确 JobID：<code>{{ detail.jobID }}</code>
      </p>
      <UiState v-if="detail.phase === 'loading'" kind="loading" title="正在读取任务详情" />
      <UiState
        v-else-if="detail.phase === 'error'"
        kind="error"
        :title="
          state.confirmed?.jobID === detail.jobID
            ? '请求已确认，任务详情读取失败'
            : '任务详情读取失败'
        "
        :description="detail.message"
      />
      <template v-else-if="detail.value">
        <dl class="job-values">
          <div>
            <dt>任务类型</dt>
            <dd>{{ smtpJobKinds[detail.value.kind] }}</dd>
          </div>
          <div>
            <dt>当前阶段</dt>
            <dd>{{ smtpJobPhases[detail.value.phase] }}</dd>
          </div>
          <div>
            <dt>尝试次数</dt>
            <dd>{{ detail.value.attempts }}</dd>
          </div>
          <div>
            <dt>当前版本</dt>
            <dd>{{ detail.value.version }}</dd>
          </div>
          <div>
            <dt>实际当前尝试渠道</dt>
            <dd>{{ smtpChannelLabel(detail.value.attempt_channel) }}</dd>
          </div>
          <div>
            <dt>实际当前尝试结果</dt>
            <dd>
              {{
                detail.value.attempt_result === null
                  ? '尚无已报告终局结果'
                  : smtpAttemptResults[detail.value.attempt_result]
              }}
            </dd>
          </div>
          <div>
            <dt>创建时间</dt>
            <dd>
              <time :datetime="detail.value.created_at">{{ detail.value.created_at }}</time>
            </dd>
          </div>
          <div>
            <dt>安全原因</dt>
            <dd>
              {{
                detail.value.reason === undefined ? '未报告' : smtpJobReasons[detail.value.reason]
              }}
            </dd>
          </div>
          <div>
            <dt>兼容渠道摘要</dt>
            <dd>{{ smtpChannelLabel(detail.value.channel) }}；不证明已尝试或下次使用渠道。</dd>
          </div>
        </dl>
        <p class="meta">
          任务结果未知与客户端请求未确认是两个不同状态。已报告接受表示 SMTP
          接受或后台日志持久写入，不保证收件箱送达。本页没有历史收件人、恢复链接或完整尝试记录。
        </p>
        <p v-if="retryReason" class="notice">{{ retryReason }}</p>
        <div class="actions">
          <UiButton :disabled="!retryReady" @click="page.requestRetry">申请新重试周期</UiButton>
        </div>
      </template>
    </section>
  </section>
</template>
<style scoped>
.smtp-delivery,
section,
form {
  display: grid;
  gap: 20px;
  min-width: 0;
}
h2 {
  font-size: 1.1rem;
}
.meta,
dt {
  color: var(--muted);
}
p,
dt,
dd,
code,
time {
  overflow-wrap: anywhere;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.test-form {
  max-width: 720px;
}
.table-container {
  position: relative;
  min-width: 0;
}
table {
  width: 100%;
  table-layout: fixed;
}
th,
td {
  white-space: normal;
  overflow-wrap: anywhere;
  vertical-align: top;
}
td > span,
td > code {
  display: block;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
.job-values {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px;
  margin: 0;
}
.job-values > div {
  display: grid;
  gap: 6px;
  min-width: 0;
}
dd {
  margin: 0;
}
@media (max-width: 760px) {
  table,
  tbody,
  tr,
  td {
    display: block;
  }
  thead {
    display: none;
  }
  tr {
    padding-block: 12px;
    border-bottom: 1px solid var(--border);
  }
  td {
    display: grid;
    grid-template-columns: 96px minmax(0, 1fr);
    gap: 6px 12px;
    padding: 10px 0;
  }
  td::before {
    content: attr(data-label);
    color: var(--muted);
    grid-column: 1;
    grid-row: 1 / span 3;
  }
  td > * {
    grid-column: 2;
  }
  .job-values {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
