<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
} from "vue";
import { UiButton, UiDialog, UiInput, UiMenu } from "../../components/ui";
import CommandFeedback from "../../components/account/CommandFeedback.vue";
import {
  accountEnabled,
  AccountError,
  accountErrorMessage,
} from "../../account/transport";
import { useCommand } from "../../account/useCommand";
import { useLeaveProtection } from "../../account/leave";
import { emailError } from "../../account/validation";
import { formatInstant } from "../../system/directory";
import { useSystemAccess } from "../../system/requests";
import { useCursorList } from "../../system/useCursorList";
import {
  retryMailJobCommand,
  mailJobRetryReason,
} from "../../system/account-settings";
import {
  listInvitations,
  createInvitation,
  resendInvitation,
  revokeInvitation,
  deliveryLabel,
  attemptLabel,
  reasonLabel,
  type Invitation,
} from "../../system/invitations";

type Task = "create" | "detail" | "resend" | "retry" | "revoke";
const access = useSystemAccess();
const list = useCursorList(listInvitations);
const operation = useCommand<unknown>();
const task = ref<Task>();
const selected = shallowRef<Invitation>();
const email = ref("");
const submitted = ref(false);
const closeRequested = ref(false);
const feedback = ref("");
const refreshNeeded = ref(false);
const resultTarget = ref<HTMLElement>();
const errorTarget = ref<HTMLElement>();
const now = ref(Date.now());
const clock = window.setInterval(() => {
  now.value = Date.now();
}, 30000);
let alive = true;
let dialogGeneration = 0;
const locked = computed(
  () =>
    !accountEnabled ||
    !access.allowed.value ||
    operation.busy.value ||
    operation.uncertain.value ||
    list.loading.value,
);
const dirty = computed(
  () => task.value === "create" && !!email.value && !operation.confirmed.value,
);
const title = computed(() =>
  task.value
    ? {
        create: "邀请用户",
        detail: "邀请详情",
        resend: "重发邀请",
        retry: "重试投递",
        revoke: "撤销邀请",
      }[task.value]
    : "邀请",
);
const actionLabel = computed(() =>
  task.value
    ? {
        create: "发送邀请",
        detail: "完成",
        resend: "确认重发",
        retry: "确认重试投递",
        revoke: "确认撤销",
      }[task.value]
    : "确认",
);
const issue = computed(() => (submitted.value ? emailError(email.value) : ""));
const conflict = computed(
  () =>
    operation.error.value instanceof AccountError &&
    operation.error.value.code === "VERSION_CONFLICT",
);
const expired = (item: Invitation) => Date.parse(item.expires_at) <= now.value;
const exhausted = (item: Invitation) =>
  BigInt(item.version) >= 9223372036854775807n;
function menuItems(item: Invitation) {
  const unavailable = expired(item) || exhausted(item);
  return [
    { id: "detail", label: "查看详情" },
    {
      id: "resend",
      label: "重发邀请",
      disabled: unavailable || refreshNeeded.value,
    },
    {
      id: "retry",
      label: "重试投递",
      disabled:
        expired(item) ||
        !!mailJobRetryReason(item.latest_delivery) ||
        refreshNeeded.value,
    },
    {
      id: "revoke",
      label: "撤销邀请",
      disabled: unavailable || refreshNeeded.value,
    },
  ];
}
function open(next: Task, item?: Invitation) {
  if (locked.value || (refreshNeeded.value && next !== "detail")) return;
  dialogGeneration++;
  operation.abandon();
  task.value = next;
  selected.value = item;
  email.value = "";
  submitted.value = false;
  closeRequested.value = false;
  feedback.value = "";
}
function close(force = false) {
  if (operation.busy.value) return;
  if (!force && (dirty.value || operation.uncertain.value)) {
    closeRequested.value = true;
    return;
  }
  dialogGeneration++;
  if (operation.uncertain.value) {
    feedback.value =
      "已停止确认原请求，其结果仍未确认。请重新读取当前邀请后再操作。";
    refreshNeeded.value = true;
  }
  operation.abandon();
  task.value = undefined;
  selected.value = undefined;
  email.value = "";
  submitted.value = false;
  closeRequested.value = false;
}
async function refresh() {
  if (locked.value) return;
  await list.first();
  if (!list.error.value && access.allowed.value) refreshNeeded.value = false;
}
async function navigate(action: () => Promise<void>) {
  if (locked.value) return;
  await action();
  await nextTick();
  (list.error.value ? errorTarget.value : resultTarget.value)?.focus();
  resultTarget.value?.scrollIntoView({ block: "start" });
}
async function submit() {
  if (
    locked.value ||
    refreshNeeded.value ||
    conflict.value ||
    operation.confirmed.value ||
    !task.value ||
    task.value === "detail"
  )
    return;
  submitted.value = true;
  if (task.value === "create" && emailError(email.value)) return;
  const target = selected.value;
  if (task.value !== "create" && (!target || expired(target))) return;
  const kind = task.value;
  const captured = dialogGeneration;
  const current = () => alive && captured === dialogGeneration;
  await operation.submit(
    () => {
      if (kind === "create") return createInvitation(email.value, current);
      if (!target) throw new AccountError("INVALID_ARGUMENT");
      if (kind === "resend") return resendInvitation(target, current);
      if (kind === "revoke") return revokeInvitation(target, current);
      if (mailJobRetryReason(target.latest_delivery))
        throw new AccountError("INVALID_STATE");
      return retryMailJobCommand(
        target.latest_delivery.job_id,
        target.latest_delivery.version,
        { current },
      );
    },
    async () => {
      feedback.value =
        kind === "revoke"
          ? "邀请已撤销，原链接已失效。"
          : "请求已受理。最新投递结果请以任务状态为准。";
      await list.first();
      if (!list.error.value && access.allowed.value)
        refreshNeeded.value = false;
    },
    () => {
      refreshNeeded.value = true;
      email.value = "";
      submitted.value = false;
    },
  );
}
async function closeAndRefresh() {
  close(true);
  await refresh();
}
watch(
  access.generation,
  () => {
    dialogGeneration++;
    operation.abandon();
    task.value = undefined;
    selected.value = undefined;
    email.value = "";
    feedback.value = "";
    refreshNeeded.value = false;
  },
  { flush: "sync" },
);
useLeaveProtection(
  () => dirty.value || operation.busy.value || operation.uncertain.value,
);
onBeforeUnmount(() => {
  alive = false;
  dialogGeneration++;
  window.clearInterval(clock);
  email.value = "";
});
</script>

<template>
  <div class="invitation-directory">
    <div class="directory-toolbar">
      <p class="form-note">邀请通过后，用户可注册并加入平台。</p>
      <div class="form-actions">
        <UiButton :disabled="locked" @click="refresh">刷新</UiButton>
        <UiButton
          variant="primary"
          :disabled="locked || refreshNeeded"
          @click="open('create')"
          >邀请用户</UiButton
        >
      </div>
    </div>
    <p v-if="!accountEnabled" class="form-feedback">
      当前为界面预览。连接账号服务后可管理邀请。
    </p>
    <div v-else-if="access.denied.value" class="form-feedback" role="alert">
      <p>当前没有系统管理权限，邀请数据已清除。</p>
      <UiButton @click="access.recheck">重新检查权限</UiButton>
    </div>
    <div
      v-else-if="list.error.value"
      ref="errorTarget"
      tabindex="-1"
      class="form-feedback"
      role="alert"
    >
      <p>{{ accountErrorMessage(list.error.value) }}</p>
      <p v-if="refreshNeeded">已确认的操作不会再次提交，请只重新读取列表。</p>
      <UiButton :disabled="locked" @click="refresh">重新读取邀请</UiButton>
    </div>
    <p v-if="feedback && !task" class="form-feedback" role="status">
      {{ feedback }}
    </p>
    <p class="directory-status form-note" role="status" aria-live="polite">
      {{
        list.loading.value
          ? "正在读取邀请…"
          : list.loaded.value
            ? `第 ${list.index.value + 1} 页，已读取 ${list.items.value.length} 条邀请。`
            : ""
      }}
    </p>
    <div
      ref="resultTarget"
      tabindex="-1"
      class="directory-results"
      aria-label="待注册邀请列表"
    >
      <p
        v-if="list.loaded.value && !list.items.value.length"
        class="directory-empty"
      >
        暂无待注册邀请。已接受、撤销或过期的邀请不会列在这里。
      </p>
      <ul
        v-if="list.items.value.length"
        class="directory-list"
        :aria-busy="list.loading.value"
      >
        <li
          v-for="item in list.items.value"
          :key="item.id"
          class="directory-row"
        >
          <div class="directory-object">
            <strong>{{ item.email }}</strong>
            <span class="directory-meta"
              >{{ expired(item) ? "已过期" : "等待注册" }} · 有效至
              <time
                :datetime="item.expires_at"
                :title="item.expires_at"
                :aria-label="item.expires_at"
                >{{ formatInstant(item.expires_at) }}</time
              ></span
            >
            <span class="directory-delivery"
              >{{ deliveryLabel[item.latest_delivery.phase]
              }}<span
                v-if="
                  item.latest_delivery.phase !== 'unknown' &&
                  item.latest_delivery.attempt_result === 'unknown'
                "
              >
                · 上次尝试结果未知</span
              ></span
            >
          </div>
          <UiMenu
            :label="`${item.email} 的邀请操作`"
            :items="menuItems(item)"
            :disabled="locked"
            @select="open($event as Task, item)"
            ><template #trigger><span>操作</span></template></UiMenu
          >
        </li>
      </ul>
    </div>
    <nav
      v-if="list.loaded.value && access.allowed.value"
      class="directory-pagination"
      aria-label="邀请分页"
    >
      <span class="form-note">第 {{ list.index.value + 1 }} 页</span>
      <div class="form-actions">
        <UiButton
          :disabled="locked || list.index.value === 0"
          @click="navigate(list.previous)"
          >上一页</UiButton
        >
        <UiButton
          :disabled="locked || !list.nextCursor.value"
          @click="navigate(list.next)"
          >下一页</UiButton
        >
      </div>
    </nav>
    <UiDialog
      :open="!!task"
      :title="title"
      :close-on-outside="!operation.busy.value"
      :close-on-escape="!operation.busy.value"
      @update:open="!$event && close()"
    >
      <form v-if="task" class="form-section" @submit.prevent="submit">
        <template v-if="task === 'create'">
          <UiInput
            v-model="email"
            label="邮箱"
            type="email"
            autocomplete="off"
            :error="issue"
            :disabled="locked || operation.confirmed.value"
            hint="邀请从首次创建起 24 小时内有效。"
          />
          <p class="form-note">
            已有有效邀请会由服务处理；已注册用户不能再次受邀。
          </p>
        </template>
        <template v-else-if="selected">
          <p class="dialog-email">{{ selected.email }}</p>
          <template v-if="task === 'detail'">
            <dl class="delivery-details">
              <div>
                <dt>邀请有效至</dt>
                <dd>
                  <time
                    :datetime="selected.expires_at"
                    :title="selected.expires_at"
                    >{{ formatInstant(selected.expires_at) }}</time
                  >
                </dd>
              </div>
              <div>
                <dt>最近任务受理</dt>
                <dd>
                  <time
                    :datetime="selected.latest_delivery.accepted_at"
                    :title="selected.latest_delivery.accepted_at"
                    >{{
                      formatInstant(selected.latest_delivery.accepted_at)
                    }}</time
                  >
                </dd>
              </div>
              <div>
                <dt>当前任务状态</dt>
                <dd>{{ deliveryLabel[selected.latest_delivery.phase] }}</dd>
              </div>
              <div>
                <dt>实际尝试渠道</dt>
                <dd>
                  {{
                    selected.latest_delivery.channel === "smtp"
                      ? "SMTP"
                      : selected.latest_delivery.channel === "backend_log"
                        ? "受限后台恢复日志"
                        : "尚未记录"
                  }}
                </dd>
              </div>
              <div>
                <dt>当前尝试结果</dt>
                <dd>
                  {{
                    selected.latest_delivery.attempt_result
                      ? attemptLabel[selected.latest_delivery.attempt_result]
                      : "尚未记录"
                  }}
                </dd>
              </div>
              <div>
                <dt>尝试次数</dt>
                <dd>{{ selected.latest_delivery.attempts }}</dd>
              </div>
              <div v-if="selected.latest_delivery.reason">
                <dt>结果说明</dt>
                <dd>{{ reasonLabel[selected.latest_delivery.reason] }}</dd>
              </div>
            </dl>
            <p class="form-note">
              受理时间不代表发送时间。SMTP
              接受或日志写入成功均不保证邮件已到达收件箱。
            </p>
            <p
              v-if="selected.latest_delivery.channel === 'backend_log'"
              class="form-note"
            >
              恢复材料仅由获授权运维人员处理，此处不展示链接。
            </p>
            <p
              v-if="mailJobRetryReason(selected.latest_delivery)"
              class="form-note"
            >
              {{ mailJobRetryReason(selected.latest_delivery) }}
            </p>
          </template>
          <p v-else-if="task === 'resend'" class="form-note">
            重新安排发送原邀请。原链接和到期时间保持不变；60
            秒内重复请求可能被拒绝。
          </p>
          <p v-else-if="task === 'retry'" class="form-note">
            使用当前合法配置和原业务材料重新投递，可能导致重复邮件。原邀请的到期时间保持不变。
          </p>
          <p v-else class="form-note">
            撤销后，原邀请链接立即失效。此操作无法恢复。
          </p>
        </template>
        <CommandFeedback
          :error="operation.error.value"
          :uncertain="operation.uncertain.value"
          :replayable="operation.replayable.value"
          :busy="operation.busy.value"
          @retry="operation.retry"
          @abandon="close(true)"
        />
        <div v-if="conflict" class="form-feedback">
          <p>邀请或任务已发生变化。请回到列表读取当前状态，再明确选择操作。</p>
          <UiButton @click="closeAndRefresh">关闭并刷新列表</UiButton>
        </div>
        <p v-if="operation.confirmed.value" class="form-feedback" role="status">
          {{ feedback || "操作已确认。"
          }}{{ list.error.value ? "列表暂未更新，请关闭后重新读取。" : "" }}
        </p>
        <div v-if="closeRequested" class="form-feedback">
          <p>
            {{
              operation.uncertain.value
                ? "原请求结果仍未确认。放弃后将无法继续恢复这个请求。"
                : "关闭将放弃尚未发送的邮箱。"
            }}
          </p>
          <div class="form-actions">
            <UiButton @click="closeRequested = false">继续处理</UiButton
            ><UiButton @click="close(true)">放弃并关闭</UiButton>
          </div>
        </div>
        <div v-else class="form-actions">
          <UiButton
            v-if="task !== 'detail' && !operation.confirmed.value"
            type="submit"
            :variant="task === 'revoke' ? 'danger' : 'primary'"
            :loading="operation.busy.value"
            :disabled="locked || refreshNeeded || conflict"
            >{{ actionLabel }}</UiButton
          >
          <UiButton :disabled="operation.busy.value" @click="close()">{{
            operation.confirmed.value || task === "detail" ? "完成" : "取消"
          }}</UiButton>
        </div>
      </form>
    </UiDialog>
  </div>
</template>

<style scoped>
.directory-toolbar,
.directory-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.directory-toolbar .form-actions {
  flex: none;
}
.directory-status {
  min-height: 20px;
  margin-block: 18px 0;
}
.directory-results {
  scroll-margin-block: 12px;
}
.directory-results:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 5px;
}
.directory-list {
  list-style: none;
  padding: 0;
  margin: 6px 0 24px;
}
.directory-row {
  display: flex;
  gap: 16px;
  justify-content: space-between;
  align-items: flex-start;
  padding: 20px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.directory-row:first-child {
  border-top: 1px solid var(--border-subtle);
}
.directory-object {
  display: grid;
  gap: 7px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.directory-object strong {
  font-size: 14px;
  font-weight: 550;
}
.directory-meta,
.directory-delivery {
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.6;
}
.directory-row :deep(.ui-menu-trigger) {
  flex: none;
}
.directory-empty {
  padding: 32px 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.7;
}
.dialog-email {
  margin: 0;
  font-weight: 550;
  overflow-wrap: anywhere;
}
.delivery-details {
  margin: 0;
  display: grid;
  gap: 15px;
  font-size: 13px;
}
.delivery-details > div {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 12px;
}
.delivery-details dt {
  color: var(--text-secondary);
}
.delivery-details dd {
  margin: 0;
  overflow-wrap: anywhere;
}
@media (max-width: 480px) {
  .directory-toolbar {
    align-items: flex-start;
    flex-wrap: wrap;
  }
  .directory-toolbar .form-actions {
    width: 100%;
  }
  .delivery-details > div {
    grid-template-columns: 90px minmax(0, 1fr);
  }
}
</style>
