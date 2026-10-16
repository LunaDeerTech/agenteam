<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, provide, ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { UiButton, UiCard, UiDialog, UiState } from '../../components/ui'
import { createSystemRunners, systemRunnersKey } from '../../composables/useSystemRunners'
import SystemRunnerEditor from './SystemRunnerEditor.vue'
import SystemRunnerEnrollmentPanel from './SystemRunnerEnrollmentPanel.vue'
const page = createSystemRunners()
provide(systemRunnersKey, page)
const { state, visible, blocked, locked, progress, business, confirmation, editor } = page
const heading = ref<HTMLElement | null>(null),
  detailHeading = ref<HTMLElement | null>(null)
const status = { online: '在线', offline: '离线', incompatible: '协议不兼容' }
let trigger: HTMLElement | null = null
async function select(id: string, event: Event) {
  if (locked.value) return
  trigger = event.currentTarget as HTMLElement
  const pending = page.select(id)
  await nextTick()
  if (visible.value && state.selected === id) detailHeading.value?.focus()
  await pending
}
async function back() {
  if (locked.value) return
  page.clearMaterial()
  state.selected = null
  state.detail = null
  state.detailPhase = 'inactive'
  await nextTick()
  if (trigger?.isConnected) trigger.focus()
}
onMounted(page.attach)
onBeforeRouteLeave(async () => {
  const allowed = await page.confirmLeave()
  if (allowed) page.clearMaterial()
  return allowed
})
onBeforeRouteUpdate(async () => {
  const allowed = await page.confirmLeave()
  if (allowed) page.clearMaterial()
  return allowed
})
onBeforeUnmount(page.dispose)
</script>
<template>
  <section class="system-runners" aria-label="Runner 设备管理">
    <header class="page-header">
      <div class="page-heading">
        <h1 ref="heading" tabindex="-1">Runner 设备</h1>
        <p>管理系统设备与登记身份；设备在线不代表可以执行命令。</p>
      </div>
      <div class="runner-actions page-actions">
        <UiButton variant="primary" :disabled="locked" @click="page.openCreate"
          >创建 Runner</UiButton
        ><UiButton :disabled="blocked || !visible" variant="ghost" @click="page.readList()"
          >重新读取目录</UiButton
        ><UiButton v-if="blocked" variant="ghost" @click="page.cancel">停止当前请求</UiButton>
      </div>
    </header>
    <UiState v-if="!visible" kind="loading" title="正在确认系统管理员身份" />
    <template v-else>
      <p v-if="state.writeMessage" role="status">{{ state.writeMessage }}</p>
      <UiCard
        v-if="progress?.phase === 'uncertain' && !editor.mode && !business.kind"
        aria-label="原命令状态未确认"
        ><h2>原命令状态未确认</h2>
        <p>不要重复提交。检查只使用原设备 ID、完整请求和幂等键；历史回执不包含登记 token。</p>
        <p v-if="progress.observation === 'not_observed'">
          尚未观察到提交，不表示回滚。请继续检查原请求。
        </p>
        <UiButton :disabled="blocked || !progress.contextValid" @click="page.checkOriginal"
          >检查原请求</UiButton
        ></UiCard
      >
      <SystemRunnerEnrollmentPanel />
      <UiState v-if="state.phase === 'loading'" kind="loading" title="正在读取 Runner 目录" />
      <UiState
        v-else-if="state.phase === 'error'"
        kind="error"
        title="Runner 目录读取失败"
        :description="state.message"
        ><UiButton :disabled="blocked" @click="page.readList()">重新读取目录</UiButton></UiState
      >
      <template v-else-if="state.phase === 'ready'">
        <p v-if="!state.rows.length">当前没有 Runner 设备。</p>
        <ul v-else class="runner-list page-surface" aria-label="Runner 目录">
          <li v-for="row in state.rows" :key="row.id">
            <div class="runner-row-heading">
              <UiButton
                variant="ghost"
                :disabled="locked"
                :aria-pressed="state.selected === row.id"
                @click="select(row.id, $event)"
                >{{ row.name }}</UiButton
              ><span>{{ status[row.status] }}</span>
            </div>
            <p class="runner-description" :title="row.description">
              {{ row.description || '暂无描述' }}
            </p>
            <p v-if="row.last_hello">
              {{ row.last_hello.os }} / {{ row.last_hello.arch }} ·
              {{ row.last_hello.runner_version }}
            </p>
            <p>最后联系：{{ row.last_seen_at ?? '尚无报告' }}</p>
            <p v-if="row.tags.length">{{ row.tags.join(' · ') }}</p>
          </li>
        </ul>
        <p v-if="state.message" role="alert">{{ state.message }}</p>
        <footer v-if="state.next" class="page-footer">
          <UiButton :disabled="blocked" @click="page.readList(true)">加载更多 Runner</UiButton>
        </footer>
      </template>
      <UiCard v-if="state.selected" class="runner-detail" aria-label="Runner 详情">
        <h2 ref="detailHeading" tabindex="-1">{{ state.detail?.name ?? 'Runner 详情' }}</h2>
        <div class="runner-actions page-actions">
          <UiButton variant="ghost" :disabled="locked" @click="back">返回目录</UiButton
          ><UiButton :disabled="locked" @click="page.refreshDetail">重新读取详情</UiButton>
        </div>
        <UiState
          v-if="state.detailPhase === 'loading'"
          kind="loading"
          title="正在读取当前 Runner"
        />
        <UiState
          v-else-if="state.detailPhase === 'error'"
          kind="error"
          title="当前 Runner 读取失败"
          :description="state.detailMessage"
        />
        <template v-else-if="state.detail"
          ><dl class="runner-facts">
            <dt>状态</dt>
            <dd>{{ status[state.detail.status] }}</dd>
            <dt>描述</dt>
            <dd>{{ state.detail.description || '暂无描述' }}</dd>
            <dt>标签</dt>
            <dd>{{ state.detail.tags.join(' · ') || '无' }}</dd>
            <dt>Runner ID</dt>
            <dd>{{ state.detail.id }}</dd>
            <dt>根目录</dt>
            <dd>{{ state.detail.root_path }}</dd>
            <dt>配置版本</dt>
            <dd>{{ state.detail.version }}</dd>
            <dt>身份代际</dt>
            <dd>{{ state.detail.credential_generation }}</dd>
            <dt>创建时间</dt>
            <dd>{{ state.detail.created_at }}</dd>
            <dt>更新时间</dt>
            <dd>{{ state.detail.updated_at }}</dd>
            <dt>登记时间</dt>
            <dd>{{ state.detail.enrolled_at ?? '未登记' }}</dd>
            <dt>公钥指纹</dt>
            <dd>{{ state.detail.public_key_fingerprint ?? '未登记' }}</dd>
            <dt>最后联系</dt>
            <dd>{{ state.detail.last_seen_at ?? '尚无报告' }}</dd>
            <template v-if="state.detail.last_hello"
              ><dt>设备平台</dt>
              <dd>{{ state.detail.last_hello.os }} / {{ state.detail.last_hello.arch }}</dd>
              <dt>Runner 版本</dt>
              <dd>{{ state.detail.last_hello.runner_version }}</dd>
              <dt>协议版本</dt>
              <dd>
                {{ state.detail.last_hello.protocol_version.major }}.{{
                  state.detail.last_hello.protocol_version.minor
                }}
              </dd>
              <dt>无界面运行</dt>
              <dd>{{ state.detail.last_hello.headless ? '是' : '否' }}</dd>
              <dt>特性报告</dt>
              <dd>{{ state.detail.last_hello.feature_flags.join(' · ') || '未报告特性' }}</dd>
              <dt>能力报告</dt>
              <dd>
                {{ state.detail.last_hello.capabilities.join(' · ') || '未报告可用能力' }}
              </dd></template
            >
          </dl>
          <div class="runner-actions page-actions">
            <UiButton :disabled="locked" @click="page.openEdit">编辑 Runner</UiButton
            ><UiButton :disabled="locked" @click="page.openBusiness('enrollment')"
              >重新签发登记材料</UiButton
            ><UiButton variant="danger" :disabled="locked" @click="page.openBusiness('revoke')"
              >撤销 Runner 凭据</UiButton
            >
          </div></template
        >
      </UiCard>
      <SystemRunnerEditor />
      <UiDialog
        :open="business.kind !== null"
        :title="business.kind === 'enrollment' ? '重新签发登记材料？' : '撤销 Runner 凭据？'"
        :close-on-outside="false"
        @update:open="!$event && !blocked && (business.kind = null)"
      >
        <p>
          将使该设备旧密钥、登记 token、挑战与连接失效。设备记录和项目 Mount
          引用保留；即使尚未登记也会推进身份代际。
        </p>
        <p v-if="state.writeMessage" role="alert">{{ state.writeMessage }}</p>
        <UiButton
          v-if="progress?.phase === 'uncertain'"
          :disabled="blocked || !progress.contextValid"
          @click="page.checkOriginal"
          >检查原请求</UiButton
        >
        <template #footer
          ><UiButton variant="ghost" :disabled="blocked" @click="business.kind = null"
            >取消</UiButton
          ><UiButton variant="danger" :disabled="locked" @click="page.confirmBusiness">{{
            business.kind === 'enrollment' ? '确认重新签发' : '确认撤销凭据'
          }}</UiButton></template
        >
      </UiDialog>
      <UiDialog
        :open="confirmation.open"
        title="放弃未保存的修改？"
        :close-on-outside="false"
        @update:open="!$event && page.finishConfirmation(false)"
        ><p>未保存的表单内容将被清除。已发送的未决命令仍只能查询原请求。</p>
        <template #footer
          ><UiButton variant="ghost" @click="page.finishConfirmation(false)">继续编辑</UiButton
          ><UiButton @click="page.finishConfirmation(true)">放弃修改</UiButton></template
        ></UiDialog
      >
    </template>
  </section>
</template>
<style scoped>
.page-header {
  margin-bottom: 4px;
}
.system-runners,
.runner-detail {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.system-runners p,
.system-runners h2,
.system-runners dd {
  overflow-wrap: anywhere;
}
.runner-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.runner-actions :deep(button) {
  white-space: normal;
  max-width: 100%;
}
.runner-list {
  padding: 0;
  margin: 0;
  list-style: none;
  display: grid;
  gap: 0;
}
.runner-list li {
  border-bottom: 1px solid var(--border);
  padding: 12px 16px;
}
.runner-list span {
  color: var(--muted);
}
.runner-list li:last-child {
  border-bottom: 0;
}
.runner-row-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.runner-row-heading :deep(button) {
  padding-inline: 0;
  min-width: 0;
  text-align: left;
  overflow-wrap: anywhere;
}
.runner-row-heading span {
  flex-shrink: 0;
  font-size: 12px;
}
.runner-list li > p {
  color: var(--muted);
  font-size: 12px;
}
.runner-description {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.runner-facts {
  display: grid;
  grid-template-columns: minmax(100px, 1fr) minmax(0, 3fr);
  gap: 10px 20px;
  margin: 0;
}
.runner-facts dt {
  color: var(--muted);
}
.runner-facts dd {
  margin: 0;
  min-width: 0;
}
@media (max-width: 600px) {
  .runner-facts {
    grid-template-columns: minmax(0, 1fr);
    gap: 6px;
  }
  .runner-facts dd {
    margin-bottom: 10px;
  }
}
</style>
