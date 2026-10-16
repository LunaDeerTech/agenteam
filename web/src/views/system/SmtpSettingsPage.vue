<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  shallowRef,
  watch,
} from "vue";
import {
  UiButton,
  UiDialog,
  UiInput,
  UiSelect,
  UiTabs,
} from "../../components/ui";
import PageSizeSelect from "../../components/system/PageSizeSelect.vue";
import { isPageSize } from "../../system/pagination";
import { useLeaveProtection } from "../../account/leave";
import {
  AccountError,
  accountEnabled,
  accountErrorMessage,
} from "../../account/transport";
import { useSystemAccess } from "../../system/requests";
import {
  deliveryChannel,
  deliveryKinds,
  deliveryOutcome,
  deliveryPhases,
  deliveryReasons,
  getSMTPJob,
  getSMTPSettings,
  jobRetryIssue,
  listSMTPJobs,
  removeSMTPAuthentication,
  retrySMTPJob,
  saveSMTPSettings,
  settingsEmailIssue,
  smtpDraft,
  smtpIssues,
  testSMTP,
  unconfigureSMTP,
  useSettingsCommand,
  type JobAccepted,
  type JobRetryAccepted,
  type MailJobManagement,
  type SMTPDraft,
  type SMTPResult,
  type SMTPSettings,
} from "../../system/account-settings";

const access = useSystemAccess();
const pageElement = ref<HTMLElement | null>(null);
const section = ref("configuration");
const observation = shallowRef<SMTPSettings>();
const baseline = shallowRef<SMTPSettings>();
const draft = reactive(smtpDraft());
const reading = ref(false);
const readError = shallowRef<unknown>();
const submitted = ref(false);
const needsPassword = ref(false);
const configSave = useSettingsCommand<SMTPResult>();
const configAction = ref<"save" | "unconfigure" | "remove-auth">("save");
const recipient = ref("");
const acceptedRecipient = ref("");
const testSubmitted = ref(false);
const deliverySave = useSettingsCommand<JobAccepted | JobRetryAccepted>();
const deliveryAction = ref<"test" | "retry">("test");
const sourceJob = ref<string>();
const confirmedJob = shallowRef<{
  job_id: string;
  source?: string;
  kind: "test" | "retry";
}>();
const rows = shallowRef<MailJobManagement[]>([]);
const listReady = ref(false);
const listReading = ref(false);
const listRefreshing = ref(false);
const listError = shallowRef<unknown>();
const nextCursor = ref<string>();
const cursors = ref<(string | undefined)[]>([undefined]);
const page = ref(0);
const pageSize = ref(10);
let listTarget: { cursor?: string; page: number } = { page: 0 };
const detailId = ref<string>();
const detail = shallowRef<MailJobManagement>();
const detailReading = ref(false);
const detailError = shallowRef<unknown>();
const listHeading = ref<HTMLElement | null>(null);
const configFeedback = ref<HTMLElement | null>(null);
const checking = ref(false);
const confirmation = ref<
  | "discard"
  | "adopt"
  | "stop"
  | "abandon-config"
  | "abandon-delivery"
  | "switch"
  | "retry"
  | "remove-auth"
  | "close-detail"
>();
const nextSection = ref("");
const retryTarget = shallowRef<MailJobManagement>();
let alive = true;
let configRevision = 0;
let listRevision = 0;
let detailRevision = 0;
let detailOrigin: HTMLElement | null = null;
let detailScroll = 0;

const configDirty = computed(() => {
  if (!baseline.value) return false;
  if (needsPassword.value) return true;
  const initial = smtpDraft(baseline.value);
  return (Object.keys(initial) as (keyof SMTPDraft)[]).some(
    (key) => draft[key] !== initial[key],
  );
});
const deliveryDirty = computed(
  () =>
    (!!recipient.value && recipient.value !== acceptedRecipient.value) ||
    deliverySave.busy.value ||
    deliverySave.uncertain.value,
);
const configConflict = computed(
  () =>
    configSave.error.value instanceof AccountError &&
    configSave.error.value.code === "VERSION_CONFLICT",
);
const configLocked = computed(
  () =>
    !accountEnabled ||
    !access.allowed.value ||
    !baseline.value ||
    reading.value ||
    configSave.busy.value ||
    configSave.uncertain.value ||
    checking.value,
);
const anyBusy = computed(
  () =>
    reading.value ||
    listReading.value ||
    detailReading.value ||
    configSave.busy.value ||
    deliverySave.busy.value ||
    checking.value,
);
const deliveryLocked = computed(
  () =>
    !access.allowed.value ||
    !accountEnabled ||
    anyBusy.value ||
    deliverySave.uncertain.value,
);
const configCommitted = configSave.committed;
const deliveryCommitted = deliverySave.committed;
const issues = computed(() => {
  const value = smtpIssues(draft);
  if (draft.configured) {
    const hasCredential =
      !!draft.password || !!baseline.value?.credential_present;
    if (draft.username && !hasCredential)
      value.password = "认证账号需要同时配置密码。";
    if (needsPassword.value && !draft.password)
      value.password =
        "上次填写的新密码已从输入框清除，请重新输入。取消修改可继续保留原密码。";
    if (!draft.username && hasCredential)
      value.username =
        "保留或更新密码时需要填写认证账号。若要取消认证，请先取消修改，再使用移除已保存认证。";
  }
  return value;
});
const testIssue = computed(() =>
  testSubmitted.value ? settingsEmailIssue(recipient.value) : "",
);
const latestChanged = computed(
  () =>
    !!observation.value &&
    !!baseline.value &&
    observation.value.version !== baseline.value.version,
);
const selectedSection = computed({
  get: () => section.value,
  set: (value: string) => void requestSection(value),
});
const tabs = computed(() => [
  {
    value: "configuration",
    label: "配置",
    disabled: anyBusy.value && section.value !== "configuration",
  },
  {
    value: "delivery",
    label: "测试与任务",
    disabled: anyBusy.value && section.value !== "delivery",
  },
]);
const encryptionOptions = [
  { value: "tls", label: "TLS" },
  { value: "starttls", label: "STARTTLS" },
  { value: "none", label: "不加密" },
];

function fieldIssue(key: string) {
  if (submitted.value && issues.value[key]) return issues.value[key];
  return configSave.error.value instanceof AccountError &&
    configSave.error.value.fields.some((field) => field.path === `/${key}`)
    ? "请检查此项内容后再试。"
    : "";
}
async function focusInvalid() {
  await nextTick();
  if (alive && access.allowed.value)
    pageElement.value
      ?.querySelector<HTMLElement>('[aria-invalid="true"]')
      ?.focus();
}
function adopt(value: SMTPSettings) {
  baseline.value = value;
  Object.assign(draft, smtpDraft(value));
  submitted.value = false;
  needsPassword.value = false;
}
async function readConfig(replace = false) {
  if (reading.value || !access.allowed.value || !accountEnabled) return;
  const revision = ++configRevision;
  const generation = access.generation.value;
  const current = () =>
    alive &&
    revision === configRevision &&
    generation === access.generation.value;
  reading.value = true;
  readError.value = undefined;
  try {
    const value = await getSMTPSettings({ current });
    if (!current()) return;
    if (baseline.value && baseline.value.id !== value.id)
      throw new AccountError("INVALID_RESPONSE");
    if (
      !observation.value ||
      BigInt(value.version) >= BigInt(observation.value.version)
    )
      observation.value = value;
    if (!baseline.value || replace) adopt(observation.value!);
  } catch (error) {
    if (current()) readError.value = error;
  } finally {
    if (revision === configRevision) reading.value = false;
  }
}
async function saveConfiguration(stop = false) {
  if (configLocked.value || configConflict.value || !baseline.value) return;
  submitted.value = true;
  const policy = {
    auto_retry_count: draft.auto_retry_count,
    retry_interval_seconds: draft.retry_interval_seconds,
  };
  const invalid = stop
    ? smtpIssues({ ...smtpDraft(), ...policy }).auto_retry_count ||
      smtpIssues({ ...smtpDraft(), ...policy }).retry_interval_seconds
    : Object.keys(issues.value).length;
  if (invalid) {
    await focusInvalid();
    return;
  }
  const base = baseline.value;
  const generation = access.generation.value;
  configAction.value = stop ? "unconfigure" : "save";
  await configSave.submit(
    () => {
      const options = {
        current: () => alive && generation === access.generation.value,
      };
      const command =
        stop || !draft.configured
          ? unconfigureSMTP(base, policy, options)
          : saveSMTPSettings(base, draft, options);
      // Keep replacement material only in the command's frozen body after dispatch.
      needsPassword.value = !stop && !!draft.password;
      draft.password = "";
      return command;
    },
    (value) => {
      if (
        !observation.value ||
        BigInt(value.settings.version) >= BigInt(observation.value.version)
      )
        observation.value = value.settings;
      adopt(observation.value!);
    },
  );
  await nextTick();
  if (
    alive &&
    generation === access.generation.value &&
    access.allowed.value &&
    section.value === "configuration" &&
    configSave.error.value &&
    !configSave.uncertain.value
  )
    configFeedback.value?.focus();
}
async function removeAuthentication() {
  if (
    configLocked.value ||
    configDirty.value ||
    configConflict.value ||
    !baseline.value?.credential_present
  )
    return;
  const base = baseline.value;
  const generation = access.generation.value;
  configAction.value = "remove-auth";
  await configSave.submit(
    () =>
      removeSMTPAuthentication(base, {
        current: () => alive && generation === access.generation.value,
      }),
    (value) => {
      if (
        !observation.value ||
        BigInt(value.settings.version) >= BigInt(observation.value.version)
      )
        observation.value = value.settings;
      adopt(observation.value!);
    },
  );
}
async function loadJobs(cursor?: string, targetPage = 0, preserve = false) {
  if (
    !access.allowed.value ||
    !accountEnabled ||
    listReading.value ||
    listRefreshing.value
  )
    return;
  const revision = ++listRevision;
  const limit = pageSize.value;
  const generation = access.generation.value;
  const focused = document.activeElement;
  const restoreFocus =
    !preserve &&
    focused instanceof HTMLElement &&
    focused.matches("button") &&
    !!focused.closest(".job-list");
  const current = () =>
    alive &&
    revision === listRevision &&
    generation === access.generation.value &&
    section.value === "delivery";
  listTarget = { cursor, page: targetPage };
  if (preserve) listRefreshing.value = true;
  else listReading.value = true;
  listError.value = undefined;
  if (!preserve) {
    listReady.value = false;
    rows.value = [];
    nextCursor.value = undefined;
  }
  try {
    const value = await listSMTPJobs(cursor, { current }, limit);
    if (!current()) return;
    rows.value = value.items;
    nextCursor.value = value.next_cursor;
    cursors.value = [...cursors.value.slice(0, targetPage), cursor];
    page.value = targetPage;
    listReady.value = true;
  } catch (error) {
    if (current()) listError.value = error;
  } finally {
    if (revision === listRevision) {
      listReading.value = false;
      listRefreshing.value = false;
    }
    if (current() && restoreFocus) {
      await nextTick();
      if (current()) listHeading.value?.focus({ preventScroll: true });
    }
  }
}
async function changePageSize(value: number) {
  if (!isPageSize(value) || value === pageSize.value || !access.allowed.value)
    return;
  listRevision++;
  listReading.value = false;
  listRefreshing.value = false;
  listReady.value = false;
  rows.value = [];
  nextCursor.value = undefined;
  cursors.value = [undefined];
  page.value = 0;
  pageSize.value = value;
  listError.value = undefined;
  listTarget = { page: 0 };
  await loadJobs();
}
function captureDetailOrigin() {
  detailOrigin =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  detailScroll =
    pageElement.value?.closest<HTMLElement>(".settings-main")?.scrollTop ?? 0;
}
async function openJob(jobId: string, captureOrigin = true) {
  if (!access.allowed.value || detailReading.value) return;
  if (!detailId.value && captureOrigin) captureDetailOrigin();
  const revision = ++detailRevision;
  const generation = access.generation.value;
  const previous = detail.value?.job_id === jobId ? detail.value : undefined;
  const current = () =>
    alive &&
    revision === detailRevision &&
    generation === access.generation.value &&
    section.value === "delivery";
  detailId.value = jobId;
  detail.value = undefined;
  detailReading.value = true;
  detailError.value = undefined;
  try {
    const value = await getSMTPJob(jobId, { current });
    if (!current()) return;
    detail.value =
      previous && BigInt(previous.version) > BigInt(value.version)
        ? previous
        : value;
    rows.value = rows.value.map((job) =>
      job.job_id === value.job_id &&
      BigInt(value.version) >= BigInt(job.version)
        ? value
        : job,
    );
  } catch (error) {
    if (current()) detailError.value = error;
  } finally {
    if (revision === detailRevision) detailReading.value = false;
  }
}
async function closeDetail() {
  const revision = ++detailRevision;
  const origin = detailOrigin;
  const scroll = detailScroll;
  detailOrigin = null;
  detailReading.value = false;
  detailId.value = undefined;
  detail.value = undefined;
  detailError.value = undefined;
  await nextTick();
  // UiDialog closes its native dialog after its own nextTick. Restore only
  // after that close has released the modal focus trap.
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
  if (
    alive &&
    revision === detailRevision &&
    access.allowed.value &&
    section.value === "delivery" &&
    !detailId.value
  ) {
    const main = pageElement.value?.closest<HTMLElement>(".settings-main");
    if (main) main.scrollTop = scroll;
    if (origin?.isConnected && !origin.matches(":disabled"))
      origin.focus({ preventScroll: true });
  }
}
function requestDetailClose() {
  if (deliverySave.busy.value) return;
  if (deliverySave.uncertain.value) confirmation.value = "close-detail";
  else void closeDetail();
}
async function showAcceptedJob(jobId: string) {
  const generation = access.generation.value;
  await openJob(jobId, false);
  if (
    alive &&
    generation === access.generation.value &&
    section.value === "delivery"
  )
    await loadJobs(cursors.value[page.value], page.value, true);
}
async function sendTest() {
  if (deliveryLocked.value || !observation.value?.configured) return;
  testSubmitted.value = true;
  if (settingsEmailIssue(recipient.value)) {
    await focusInvalid();
    return;
  }
  const generation = access.generation.value;
  const submittedRecipient = recipient.value;
  captureDetailOrigin();
  deliveryAction.value = "test";
  sourceJob.value = undefined;
  confirmedJob.value = undefined;
  await deliverySave.submit(
    () =>
      testSMTP(submittedRecipient, {
        current: () => alive && generation === access.generation.value,
      }),
    (value) => {
      confirmedJob.value = { job_id: value.job_id, kind: "test" };
      acceptedRecipient.value = submittedRecipient;
      testSubmitted.value = false;
      void showAcceptedJob(value.job_id);
    },
  );
}
async function retryJob() {
  const target = retryTarget.value;
  confirmation.value = undefined;
  retryTarget.value = undefined;
  if (!target || deliveryLocked.value || jobRetryIssue(target)) return;
  const generation = access.generation.value;
  deliveryAction.value = "retry";
  sourceJob.value = target.job_id;
  confirmedJob.value = undefined;
  await deliverySave.submit(
    () =>
      retrySMTPJob(target, {
        current: () => alive && generation === access.generation.value,
      }),
    (value) => {
      confirmedJob.value = {
        job_id: value.job_id,
        source: target.job_id,
        kind: "retry",
      };
      void showAcceptedJob(value.job_id);
    },
  );
}
async function inspectConfiguration() {
  if (anyBusy.value) return;
  checking.value = true;
  try {
    if (configSave.uncertain.value) {
      if (!(await configSave.verify())) return;
    } else await access.recheck();
    if (access.allowed.value) await readConfig();
  } finally {
    checking.value = false;
  }
}
async function inspectDelivery() {
  if (anyBusy.value) return;
  checking.value = true;
  try {
    if (!(await deliverySave.verify())) return;
    if (sourceJob.value) await openJob(sourceJob.value);
    else {
      await loadJobs(cursors.value[page.value], page.value, true);
    }
  } finally {
    checking.value = false;
  }
}
function clearDelivery() {
  listRevision++;
  detailRevision++;
  listReading.value = false;
  listRefreshing.value = false;
  detailReading.value = false;
  listReady.value = false;
  rows.value = [];
  nextCursor.value = undefined;
  cursors.value = [undefined];
  page.value = 0;
  pageSize.value = 10;
  detailId.value = undefined;
  detail.value = undefined;
  listError.value = undefined;
  detailError.value = undefined;
  recipient.value = "";
  acceptedRecipient.value = "";
  detailOrigin = null;
  detailScroll = 0;
  testSubmitted.value = false;
  sourceJob.value = undefined;
  confirmedJob.value = undefined;
  retryTarget.value = undefined;
  deliverySave.abandon();
}
async function switchSection(value: string) {
  if (value === section.value || !["configuration", "delivery"].includes(value))
    return;
  if (section.value === "configuration") {
    configRevision++;
    configSave.abandon();
    if (observation.value) adopt(observation.value);
    else {
      baseline.value = undefined;
      Object.assign(draft, smtpDraft());
    }
  } else clearDelivery();
  section.value = value;
  if (value === "delivery") await loadJobs();
  else if (!baseline.value) await readConfig();
}
async function requestSection(value: string) {
  if (anyBusy.value || value === section.value) return;
  if (
    section.value === "configuration"
      ? configDirty.value || configSave.uncertain.value
      : deliveryDirty.value
  ) {
    nextSection.value = value;
    confirmation.value = "switch";
    return;
  }
  await switchSection(value);
}
async function confirmAction() {
  const action = confirmation.value;
  confirmation.value = undefined;
  if (!access.allowed.value || anyBusy.value) return;
  if (action === "retry") {
    await retryJob();
    return;
  }
  if (action === "stop") {
    await saveConfiguration(true);
    return;
  }
  if (action === "remove-auth") {
    await removeAuthentication();
    return;
  }
  if (action === "switch") {
    await switchSection(nextSection.value);
    return;
  }
  if (action === "close-detail") {
    deliverySave.abandon();
    sourceJob.value = undefined;
    await closeDetail();
    return;
  }
  if (action === "abandon-config") {
    configSave.abandon();
    baseline.value = undefined;
    Object.assign(draft, smtpDraft());
    await readConfig(true);
  } else if (action === "abandon-delivery") {
    deliverySave.abandon();
    sourceJob.value = undefined;
  } else if (action === "adopt" && observation.value) {
    configSave.abandon();
    adopt(observation.value);
  } else if (action === "discard" && baseline.value) {
    configSave.abandon();
    adopt(baseline.value);
  }
}
const confirmationTitle = computed(() =>
  confirmation.value === "close-detail"
    ? "关闭并放弃追踪"
    : confirmation.value === "stop"
      ? "停用 SMTP"
      : confirmation.value === "remove-auth"
        ? "移除已保存的认证"
        : confirmation.value === "retry"
          ? "申请新的投递重试"
          : confirmation.value?.startsWith("abandon")
            ? "放弃追踪本次操作"
            : "放弃当前修改",
);
const confirmationCopy = computed(() => {
  if (
    confirmation.value === "close-detail" ||
    confirmation.value === "abandon-delivery"
  )
    return "此前提交仍可能生效。放弃追踪会清除原请求的恢复材料，不会撤销服务端操作或邮件发送。";
  if (confirmation.value === "stop")
    return "停用会清除已保存的连接配置和认证凭据，保留当前填写的重试策略。未保存的其他字段会被放弃；在途邮件不会因此撤回。";
  if (confirmation.value === "retry")
    return "使用当前合法配置和原业务材料申请新的投递周期，可能重复投递。原邀请或重置链接不会续期，是否接受由服务核实。";
  if (confirmation.value === "remove-auth")
    return "已保存的认证账号与密码将一并清除。SMTP 连接与发件人信息保持；邮件服务器需要允许不认证发送。";
  if (
    confirmation.value?.startsWith("abandon") ||
    (confirmation.value === "switch" &&
      (configSave.uncertain.value || deliverySave.uncertain.value))
  )
    return "此前提交仍可能生效。这只停止追踪原请求，不会撤销服务端操作或邮件发送；未保存的草稿也会清除。";
  return "当前未保存的草稿会被清除。";
});
function reset() {
  configRevision++;
  reading.value = false;
  observation.value = undefined;
  baseline.value = undefined;
  readError.value = undefined;
  Object.assign(draft, smtpDraft());
  submitted.value = false;
  needsPassword.value = false;
  confirmation.value = undefined;
  clearDelivery();
  section.value = "configuration";
}
watch(access.generation, reset, { flush: "sync" });
watch(access.allowed, (allowed) => {
  if (allowed && !baseline.value && !reading.value) void readConfig();
});
onMounted(() => void readConfig());
onBeforeUnmount(() => {
  alive = false;
  configRevision++;
  listRevision++;
  detailRevision++;
  draft.password = "";
});
useLeaveProtection(
  () =>
    configDirty.value ||
    configSave.busy.value ||
    configSave.uncertain.value ||
    deliveryDirty.value,
);
</script>

<template>
  <div ref="pageElement" class="smtp-page">
    <p v-if="!accountEnabled" class="form-feedback">
      当前为界面预览，连接账号服务后可管理 SMTP。
    </p>
    <div v-else-if="access.denied.value" class="form-feedback" role="alert">
      <p>你目前没有系统管理权限。</p>
      <UiButton :loading="checking" @click="inspectConfiguration"
        >重新检查权限</UiButton
      >
    </div>
    <UiTabs
      v-else
      v-model="selectedSection"
      label="SMTP 设置分区"
      :items="tabs"
    >
      <div v-if="section === 'configuration'" class="account-form smtp-form">
        <p v-if="reading && !baseline" class="form-note" role="status">
          正在读取 SMTP 配置…
        </p>
        <div v-if="readError" class="form-feedback" role="alert">
          <p>配置读取失败。{{ accountErrorMessage(readError) }}</p>
          <UiButton
            :loading="reading"
            :disabled="configSave.busy.value"
            @click="readConfig()"
            >重新读取配置</UiButton
          >
        </div>
        <form
          v-if="baseline"
          class="account-form smtp-form"
          @submit.prevent="saveConfiguration()"
        >
          <fieldset :disabled="configLocked">
            <div v-if="!draft.configured" class="unconfigured">
              <h2>尚未配置 SMTP</h2>
              <p class="form-note">
                邀请与密码恢复使用受限后台日志，由获授权运维协助转交。页面不提供恢复链接。
              </p>
              <UiButton @click="draft.configured = true">配置 SMTP</UiButton>
            </div>
            <template v-else>
              <section class="form-section" aria-labelledby="smtp-connection">
                <h2 id="smtp-connection">服务连接</h2>
                <UiInput
                  v-model="draft.host"
                  label="主机"
                  placeholder="smtp.example.com"
                  :error="fieldIssue('host')"
                  autocomplete="off"
                />
                <div class="smtp-pair">
                  <UiInput
                    v-model="draft.port"
                    label="端口"
                    inputmode="numeric"
                    :error="fieldIssue('port')"
                    autocomplete="off"
                  />
                  <UiSelect
                    v-model="draft.encryption"
                    label="加密方式"
                    :options="encryptionOptions"
                    :disabled="configLocked"
                    :error="fieldIssue('encryption')"
                  />
                </div>
              </section>
              <section
                class="form-section"
                aria-labelledby="smtp-authentication"
              >
                <h2 id="smtp-authentication">发件账号</h2>
                <UiInput
                  v-model="draft.username"
                  label="认证账号"
                  hint="不使用认证时，账号与凭据需同时为空。"
                  :error="fieldIssue('username')"
                  autocomplete="off"
                />
                <UiInput
                  v-model="draft.password"
                  label="密码"
                  type="password"
                  autocomplete="new-password"
                  :error="fieldIssue('password')"
                  :hint="
                    baseline.credential_present
                      ? '留空保持已保存的密码，填写新值则更新。已保存的密码不会回显。'
                      : '尚未保存密码。需要认证时，请同时填写账号与密码。'
                  "
                />
                <UiInput
                  v-model="draft.sender_email"
                  label="发件人邮箱"
                  inputmode="email"
                  :error="fieldIssue('sender_email')"
                  autocomplete="off"
                />
                <UiInput
                  v-model="draft.sender_name"
                  label="发件人显示名"
                  hint="可留空，留空时使用邮箱。"
                  :error="fieldIssue('sender_name')"
                  autocomplete="off"
                />
                <div
                  v-if="baseline.credential_present"
                  class="saved-auth-action"
                >
                  <UiButton
                    variant="ghost"
                    :disabled="configLocked || configDirty || configConflict"
                    @click="confirmation = 'remove-auth'"
                    >移除已保存的认证</UiButton
                  >
                  <p v-if="configDirty" class="form-note">
                    需要移除认证时，请先保存或取消当前修改。
                  </p>
                </div>
              </section>
            </template>
            <section class="form-section" aria-labelledby="smtp-retry-policy">
              <h2 id="smtp-retry-policy">自动重试</h2>
              <p class="form-note">
                次数指首次尝试之外的自动重试，0 表示不自动重试。
              </p>
              <div class="smtp-pair">
                <UiInput
                  v-model="draft.auto_retry_count"
                  label="自动重试次数"
                  hint="0–5 次"
                  inputmode="numeric"
                  :error="fieldIssue('auto_retry_count')"
                  autocomplete="off"
                />
                <UiInput
                  v-model="draft.retry_interval_seconds"
                  label="重试间隔（秒）"
                  hint="10–3600 秒"
                  inputmode="numeric"
                  :error="fieldIssue('retry_interval_seconds')"
                  autocomplete="off"
                />
              </div>
            </section>
          </fieldset>
          <p
            v-if="configSave.confirmed.value && !configDirty"
            class="form-note form-success"
            role="status"
          >
            {{
              configAction === "unconfigure"
                ? "停用操作已确认。下方编辑基于当前配置。"
                : configAction === "remove-auth"
                  ? "已保存的认证账号与密码已移除。"
                  : "SMTP 配置已保存。保存成功不代表邮件已送达。"
            }}
          </p>
          <div
            v-if="configSave.uncertain.value"
            class="form-feedback"
            role="alert"
          >
            <p>
              {{
                configCommitted
                  ? "操作已提交，回执尚未读回。"
                  : "原配置操作结果尚未确认。"
              }}
              原请求已保留，读取当前配置不能代替原请求回执。
            </p>
            <div class="form-actions">
              <UiButton
                :loading="checking"
                :disabled="anyBusy"
                @click="inspectConfiguration"
                >检查当前状态</UiButton
              >
              <UiButton
                v-if="configSave.replayable.value"
                :loading="configSave.busy.value"
                :disabled="reading || checking"
                @click="configSave.retry"
                >重试原请求</UiButton
              >
              <UiButton
                :disabled="anyBusy"
                @click="confirmation = 'abandon-config'"
                >放弃本次操作</UiButton
              >
            </div>
          </div>
          <div
            v-else-if="configSave.error.value"
            ref="configFeedback"
            class="form-feedback"
            role="alert"
            tabindex="-1"
          >
            <p>
              {{
                configConflict
                  ? "配置已发生变化，非密码字段的草稿已保留。请读取最新配置并核对，再明确采用。"
                  : accountErrorMessage(configSave.error.value)
              }}
            </p>
            <p v-if="needsPassword">
              新密码已从输入框清除。继续保存前需重新输入，取消修改会保留原密码。
            </p>
          </div>
          <div
            v-if="latestChanged && !configSave.uncertain.value"
            class="form-feedback"
          >
            <p>
              已读取较新的配置：{{
                observation?.configured
                  ? `${observation.host}:${observation.port} · ${observation.sender_email}`
                  : "SMTP 未配置"
              }}。采用后会替换草稿与未保存的密码。
            </p>
            <UiButton :disabled="configLocked" @click="confirmation = 'adopt'"
              >采用最新配置</UiButton
            >
          </div>
          <div class="form-actions">
            <UiButton
              variant="primary"
              type="submit"
              :loading="configSave.busy.value"
              :disabled="configLocked || !configDirty || configConflict"
              >保存配置</UiButton
            >
            <UiButton
              :disabled="configLocked || !configDirty"
              @click="confirmation = 'discard'"
              >取消</UiButton
            >
            <UiButton
              variant="ghost"
              :loading="reading"
              :disabled="
                configSave.busy.value || configSave.uncertain.value || checking
              "
              @click="readConfig()"
              >读取最新配置</UiButton
            >
          </div>
          <section v-if="baseline.configured" class="form-section smtp-stop">
            <h2>停用邮件传输</h2>
            <p class="form-note">
              清除 SMTP
              连接和凭据，之后的恢复信息使用受限后台日志。此操作不会撤回已发送或在途邮件。
            </p>
            <UiButton
              variant="danger"
              :disabled="configLocked || configConflict"
              @click="confirmation = 'stop'"
              >停用 SMTP</UiButton
            >
          </section>
        </form>
      </div>
      <div v-else class="delivery-page">
        <form
          class="account-form smtp-form test-form"
          @submit.prevent="sendTest"
        >
          <h2>测试邮件</h2>
          <p v-if="!observation?.configured" class="form-note">
            当前尚未确认可用的已配置 SMTP，请返回配置区读取或保存。
          </p>
          <UiInput
            v-model="recipient"
            label="测试收件邮箱"
            inputmode="email"
            autocomplete="off"
            :disabled="deliveryLocked || !observation?.configured"
            :error="testIssue"
          />
          <div class="form-actions">
            <UiButton
              type="submit"
              variant="primary"
              :loading="deliverySave.busy.value && deliveryAction === 'test'"
              :disabled="
                deliveryLocked || !observation?.configured || !recipient
              "
              >请求测试发送</UiButton
            >
            <UiButton
              v-if="confirmedJob"
              :disabled="anyBusy"
              @click="openJob(confirmedJob.job_id)"
              >查看已受理任务</UiButton
            >
          </div>
        </form>
        <div
          v-if="!detailId && deliverySave.uncertain.value"
          class="form-feedback"
          role="alert"
        >
          <p>
            {{
              deliveryCommitted
                ? "操作已提交，回执尚未读回。"
                : "原请求结果尚未确认。"
            }}
            查看任务不能确认原请求是否成功；不要另发一个新请求。
          </p>
          <div class="form-actions">
            <UiButton
              :loading="checking"
              :disabled="anyBusy"
              @click="inspectDelivery"
              >检查当前状态</UiButton
            >
            <UiButton
              v-if="deliverySave.replayable.value"
              :loading="deliverySave.busy.value"
              :disabled="listReading || detailReading || checking"
              @click="deliverySave.retry"
              >重试原请求</UiButton
            >
            <UiButton
              :disabled="anyBusy"
              @click="confirmation = 'abandon-delivery'"
              >放弃本次操作</UiButton
            >
          </div>
        </div>
        <div
          v-else-if="!detailId && deliverySave.error.value"
          class="form-feedback"
          role="alert"
        >
          <p>{{ accountErrorMessage(deliverySave.error.value) }}</p>
        </div>
        <section aria-labelledby="smtp-job-list" class="job-list">
          <div class="section-toolbar">
            <h2 id="smtp-job-list" ref="listHeading" tabindex="-1">投递任务</h2>
            <UiButton
              :loading="listReading || listRefreshing"
              :disabled="anyBusy || listRefreshing"
              @click="loadJobs()"
              >刷新任务</UiButton
            >
          </div>
          <p v-if="listReading" class="form-note" role="status">
            正在读取投递任务…
          </p>
          <div v-if="listError" class="form-feedback" role="alert">
            <p>{{ accountErrorMessage(listError) }}</p>
            <UiButton
              :disabled="anyBusy"
              @click="
                listError instanceof AccountError &&
                listError.code === 'CURSOR_INVALID'
                  ? loadJobs()
                  : loadJobs(listTarget.cursor, listTarget.page)
              "
              >{{
                listError instanceof AccountError &&
                listError.code === "CURSOR_INVALID"
                  ? "返回首页重新加载"
                  : "重新读取任务列表"
              }}</UiButton
            >
          </div>
          <p v-if="listReady && !rows.length" class="form-note">
            暂无投递任务。
          </p>
          <ul v-if="rows.length" class="task-rows">
            <li v-for="job in rows" :key="job.job_id">
              <div class="task-summary">
                <strong>{{ deliveryKinds[job.kind] }}</strong
                ><span>{{ deliveryPhases[job.phase] }}</span
                ><small
                  >{{ deliveryChannel(job.attempt_channel) }} ·
                  {{
                    deliveryOutcome(job.attempt_result, job.attempt_channel)
                  }}</small
                ><time :datetime="job.created_at" :title="job.created_at"
                  >{{ job.created_at.slice(0, 19).replace("T", " ") }} UTC</time
                >
              </div>
              <UiButton
                :disabled="anyBusy"
                :aria-label="`查看${deliveryKinds[job.kind]}任务 ${job.job_id}`"
                @click="openJob(job.job_id)"
                >详情</UiButton
              >
            </li>
          </ul>
          <div class="pagination">
            <div class="pagination-status">
              <span class="form-note" role="status">{{
                listReady
                  ? `第 ${page + 1} 页 · 本页 ${rows.length} 项`
                  : listReading
                    ? "正在读取任务…"
                    : "尚未读取任务"
              }}</span>
              <PageSizeSelect
                :model-value="pageSize"
                :disabled="anyBusy"
                @update:model-value="changePageSize"
              />
            </div>
            <div class="form-actions">
              <UiButton
                :disabled="
                  anyBusy || listRefreshing || !listReady || page === 0
                "
                @click="loadJobs(cursors[page - 1], page - 1)"
                >上一页</UiButton
              ><UiButton
                :disabled="
                  anyBusy || listRefreshing || !listReady || !nextCursor
                "
                @click="loadJobs(nextCursor, page + 1)"
                >下一页</UiButton
              >
            </div>
          </div>
        </section>
      </div>
    </UiTabs>
    <UiDialog
      :open="!!detailId && access.allowed.value"
      title="投递任务详情"
      @update:open="!$event && requestDetailClose()"
    >
      <div class="job-detail">
        <p
          v-if="confirmedJob && confirmedJob.job_id === detailId"
          class="form-note form-success"
          role="status"
        >
          {{
            confirmedJob?.kind === "test"
              ? "测试请求已受理。"
              : "新的投递重试已受理。"
          }}
        </p>
        <p v-if="deliverySave.busy.value" class="form-note" role="status">
          正在提交，请稍候…
        </p>
        <div
          v-if="deliverySave.uncertain.value"
          class="form-feedback"
          role="alert"
        >
          <p>
            {{
              deliveryCommitted
                ? "操作已提交，回执尚未读回。"
                : "原请求结果尚未确认。"
            }}
            查看任务不能确认原请求是否成功；不要另发一个新请求。
          </p>
          <div class="form-actions">
            <UiButton
              :loading="checking"
              :disabled="anyBusy"
              @click="inspectDelivery"
              >检查当前状态</UiButton
            >
            <UiButton
              v-if="deliverySave.replayable.value"
              :loading="deliverySave.busy.value"
              :disabled="listReading || detailReading || checking"
              @click="deliverySave.retry"
              >重试原请求</UiButton
            >
            <UiButton
              :disabled="anyBusy"
              @click="confirmation = 'abandon-delivery'"
              >放弃本次操作</UiButton
            >
          </div>
        </div>
        <div
          v-else-if="deliverySave.error.value"
          class="form-feedback"
          role="alert"
        >
          <p>{{ accountErrorMessage(deliverySave.error.value) }}</p>
        </div>

        <p v-if="detailReading" class="form-note" role="status">
          正在读取任务…
        </p>
        <div v-if="detailError" class="form-feedback" role="alert">
          <p>
            任务读取失败，已受理的请求不会重新发送。{{
              accountErrorMessage(detailError)
            }}
          </p>
          <p class="job-reference">任务 {{ detailId }}</p>
        </div>
        <template v-if="detail">
          <dl class="job-facts">
            <dt>任务类型</dt>
            <dd>{{ deliveryKinds[detail.kind] }}</dd>
            <dt>当前阶段</dt>
            <dd>{{ deliveryPhases[detail.phase] }}</dd>
            <dt>尝试次数</dt>
            <dd>{{ detail.attempts }} 次</dd>
            <dt>实际尝试渠道</dt>
            <dd>{{ deliveryChannel(detail.attempt_channel) }}</dd>
            <dt>当前尝试结果</dt>
            <dd>
              {{
                deliveryOutcome(detail.attempt_result, detail.attempt_channel)
              }}
            </dd>
            <template v-if="detail.reason"
              ><dt>原因</dt>
              <dd>{{ deliveryReasons[detail.reason] }}</dd></template
            >
            <dt>创建时间（UTC）</dt>
            <dd>
              <time :datetime="detail.created_at" :title="detail.created_at">{{
                detail.created_at.slice(0, 19).replace("T", " ")
              }}</time>
            </dd>
            <dt>任务 ID</dt>
            <dd class="job-reference">{{ detail.job_id }}</dd>
            <template
              v-if="
                confirmedJob &&
                confirmedJob.job_id === detailId &&
                confirmedJob.source
              "
              ><dt>源任务 ID</dt>
              <dd class="job-reference">{{ confirmedJob.source }}</dd></template
            >
          </dl>
          <p v-if="jobRetryIssue(detail)" class="form-note">
            {{ jobRetryIssue(detail) }}
          </p>
        </template>
      </div>
      <template #footer>
        <UiButton
          :disabled="deliverySave.busy.value"
          @click="requestDetailClose"
          >关闭详情</UiButton
        >
        <UiButton
          :loading="detailReading"
          :disabled="anyBusy"
          @click="detailId && openJob(detailId, false)"
          >重新读取任务</UiButton
        >
        <UiButton
          v-if="detail"
          variant="primary"
          :disabled="deliveryLocked || !!jobRetryIssue(detail)"
          @click="
            retryTarget = { ...detail };
            confirmation = 'retry';
          "
          >申请重试投递</UiButton
        >
      </template>
    </UiDialog>
    <UiDialog
      :open="!!confirmation && access.allowed.value"
      :title="confirmationTitle"
      @update:open="!$event && (confirmation = undefined)"
    >
      <p class="confirmation-copy">{{ confirmationCopy }}</p>
      <p
        v-if="confirmation === 'retry' && retryTarget"
        class="job-reference confirmation-copy"
      >
        源任务 {{ retryTarget.job_id }}
      </p>
      <template #footer>
        <UiButton @click="confirmation = undefined">{{
          confirmation === "close-detail"
            ? "继续追踪"
            : confirmation === "stop" ||
                confirmation === "retry" ||
                confirmation === "remove-auth"
              ? "取消"
              : "继续编辑"
        }}</UiButton>
        <UiButton
          :variant="
            confirmation === 'stop' || confirmation === 'remove-auth'
              ? 'danger'
              : 'primary'
          "
          @click="confirmAction"
          >{{
            confirmation === "close-detail"
              ? "放弃追踪并关闭"
              : confirmation === "stop"
                ? "确认停用"
                : confirmation === "remove-auth"
                  ? "确认移除认证"
                  : confirmation === "retry"
                    ? "申请重试"
                    : confirmation?.startsWith("abandon")
                      ? "放弃追踪"
                      : "放弃修改"
          }}</UiButton
        >
      </template>
    </UiDialog>
  </div>
</template>

<style scoped>
.smtp-page,
.delivery-page {
  display: grid;
  gap: 24px;
  min-width: 0;
}
.smtp-form {
  max-width: 560px;
}
h2 {
  font-size: 14px;
  font-weight: 600;
}
.smtp-pair {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 18px;
}
.unconfigured {
  display: grid;
  justify-items: start;
  gap: 14px;
}
.smtp-stop {
  justify-items: start;
}
.test-form {
  padding-bottom: 24px;
  border-bottom: 1px solid var(--border-subtle);
}
.job-list,
.job-detail {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.section-toolbar,
.pagination,
.pagination-status {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.pagination {
  justify-content: flex-end;
}
.pagination-status {
  width: 100%;
}
.job-reference {
  font-family: var(--font-mono);
  font-size: 11px;
  overflow-wrap: anywhere;
}
.form-feedback .job-reference {
  margin-top: 6px;
}
.form-feedback > .ui-button {
  margin-top: 12px;
}
.job-facts {
  display: grid;
  grid-template-columns: 130px minmax(0, 1fr);
  gap: 12px 24px;
  font-size: 12px;
  line-height: 1.7;
}
.job-facts dt {
  color: var(--text-secondary);
}
.job-facts dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.job-facts small {
  display: block;
  margin-top: 4px;
  color: var(--text-secondary);
  font-size: 11px;
}
.task-rows {
  list-style: none;
  padding: 0;
  margin: 0;
}
.task-rows li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 16px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.task-summary {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 6px 12px;
  min-width: 0;
  font-size: 12px;
}
.task-summary strong {
  font-weight: 600;
}
.task-summary small,
.task-summary time {
  color: var(--text-secondary);
  font-size: 11px;
  grid-column: 1 / -1;
  overflow-wrap: anywhere;
}
.confirmation-copy {
  font-size: 13px;
  line-height: 1.7;
  color: var(--text-secondary);
}
@media (max-width: 520px) {
  .smtp-pair {
    grid-template-columns: minmax(0, 1fr);
    gap: 20px;
  }
  .job-facts {
    grid-template-columns: minmax(0, 1fr);
    gap: 5px;
  }
  .job-facts dd {
    margin-bottom: 9px;
  }
  .section-toolbar {
    align-items: flex-start;
  }
}
</style>
