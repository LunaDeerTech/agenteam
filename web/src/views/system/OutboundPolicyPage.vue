<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import {
  UiButton,
  UiDialog,
  UiInput,
  UiSelect,
  UiTabs,
} from "../../components/ui";
import SettingsPageActions from "../../layouts/SettingsPageActions.vue";
import { useLeaveProtection } from "../../account/leave";
import {
  AccountError,
  accountEnabled,
  accountErrorMessage,
} from "../../account/transport";
import { useSystemAccess } from "../../system/requests";
import {
  getOutboundPolicy,
  getOutboundProxy,
  saveOutboundPolicy,
  saveOutboundProxy,
  ruleDrafts,
  ruleIssues,
  proxyDraft,
  proxyIssues,
} from "../../system/outbound-policy";
import { useOutboundDraft } from "../../system/useOutboundDraft";

const access = useSystemAccess();
const section = ref("rules");
const rules = useOutboundDraft({
  read: getOutboundPolicy,
  draft: ruleDrafts,
  write: saveOutboundPolicy,
});
const proxy = useOutboundDraft({
  read: getOutboundProxy,
  draft: proxyDraft,
  write: saveOutboundProxy,
  clearSecret: (draft) => {
    draft.password = "";
  },
});
const active = computed(() => (section.value === "rules" ? rules : proxy));
const submitted = ref(false);
const checking = ref(false);
const page = ref<HTMLElement | null>(null);
const confirmation = ref<
  "cancel" | "adopt" | "abandon" | "remove" | "delete"
>();
const deleteIndex = ref<number>();
const commandBusy = computed(
  () => rules.command.busy.value || proxy.command.busy.value || checking.value,
);
const unresolved = computed(
  () => rules.command.uncertain.value || proxy.command.uncertain.value,
);
const tabs = computed(() =>
  [
    { value: "rules", label: "访问规则" },
    { value: "proxy", label: "代理设置" },
  ].map((item) => ({
    ...item,
    disabled:
      item.value !== section.value && (commandBusy.value || unresolved.value),
  })),
);
const issues = computed(() =>
  section.value === "rules"
    ? ruleIssues(rules.draft.value)
    : proxyIssues(proxy.draft.value, proxy.baseline.value),
);
const fieldError = (name: string) => {
  if (
    submitted.value &&
    !active.value.command.uncertain.value &&
    !active.value.command.busy.value &&
    issues.value[name]
  )
    return issues.value[name];
  const path =
    section.value === "rules"
      ? name === "rule_mode"
        ? "/rule_mode"
        : `/rules/${name.replace(".", "/")}`
      : name === "password"
        ? "/password"
        : `/gateway/${name}`;
  const error = active.value.command.error.value;
  return error instanceof AccountError &&
    error.fields.some((field) => field.path === path)
    ? "请检查此项内容后再试。"
    : undefined;
};
const locked = computed(() => active.value.locked.value || checking.value);
const ruleModeOptions = [
  { value: "allowlist", label: "白名单" },
  { value: "denylist", label: "黑名单" },
];
const ruleModeHint = computed(() =>
  rules.draft.value.ruleMode === "allowlist"
    ? "仅允许匹配目标和端口的访问。"
    : "阻止匹配目标和端口的访问，其余允许。",
);
const modeOptions = [
  { value: "direct", label: "直连" },
  { value: "proxy", label: "使用代理" },
];
const protocolOptions = [
  { value: "https", label: "HTTPS CONNECT" },
  { value: "http", label: "HTTP CONNECT" },
  { value: "socks5", label: "SOCKS5" },
];
async function load() {
  await active.value.load();
}
async function submit() {
  if (locked.value) return;
  submitted.value = true;
  if (Object.keys(issues.value).length) {
    await nextTick();
    page.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
    return;
  }
  await active.value.submit();
  await nextTick();
  page.value?.querySelector<HTMLElement>('[role="alert"]')?.focus();
}
async function inspect() {
  if (checking.value || commandBusy.value) return;
  checking.value = true;
  try {
    if (active.value.command.uncertain.value)
      await active.value.command.verify();
    else await access.recheck();
  } finally {
    checking.value = false;
  }
}
async function confirm() {
  const action = confirmation.value;
  confirmation.value = undefined;
  if (!access.allowed.value || commandBusy.value) return;
  const generation = access.generation.value;
  if (action === "cancel") active.value.cancel();
  if (action === "adopt") active.value.adoptLatest();
  if (action === "abandon") await active.value.abandon();
  if (action === "remove") {
    proxy.draft.value.username = "";
    proxy.draft.value.password = "";
    proxy.draft.value.removeCredential = true;
  }
  if (action === "delete" && deleteIndex.value !== undefined && !locked.value) {
    rules.draft.value.rules.splice(deleteIndex.value, 1);
    deleteIndex.value = undefined;
  }
  submitted.value = false;
  // Resetting can disable or remove the dialog's original trigger.
  // Only explicit dialog confirmations hand focus to another control.
  await nextTick();
  requestAnimationFrame(() => {
    if (!access.allowed.value || generation !== access.generation.value) return;
    const selector =
      action === "delete"
        ? ".rules-heading button"
        : 'form input:not(:disabled), form [role="combobox"]:not(:disabled), .rules-heading button:not(:disabled)';
    const target =
      page.value?.querySelector<HTMLElement>(selector) ??
      page.value?.querySelector<HTMLElement>(
        '[role="tab"][aria-selected="true"]',
      );
    target?.focus();
  });
}
function addRule() {
  if (locked.value || rules.draft.value.rules.length >= 512) return;
  rules.draft.value.rules.push({ target: "", ports: "" });
  void nextTick(() =>
    page.value
      ?.querySelector<HTMLElement>(".rule-row:last-child input")
      ?.focus(),
  );
}
watch(section, () => {
  submitted.value = false;
  if (!active.value.baseline.value) void load();
});
watch(access.allowed, (allowed) => {
  if (allowed && !active.value.baseline.value) void load();
});
watch(access.generation, () => {
  confirmation.value = undefined;
  deleteIndex.value = undefined;
  submitted.value = false;
});
onMounted(() => void load());
useLeaveProtection(
  () =>
    rules.dirty.value ||
    proxy.dirty.value ||
    commandBusy.value ||
    unresolved.value,
);
</script>

<template>
  <div ref="page" class="outbound-page">
    <SettingsPageActions>
      <UiButton
        v-if="accountEnabled && access.allowed.value"
        :loading="active.reading.value"
        :disabled="commandBusy || unresolved"
        @click="load"
        >读取最新配置</UiButton
      >
    </SettingsPageActions>
    <p v-if="!accountEnabled" class="form-feedback">
      当前为界面预览，连接账号服务后可读取和修改设置。
    </p>
    <div v-else-if="!access.allowed.value" class="form-feedback" role="alert">
      <p>你目前没有系统管理权限。</p>
      <UiButton :loading="checking" @click="inspect">重新检查权限</UiButton>
    </div>
    <UiTabs
      v-else-if="access.allowed.value"
      v-model="section"
      label="出站设置"
      :items="tabs"
    >
      <p
        v-if="
          active.reading.value &&
          !active.baseline.value &&
          !active.command.confirmed.value
        "
        class="form-note"
        role="status"
      >
        {{ section === "rules" ? "正在读取访问规则…" : "正在读取代理设置…" }}
      </p>
      <p
        v-if="active.command.confirmed.value && !active.baseline.value"
        class="form-note form-success"
        role="status"
      >
        {{ section === "rules" ? "访问规则已保存。" : "代理设置已保存。" }}
        {{ active.reading.value ? "正在读取当前配置…" : "当前配置尚未读回。" }}
      </p>
      <div
        v-if="active.readError.value"
        class="form-feedback"
        role="alert"
        tabindex="-1"
      >
        <p>
          {{
            active.command.confirmed.value
              ? "保存已确认，当前配置读取失败。"
              : "当前配置读取失败。"
          }}
          {{ accountErrorMessage(active.readError.value) }}
        </p>
        <UiButton
          :loading="active.reading.value"
          :disabled="commandBusy"
          @click="load"
          >重新读取配置</UiButton
        >
      </div>
      <form
        v-if="active.baseline.value"
        class="outbound-form"
        @submit.prevent="submit"
      >
        <template v-if="section === 'rules'">
          <UiSelect
            v-model="rules.draft.value.ruleMode"
            class="rule-mode"
            label="规则模式"
            :options="ruleModeOptions"
            :hint="ruleModeHint"
            :error="fieldError('rule_mode')"
            :disabled="locked"
          />
          <div class="rules-heading">
            <h2>访问目标</h2>
            <UiButton
              :disabled="locked || rules.draft.value.rules.length >= 512"
              @click="addRule"
              >添加规则</UiButton
            >
          </div>
          <div class="rule-list">
            <table class="rules-table" aria-label="访问规则">
              <colgroup>
                <col />
                <col class="ports-column" />
                <col class="action-column" />
              </colgroup>
              <thead>
                <tr>
                  <th scope="col">目标</th>
                  <th scope="col">端口</th>
                  <th scope="col"><span class="visually-hidden">操作</span></th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!rules.draft.value.rules.length">
                  <td colspan="3" class="empty-rules">
                    {{
                      rules.draft.value.ruleMode === "allowlist"
                        ? "未添加规则，所有业务请求将被阻止。"
                        : "未添加规则，允许访问所有目标。"
                    }}
                  </td>
                </tr>
                <tr
                  v-for="(rule, index) in rules.draft.value.rules"
                  :key="index"
                  class="rule-row"
                  :aria-label="`规则 ${index + 1}`"
                >
                  <td>
                    <UiInput
                      v-model="rule.target"
                      label="目标"
                      placeholder="IP、网段或域名"
                      aria-describedby="rule-target-help"
                      :error="fieldError(`${index}.target`)"
                      :disabled="locked"
                      autocomplete="off"
                      spellcheck="false"
                    />
                  </td>
                  <td>
                    <UiInput
                      v-model="rule.ports"
                      label="端口"
                      placeholder="443 或 all"
                      :error="fieldError(`${index}.ports`)"
                      :disabled="locked"
                      autocomplete="off"
                    />
                  </td>
                  <td class="rule-action">
                    <UiButton
                      variant="ghost"
                      :disabled="locked"
                      :aria-label="`删除规则 ${index + 1}`"
                      @click="
                        deleteIndex = index;
                        confirmation = 'delete';
                      "
                      >删除</UiButton
                    >
                  </td>
                </tr>
              </tbody>
            </table>
            <p id="rule-target-help" class="form-note">
              目标支持 IP、网段、域名；*.example.com 仅匹配子域。
            </p>
            <p
              v-if="submitted && issues.rules"
              class="form-feedback"
              role="alert"
            >
              {{ issues.rules }}
            </p>
          </div>
        </template>
        <fieldset v-else class="proxy-fields" :disabled="locked">
          <UiSelect
            v-model="proxy.draft.value.mode"
            label="连接方式"
            :options="modeOptions"
            :disabled="locked"
          />
          <template
            v-if="
              proxy.draft.value.mode === 'proxy' ||
              proxy.baseline.value?.gateway ||
              proxy.draft.value.host ||
              proxy.draft.value.port ||
              proxy.draft.value.username ||
              proxy.draft.value.password
            "
          >
            <p v-if="proxy.draft.value.mode === 'direct'" class="form-note">
              当前使用直连，以下代理配置会保留。
            </p>
            <UiSelect
              v-model="proxy.draft.value.protocol"
              label="代理服务器类型"
              :options="protocolOptions"
              :disabled="locked"
            />
            <div class="gateway-address">
              <UiInput
                v-model="proxy.draft.value.host"
                label="代理主机"
                placeholder="例如 proxy.example.com"
                hint="仅填写 IP 或域名，不含协议前缀。"
                :error="fieldError('host')"
                autocomplete="off"
                spellcheck="false"
              /><UiInput
                v-model="proxy.draft.value.port"
                label="代理端口"
                placeholder="7890"
                :error="fieldError('port')"
                inputmode="numeric"
                autocomplete="off"
              />
            </div>
            <UiInput
              v-model="proxy.draft.value.username"
              label="用户名（可选）"
              :error="fieldError('username')"
              autocomplete="off"
            />
            <UiInput
              v-model="proxy.draft.value.password"
              label="密码（可选）"
              type="password"
              :secret-configured="
                !!proxy.baseline.value?.gateway?.credential_present &&
                !proxy.draft.value.removeCredential
              "
              :placeholder="
                proxy.baseline.value?.gateway?.credential_present &&
                !proxy.draft.value.removeCredential
                  ? '留空保持现有密码'
                  : '可选代理密码'
              "
              :error="fieldError('password')"
              autocomplete="new-password"
            />
            <div
              v-if="proxy.baseline.value?.gateway?.credential_present"
              class="credential-status"
            >
              <span class="form-note">{{
                proxy.draft.value.removeCredential
                  ? "保存后移除身份认证。"
                  : "已设置代理密码。"
              }}</span
              ><UiButton
                v-if="!proxy.draft.value.removeCredential"
                variant="ghost"
                :disabled="locked"
                @click="confirmation = 'remove'"
                >移除身份认证</UiButton
              >
            </div>
            <p
              v-if="
                proxy.draft.value.mode === 'proxy' &&
                proxy.draft.value.protocol !== 'https'
              "
              class="form-note"
            >
              到 HTTP / SOCKS5 代理网关的连接不加密，代理凭据可能明文传输。
            </p>
          </template>
          <p v-if="proxy.draft.value.mode === 'proxy'" class="form-note">
            代理连接失败时不会自动直连。
          </p>
        </fieldset>
        <p class="form-note effect-note">
          仅对 Central 新发起的业务请求生效，不中断在途请求。SMTP
          不受本页规则或代理影响。
        </p>
        <p
          v-if="active.command.confirmed.value && !active.dirty.value"
          class="form-note form-success"
          role="status"
        >
          {{ section === "rules" ? "访问规则已保存。" : "代理设置已保存。" }}
        </p>
        <div
          v-if="active.command.uncertain.value"
          class="form-feedback"
          role="alert"
          tabindex="-1"
        >
          <p>
            {{
              active.command.committed.value
                ? "操作已提交，回执尚未读回。"
                : "原保存结果尚未确认。"
            }}
            检查登录状态后可重试原请求；读取当前配置不能确认原保存结果。
            刷新或关闭页面后将无法恢复此请求。
          </p>
          <div class="form-actions">
            <UiButton
              :loading="checking"
              :disabled="active.command.busy.value"
              @click="inspect"
              >检查当前状态</UiButton
            ><UiButton
              v-if="active.command.replayable.value"
              :loading="active.command.busy.value"
              :disabled="checking"
              @click="active.command.retry"
              >重试原请求</UiButton
            ><UiButton :disabled="commandBusy" @click="confirmation = 'abandon'"
              >放弃本次操作</UiButton
            >
          </div>
        </div>
        <div
          v-else-if="active.command.error.value"
          class="form-feedback"
          role="alert"
          tabindex="-1"
        >
          <p>
            {{
              active.conflict.value
                ? "配置已被其他管理员修改，你的草稿已保留。请读取最新配置并核对。"
                : accountErrorMessage(active.command.error.value)
            }}
          </p>
        </div>
        <div
          v-if="active.changed.value && !active.command.uncertain.value"
          class="form-feedback"
        >
          <p>
            已读取更新的配置（版本
            {{ active.latest.value?.version }}）。采用后会替换当前草稿。
          </p>
          <dl v-if="section === 'proxy'" class="latest-values">
            <dt>连接方式</dt>
            <dd>
              {{ proxy.latest.value?.mode === "proxy" ? "使用代理" : "直连" }}
            </dd>
            <dt>代理网关</dt>
            <dd>
              {{
                proxy.latest.value?.gateway
                  ? `${proxy.latest.value.gateway.protocol}://${proxy.latest.value.gateway.host}:${proxy.latest.value.gateway.port}`
                  : "未设置"
              }}
            </dd>
            <dt>用户名</dt>
            <dd>{{ proxy.latest.value?.gateway?.username || "未设置" }}</dd>
          </dl>
          <template v-else>
            <p class="latest-rule-mode">
              规则模式：{{
                rules.latest.value?.rule_mode === "allowlist"
                  ? "白名单"
                  : "黑名单"
              }}
            </p>
            <ul class="latest-rules">
              <li
                v-for="(rule, index) in rules.latest.value?.rules"
                :key="index"
              >
                {{ rule.target }} ·
                {{ rule.ports === "all" ? "全部端口" : rule.ports.join(", ") }}
              </li>
            </ul>
          </template>
          <UiButton :disabled="locked" @click="confirmation = 'adopt'"
            >采用最新配置</UiButton
          >
        </div>
        <div class="form-actions">
          <UiButton
            type="submit"
            variant="primary"
            :loading="active.command.busy.value"
            :disabled="locked || !active.dirty.value || active.conflict.value"
            >{{ section === "rules" ? "保存规则" : "保存代理设置" }}</UiButton
          ><UiButton
            :disabled="locked || !active.dirty.value"
            @click="confirmation = 'cancel'"
            >取消</UiButton
          >
        </div>
      </form>
    </UiTabs>
    <UiDialog
      :open="!!confirmation && access.allowed.value"
      :title="
        confirmation === 'abandon'
          ? '放弃追踪本次操作'
          : confirmation === 'remove'
            ? '移除代理身份认证'
            : confirmation === 'delete'
              ? '删除访问规则'
              : '放弃当前修改'
      "
      @update:open="!$event && (confirmation = undefined)"
    >
      <p class="confirmation-copy">
        {{
          confirmation === "abandon"
            ? "此前提交仍可能生效。这只停止追踪原请求，不会撤销服务端操作。"
            : confirmation === "remove"
              ? "用户名和密码将从草稿中清除，保存代理设置后生效。"
              : confirmation === "delete"
                ? "从草稿中删除此规则，保存访问规则后生效。"
                : "当前未保存的草稿会被替换。"
        }}
      </p>
      <template #footer
        ><UiButton @click="confirmation = undefined">继续编辑</UiButton
        ><UiButton variant="primary" @click="confirm">{{
          confirmation === "abandon"
            ? "放弃追踪"
            : confirmation === "remove"
              ? "移除身份认证"
              : confirmation === "delete"
                ? "删除规则"
                : "放弃修改"
        }}</UiButton></template
      >
    </UiDialog>
  </div>
</template>

<style scoped>
.outbound-page,
.outbound-form {
  display: flex;
  flex-direction: column;
  gap: 20px;
  min-width: 0;
}
.outbound-form {
  margin-top: 0;
}
.outbound-form fieldset {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
.rules-heading,
.credential-status {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.rule-mode {
  max-width: 560px;
}
.rule-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
  margin-top: -12px;
}
.rules-table {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  font-size: 13px;
}
.ports-column {
  width: 30%;
}
.action-column {
  width: 60px;
}
.rules-table th {
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 500;
  text-align: left;
}
.rules-table th,
.rules-table td {
  padding: 6px;
  border-bottom: 1px solid var(--border-subtle);
  vertical-align: top;
}
.rules-table th:first-child,
.rules-table td:first-child {
  padding-left: 0;
}
.rules-table th:last-child,
.rules-table td:last-child {
  padding-right: 0;
}
.rule-action {
  text-align: right;
}
.visually-hidden,
.rule-row :deep(.ui-field-label) {
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
.rules-table .empty-rules {
  padding: 20px 0;
  color: var(--text-secondary);
}
.latest-rule-mode {
  margin-top: 12px;
}
.proxy-fields {
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 560px;
}
.gateway-address {
  display: grid;
  align-items: start;
  grid-template-columns: minmax(0, 1fr) 120px;
  gap: 16px;
}
.effect-note {
  margin-top: 4px;
}
.latest-values {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px 20px;
  font-size: 13px;
}
.latest-values dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.latest-rules {
  padding-left: 20px;
  font-size: 13px;
  overflow-wrap: anywhere;
}
@media (max-width: 560px) {
  .gateway-address {
    grid-template-columns: minmax(0, 1fr) 100px;
    gap: 12px;
  }
  .credential-status {
    align-items: flex-start;
    flex-wrap: wrap;
  }
}
</style>
