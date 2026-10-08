<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from 'vue'
import { UiButton, UiField, UiInput, UiState, UiTextarea } from '../../components/ui'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'

const s = useProjectWorkspace()
const {
  visible,
  detail,
  ownerName,
  draft,
  editor,
  changed,
  readOnly,
  blocked,
  canSave,
  canAdoptCurrent,
  progress,
  canLookup,
  canReplay,
  writeMessage,
  feedback,
} = s
const heading = ref<HTMLElement | null>(null)
const actions = ref<HTMLElement | null>(null)
let alive = true
onBeforeUnmount(() => {
  alive = false
})
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let current: HTMLElement | null = node; current; current = current.parentElement)
    if (current.inert || current.hidden || current.getAttribute('aria-hidden') === 'true')
      return false
  return true
}
async function perform(work: () => unknown | Promise<unknown>, event: Event) {
  const previous =
    event.type === 'submit'
      ? ((event as SubmitEvent).submitter ?? document.activeElement)
      : event.currentTarget
  await work()
  await nextTick()
  if (!alive) return
  const current = document.activeElement
  if (current instanceof HTMLElement && current !== document.body && operable(current)) return
  const target =
    previous instanceof HTMLElement && operable(previous)
      ? previous
      : (actions.value?.querySelector<HTMLElement>('button:not(:disabled)') ?? heading.value)
  if (target && operable(target)) target.focus()
}
</script>

<template>
  <section v-if="visible" class="project-general" aria-labelledby="project-general-heading">
    <h2 id="project-general-heading" ref="heading" tabindex="-1">基本信息</h2>
    <template v-if="detail.project">
      <p class="meta">查看项目身份、名称和描述。</p>
      <dl class="project-facts">
        <div>
          <dt>Project ID</dt>
          <dd>
            <code>{{ detail.project.id }}</code>
          </dd>
        </div>
        <div>
          <dt>Owner</dt>
          <dd>
            {{ ownerName }} <code>{{ detail.project.owner_user_id }}</code>
          </dd>
        </div>
        <div>
          <dt>当前版本</dt>
          <dd>
            <code>{{ detail.project.version }}</code>
          </dd>
        </div>
        <div>
          <dt>创建时间</dt>
          <dd>
            <time :datetime="detail.project.created_at">{{ detail.project.created_at }}</time>
          </dd>
        </div>
        <div>
          <dt>更新时间</dt>
          <dd>
            <time :datetime="detail.project.updated_at">{{ detail.project.updated_at }}</time>
          </dd>
        </div>
        <div v-if="detail.project.archived_at">
          <dt>归档时间</dt>
          <dd>
            <time :datetime="detail.project.archived_at">{{ detail.project.archived_at }}</time>
          </dd>
        </div>
      </dl>
      <form
        v-if="editor.ready"
        class="project-form"
        aria-label="项目基本信息"
        @submit.prevent="perform(s.save, $event)"
      >
        <UiField
          id="project-name"
          v-slot="field"
          label="项目名称"
          required
          :error="editor.fields.name"
          hint="1–64 位英文字母、数字、点、下划线或连字符，不能为 . 或 ..；名称不区分大小写。"
        >
          <UiInput
            :id="field.id"
            v-model="draft.name"
            name="name"
            autocomplete="off"
            autocapitalize="none"
            :spellcheck="false"
            :readonly="readOnly || blocked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
          />
        </UiField>
        <p class="meta">修改名称后旧链接将失效，不提供别名或自动跳转。</p>
        <UiField
          id="project-description"
          v-slot="field"
          label="项目描述"
          :error="editor.fields.description"
          hint="最多 8192 UTF-8 字节；保留空格和换行，留空会清空描述。"
        >
          <UiTextarea
            :id="field.id"
            v-model="draft.description"
            name="description"
            rows="6"
            :readonly="readOnly || blocked"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
          />
        </UiField>
        <p class="meta">
          本次编辑版本：<code>{{ editor.version }}</code>
        </p>
        <p v-if="editor.message" role="status">{{ editor.message }}</p>
        <p v-if="editor.requiresRead" role="status">请明确重读当前信息后再继续编辑。</p>
        <div ref="actions" class="form-actions">
          <UiButton
            type="submit"
            variant="primary"
            :disabled="!canSave"
            :state="feedback"
            loading-label="正在保存"
            success-label="已确认保存"
            >保存修改</UiButton
          >
          <UiButton
            variant="ghost"
            :disabled="blocked || readOnly || !changed"
            @click="perform(s.cancelEdits, $event)"
            >取消修改</UiButton
          >
        </div>
      </form>
      <UiState
        v-else
        kind="empty"
        title="需要重新读取当前项目"
        description="读取后可以继续查看基本信息。"
      />
      <div class="form-actions">
        <UiButton :disabled="blocked" @click="perform(() => s.readCurrent(), $event)"
          >重新读取当前值</UiButton
        >
      </div>
      <section
        v-if="editor.conflict"
        class="project-review"
        aria-labelledby="project-review-heading"
      >
        <h3 id="project-review-heading">核对当前信息</h3>
        <p>原草稿及编辑版本仍保留。先重读当前值，再决定是否用它替换草稿。</p>
        <dl v-if="!editor.requiresRead && detail.phase === 'current'" class="project-facts">
          <div>
            <dt>当前名称</dt>
            <dd>{{ detail.project.name }}</dd>
          </div>
          <div>
            <dt>当前描述</dt>
            <dd class="current-description">{{ detail.project.description || '未填写描述。' }}</dd>
          </div>
          <div>
            <dt>当前版本</dt>
            <dd>
              <code>{{ detail.project.version }}</code>
            </dd>
          </div>
        </dl>
        <div class="form-actions">
          <UiButton :disabled="!canAdoptCurrent" @click="perform(s.adoptCurrent, $event)"
            >使用当前值重新编辑</UiButton
          >
        </div>
      </section>
    </template>
    <section
      v-if="progress || writeMessage"
      class="project-recovery"
      aria-labelledby="project-write-heading"
    >
      <h3 id="project-write-heading">
        {{
          progress?.phase === 'submitting'
            ? '正在保存'
            : progress?.phase === 'uncertain'
              ? '结果不确定'
              : progress?.phase === 'rejected'
                ? '本次请求已明确拒绝'
                : progress?.phase === 'confirmed'
                  ? '命令已确认'
                  : '项目操作'
        }}
      </h3>
      <UiState v-if="progress?.phase === 'submitting'" kind="loading" title="正在等待保存结果" />
      <p v-if="writeMessage" role="status">{{ writeMessage }}</p>
      <template v-if="progress?.phase === 'uncertain'">
        <p v-if="progress.observation === 'in_progress'" role="status">
          查证结果：原命令仍在处理中，请稍后明确查证。
        </p>
        <p v-else-if="progress.observation === 'not_observed'" role="status">
          查证结果：此次未观察到原命令。这不能证明命令未提交或已回滚。
        </p>
        <p v-else-if="progress.observation === 'failed'" role="status">
          未取得有效查证结果，原命令仍未确认。
        </p>
        <p class="meta">重放会使用原请求，仍可能返回原命令的历史结果。</p>
        <div class="form-actions">
          <UiButton :disabled="!canLookup" @click="perform(s.checkOriginal, $event)"
            >查证原命令</UiButton
          >
          <UiButton :disabled="!canReplay" @click="perform(s.replayOriginal, $event)"
            >按原请求重放</UiButton
          >
          <UiButton variant="ghost" :disabled="blocked" @click="perform(s.abandon, $event)"
            >放弃本地追踪</UiButton
          >
        </div>
        <p class="meta">放弃本地追踪仅丢弃页面中的草稿和追踪，不会撤销服务器命令。</p>
      </template>
    </section>
  </section>
</template>

<style scoped>
.project-general,
.project-form,
.project-review,
.project-recovery {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
}
.project-facts {
  display: grid;
  gap: var(--content-gap);
  margin: 0 0 8px;
}
.project-facts > div {
  display: grid;
  grid-template-columns: minmax(88px, 0.3fr) minmax(0, 1fr);
  gap: var(--content-gap);
}
.meta,
dt {
  color: var(--muted);
}
dd {
  min-width: 0;
  margin: 0;
}
dd code {
  display: inline-block;
  max-width: 100%;
}
.form-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
.project-form :deep(textarea) {
  width: 100%;
  resize: vertical;
}
.project-review,
.project-recovery {
  padding-top: var(--section-gap);
  border-top: 1px solid var(--border);
}
.current-description {
  white-space: pre-wrap;
}
p,
dd,
code {
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .project-facts > div {
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
  }
}
</style>
