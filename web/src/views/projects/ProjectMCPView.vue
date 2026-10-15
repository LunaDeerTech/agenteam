<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, shallowRef, watch } from 'vue'
import {
  UiBadge,
  UiButton,
  UiDialog,
  UiField,
  UiInput,
  UiSelect,
  UiState,
  UiTextarea,
} from '../../components/ui'
import { AccountFailure } from '../../api/client'
import type {
  MCPAuthentication,
  MCPConfig,
  MCPConnection,
  MCPResource,
  MCPTool,
} from '../../api/project-mcp'
import type { MCPMutation } from '../../composables/mcp-session'
import { useSession } from '../../composables/useSession'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'

const auth = useSession(),
  workspace = useProjectWorkspace()
const heading = ref<HTMLElement | null>(null)
const configs = shallowRef<readonly MCPConfig[]>([]),
  connections = shallowRef<readonly MCPConnection[]>([])
const tools = shallowRef<readonly MCPTool[]>([]),
  resources = shallowRef<readonly MCPResource[]>([])
const selected = ref(''),
  loading = ref(false),
  catalogLoading = ref(false),
  writing = ref(false)
const message = ref(''),
  catalogError = ref(''),
  phase = ref<'loading' | 'ready' | 'error'>('loading')
const bound = shallowRef<typeof workspace.currentReadContext.value>(null)
let serial = 0,
  catalogSerial = 0,
  disposed = false
const context = computed(() =>
  workspace.visible.value && auth.personalContext.phase === 'current'
    ? workspace.currentReadContext.value
    : null,
)
const visible = computed(() => !!context.value && context.value === bound.value)
const readOnly = computed(() => workspace.detail.project?.lifecycle !== 'active')
const busy = computed(
  () => auth.state.busy || loading.value || catalogLoading.value || writing.value,
)
const progress = computed(() =>
  auth.projectMCP.progress?.project === context.value?.projectID ? auth.projectMCP.progress : null,
)
const credentialProgress = computed(() =>
  auth.projectMCP.credentials.progress?.project === context.value?.projectID
    ? auth.projectMCP.credentials.progress
    : null,
)
const oauthProgress = computed(() =>
  auth.projectMCP.oauth.progress?.project === context.value?.projectID
    ? auth.projectMCP.oauth.progress
    : null,
)
const oauthPending = computed(
  () =>
    !!oauthProgress.value &&
    ['authorizing', 'completing', 'uncertain'].includes(oauthProgress.value.phase),
)
const credentialPending = computed(
  () =>
    credentialProgress.value?.phase === 'uncertain' ||
    credentialProgress.value?.phase === 'submitting',
)
const pending = computed(
  () =>
    credentialPending.value ||
    oauthPending.value ||
    progress.value?.phase === 'uncertain' ||
    progress.value?.phase === 'submitting',
)
const canMutate = computed(() => visible.value && !readOnly.value && !busy.value && !pending.value)
const currentConfig = computed(() => configs.value.find((c) => c.id === selected.value))
const currentConnection = computed(() =>
  connections.value.find((c) => c.config_id === selected.value),
)
const editor = reactive({
  open: false,
  id: '',
  version: '0',
  name: '',
  endpoint: '',
  description: '',
  conflict: false,
  scheme: 'none',
  header: '',
})
const credentialEditor = reactive({ open: false, config: '', username: '', value: '' })
const credentialConfig = computed(() => configs.value.find((c) => c.id === credentialEditor.config))
const savedCredential = computed(() => {
  const value = credentialProgress.value
  return value?.phase === 'confirmed' &&
    value.contextValid &&
    !value.result?.deleted &&
    value.config === currentConfig.value?.id
    ? value.result
    : null
})
const authenticationOptions = [
  { value: 'none', label: '无认证' },
  { value: 'bearer', label: 'Bearer Token' },
  { value: 'basic', label: '用户名和密码' },
  { value: 'custom_header', label: '自定义 Header' },
  { value: 'oauth', label: 'OAuth 2.1 授权' },
]
function clearCredentialEditor() {
  credentialEditor.open = false
  credentialEditor.username = ''
  credentialEditor.value = ''
  credentialEditor.config = ''
}
const editingConfig = shallowRef<MCPConfig | null>(null)
const confirm = shallowRef<{
  title: string
  description: string
  label: string
  mutation: MCPMutation
} | null>(null)
const stateLabel = (c?: MCPConnection) =>
  !c
    ? '未连接'
    : {
        unconnected: '未连接',
        connecting: '正在发现',
        connected: '已连接',
        auth_required: '需要认证',
        error: '连接失败',
        needs_reconnect: '需要重新连接',
      }[c.state]
const discoveryLabel = (s?: string) =>
  s === 'ready' ? '已发现' : s === 'error' ? '发现失败' : '尚未发现'
const authenticationLabel = (c: MCPConfig) =>
  ({
    none: '无认证',
    bearer: 'Bearer',
    basic: 'Basic',
    custom_header: '自定义 Header',
    oauth: 'OAuth 2.1',
  })[c.authentication.scheme]
function safeMessage(error: unknown): string {
  if (!(error instanceof AccountFailure)) return '请求未完成，请重新读取。'
  if (error.kind === 'invalid-input') return '请检查名称和 HTTPS 地址。'
  if (error.kind === 'busy') return '另一项操作仍在处理中。'
  if (error.kind === 'cancelled') return '此次请求已停止。'
  switch (error.problem?.code) {
    case 'VERSION_CONFLICT':
    case 'CONFIRMATION_STALE':
      return '配置已变化。原输入已保留，请读取最新版本后核对。'
    case 'PROJECT_NOT_ACTIVE':
      return '项目已归档，当前只能查看。'
    case 'FORBIDDEN':
    case 'UNAUTHENTICATED':
    case 'SESSION_REVOKED':
      return '当前访问权限已变化，请重新确认项目与会话。'
    case 'NOT_FOUND':
      return '此配置或连接已不可用，请重新读取。'
    case 'RESOURCE_BUSY':
      return '仍有连接引用此配置，请先断开相关连接。'
    default:
      return '请求未完成。提交结果未知时，请查询原操作。'
  }
}
function live(c: NonNullable<typeof context.value>, revision: number) {
  return !disposed && serial === revision && context.value === c
}
async function readCurrent() {
  const c = context.value
  if (!c || loading.value || writing.value || auth.state.busy) return
  const revision = ++serial
  loading.value = true
  try {
    const nextConfigs = await auth.projectMCP.configs(c.projectID)
    const nextConnections = await auth.projectMCP.connections(c.projectID)
    if (!live(c, revision)) return
    bound.value = c
    configs.value = nextConfigs
    connections.value = nextConnections
    phase.value = 'ready'
    if (!nextConfigs.some((v) => v.id === selected.value)) selected.value = nextConfigs[0]?.id ?? ''
  } catch (error) {
    if (live(c, revision)) {
      bound.value = c
      phase.value = 'error'
      message.value = safeMessage(error)
    }
  } finally {
    if (live(c, revision)) loading.value = false
  }
  if (live(c, revision) && selected.value) await readCatalog()
}
async function readCatalog() {
  const c = context.value,
    connection = currentConnection.value
  tools.value = []
  resources.value = []
  catalogError.value = ''
  if (!c || !connection || connection.state === 'unconnected' || auth.state.busy) return
  const revision = ++catalogSerial
  catalogLoading.value = true
  try {
    const nextTools = await auth.projectMCP.tools(c.projectID, connection.id)
    const nextResources = await auth.projectMCP.resources(c.projectID, connection.id)
    if (
      disposed ||
      c !== context.value ||
      revision !== catalogSerial ||
      connection.id !== currentConnection.value?.id
    )
      return
    tools.value = nextTools
    resources.value = nextResources
  } catch (error) {
    if (c === context.value && revision === catalogSerial) catalogError.value = safeMessage(error)
  } finally {
    if (c === context.value && revision === catalogSerial) catalogLoading.value = false
  }
}
function select(config: MCPConfig) {
  if (busy.value) return
  selected.value = config.id
  void readCatalog()
}
function edit(config?: MCPConfig) {
  if (!canMutate.value || config?.scope === 'system') return
  editingConfig.value = config ?? null
  Object.assign(editor, {
    open: true,
    id: config?.id ?? '',
    version: config?.version ?? '0',
    name: config?.name ?? '',
    endpoint: config?.endpoint ?? '',
    description: config?.description ?? '',
    conflict: false,
    scheme: config?.authentication.scheme ?? 'none',
    header:
      config?.authentication.scheme === 'custom_header' ? config.authentication.header_name : '',
  })
  message.value = ''
}
async function perform(input: MCPMutation) {
  const c = context.value
  if (!c || !canMutate.value) return
  writing.value = true
  message.value = ''
  try {
    await auth.projectMCP.start(c.projectID, input)
    if (c !== context.value || disposed) return
    editor.open = false
    confirm.value = null
    message.value = '操作已确认，正在读取当前连接。'
  } catch (error) {
    if (c !== context.value || disposed) return
    message.value = safeMessage(error)
    if (
      error instanceof AccountFailure &&
      ['VERSION_CONFLICT', 'CONFIRMATION_STALE'].includes(error.problem?.code ?? '')
    )
      editor.conflict = true
  } finally {
    if (c === context.value) writing.value = false
  }
  if (c === context.value && progress.value?.phase === 'confirmed') await readCurrent()
}
function saveConfig() {
  const authentication: MCPAuthentication =
    editor.scheme === 'custom_header'
      ? { scheme: 'custom_header', header_name: editor.header }
      : { scheme: editor.scheme as 'none' | 'bearer' | 'basic' | 'oauth' }
  const input = {
    expected_version: editor.version,
    input: {
      name: editor.name,
      description: editor.description,
      endpoint: editor.endpoint,
      authentication,
    },
  }
  void perform(
    editor.id
      ? { kind: 'config.update', target: editor.id, input }
      : { kind: 'config.create', input },
  )
}
function connect(config: MCPConfig) {
  const prior = connections.value.find((c) => c.config_id === config.id)
  if (config.authentication.scheme === 'oauth') {
    void beginOAuth(config)
    return
  }
  if (config.authentication.scheme !== 'none') {
    if (!canMutate.value) return
    clearCredentialEditor()
    credentialEditor.config = config.id
    credentialEditor.open = true
    return
  }
  void perform({
    kind: 'connection.connect',
    input: { config_id: config.id, expected_version: prior?.version ?? '0' },
  })
}
async function beginOAuth(config: MCPConfig) {
  const c = context.value
  if (!c || !canMutate.value || config.authentication.scheme !== 'oauth') return
  const prior = connections.value.find((item) => item.config_id === config.id)
  writing.value = true
  message.value = '正在准备授权，请在服务提供方页面确认。'
  try {
    await auth.projectMCP.oauth.begin(
      c.projectID,
      { config_id: config.id, expected_version: prior?.version ?? '0' },
      `${workspace.paths.value.home}/settings/mcp`,
    )
  } catch (error) {
    if (c === context.value && !disposed) message.value = safeMessage(error)
  } finally {
    if (c === context.value) writing.value = false
  }
}

async function lookupOAuth() {
  const c = context.value
  if (!c || busy.value || !oauthProgress.value?.contextValid) return
  writing.value = true
  try {
    await auth.projectMCP.oauth.lookup()
    if (c === context.value && !disposed && oauthProgress.value?.phase === 'confirmed') {
      message.value = '授权已确认，正在读取当前连接。'
    }
  } catch (error) {
    if (c === context.value && !disposed) message.value = safeMessage(error)
  } finally {
    if (c === context.value) writing.value = false
  }
  if (
    c === context.value &&
    oauthProgress.value &&
    ['confirmed', 'stopped'].includes(oauthProgress.value.phase)
  )
    await readCurrent()
}

async function saveCredential() {
  const c = context.value,
    config = credentialConfig.value
  if (!c || !config || !canMutate.value) return
  let material = credentialEditor.value
  if (!material) {
    message.value = '请输入认证凭据。'
    return
  }
  if (config.authentication.scheme === 'basic') {
    // Basic's Secret value is the base64 payload consumed by the real SDK
    // adapter. A colon in the username would change that protocol boundary.
    if (
      !credentialEditor.username ||
      credentialEditor.username.includes(':') ||
      /[\u0000-\u001f\u007f]/.test(credentialEditor.username + material)
    ) {
      message.value = '请检查用户名与密码。用户名不能包含冒号。'
      return
    }
    const original = `${credentialEditor.username}:${material}`
    const bytes = new TextEncoder().encode(original)
    if (new TextDecoder().decode(bytes) !== original) {
      bytes.fill(0)
      message.value = '请检查用户名与密码。'
      return
    }
    material = btoa(Array.from(bytes, (byte) => String.fromCharCode(byte)).join(''))
    bytes.fill(0)
  }
  writing.value = true
  message.value = ''
  clearCredentialEditor()
  try {
    await auth.projectMCP.credentials.start(c.projectID, { kind: 'create' }, material, config.id)
    if (c === context.value && !disposed) message.value = '凭据已保存。确认后可连接此服务。'
  } catch (error) {
    if (c === context.value && !disposed) message.value = safeMessage(error)
  } finally {
    material = ''
    if (c === context.value) writing.value = false
  }
}
function connectSavedCredential() {
  const config = currentConfig.value,
    credential = savedCredential.value
  if (
    !config ||
    !credential ||
    config.authentication.scheme === 'none' ||
    config.authentication.scheme === 'oauth'
  )
    return
  const prior = connections.value.find((c) => c.config_id === config.id)
  void perform({
    kind: 'connection.connect',
    input: {
      config_id: config.id,
      expected_version: prior?.version ?? '0',
      credential_id: credential.credential_id,
    },
  })
}
async function lookupCredential() {
  const c = context.value
  if (
    !c ||
    busy.value ||
    credentialProgress.value?.phase !== 'uncertain' ||
    !credentialProgress.value.contextValid
  )
    return
  writing.value = true
  try {
    const result = await auth.projectMCP.credentials.lookup()
    if (c === context.value && !disposed)
      message.value =
        'observed' in result && !result.observed
          ? '尚未查到原凭据操作，请稍后再次查询。'
          : '原凭据操作已确认。'
  } catch (error) {
    if (c === context.value && !disposed) message.value = safeMessage(error)
  } finally {
    if (c === context.value) writing.value = false
  }
}
async function lookup() {
  const c = context.value
  if (!c || busy.value || progress.value?.phase !== 'uncertain' || !progress.value.contextValid)
    return
  writing.value = true
  let confirmed = false
  try {
    const result = await auth.projectMCP.lookup()
    if (c !== context.value) return
    message.value =
      'state' in result && result.state !== 'complete'
        ? result.state === 'stopped'
          ? '原刷新操作已停止，远端结果无法确认。请检查当前连接后再决定是否重新操作。'
          : result.state === 'pending'
            ? '原操作仍在处理中，请稍后再次查询。'
            : '尚未查到原操作，不能据此判断它未提交。'
        : '原操作已确认，正在读取当前连接。'
    confirmed = !('state' in result) || result.state === 'complete' || result.state === 'stopped'
    if (confirmed) {
      editor.open = false
      confirm.value = null
    }
  } catch (error) {
    if (c === context.value) message.value = safeMessage(error)
  } finally {
    if (c === context.value) writing.value = false
  }
  if (c === context.value && confirmed) await readCurrent()
}
function adoptVersion() {
  const latest = configs.value.find((c) => c.id === editor.id)
  if (!latest || !canMutate.value) return
  editor.version = latest.version
  editor.conflict = false
}
watch(
  context,
  (c) => {
    ++serial
    ++catalogSerial
    auth.projectMCP.abandonRead()
    configs.value = []
    connections.value = []
    tools.value = []
    resources.value = []
    selected.value = ''
    loading.value = false
    catalogLoading.value = false
    writing.value = false
    bound.value = null
    phase.value = 'loading'
    message.value = ''
    editor.open = false
    clearCredentialEditor()
    confirm.value = null
    if (c) void readCurrent()
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  clearCredentialEditor()
  disposed = true
  ++serial
  ++catalogSerial
  auth.projectMCP.abandonRead()
})
</script>

<template>
  <section class="mcp-page" aria-labelledby="mcp-heading">
    <header class="mcp-header page-header">
      <div>
        <h1 id="mcp-heading" ref="heading" tabindex="-1">MCP 连接</h1>
        <p class="meta">连接外部服务，让 Agent 使用已发现的工具与资源。</p>
      </div>
      <div class="mcp-actions">
        <UiButton :disabled="busy || pending || !context" @click="readCurrent">重新读取</UiButton
        ><UiButton variant="primary" :disabled="!canMutate" @click="edit()">添加服务</UiButton>
      </div>
    </header>
    <UiState v-if="!visible" kind="loading" title="正在确认项目与连接" />
    <template v-else>
      <p v-if="readOnly" class="meta">项目已归档，连接配置只读。</p>
      <p v-if="message" role="status">{{ message }}</p>
      <section
        v-if="progress?.phase === 'uncertain' || progress?.phase === 'submitting'"
        class="mcp-notice"
        aria-label="原连接操作"
      >
        <h2>{{ progress?.phase === 'submitting' ? '正在处理连接操作' : '操作结果尚未确认' }}</h2>
        <p>查询原操作以确认结果。当前连接状态将在确认后重新读取。</p>
        <UiButton
          v-if="progress?.phase === 'uncertain'"
          :disabled="busy || !progress.contextValid"
          @click="lookup"
          >查询原操作</UiButton
        >
      </section>
      <section
        v-if="progress?.contextValid && progress.phase === 'stopped'"
        class="mcp-notice"
        role="status"
        aria-label="已停止的连接操作"
      >
        <h2>原刷新操作已停止</h2>
        <p>远端结果无法确认，原操作不会再次发送。请检查当前连接，再决定是否重新操作。</p>
      </section>
      <section v-if="oauthPending" class="mcp-notice" aria-label="原授权操作">
        <h2>
          {{
            oauthProgress?.phase === 'authorizing'
              ? '等待服务授权'
              : oauthProgress?.phase === 'completing'
                ? '正在确认授权'
                : '授权结果尚未确认'
          }}
        </h2>
        <p>
          {{
            oauthProgress?.phase === 'authorizing'
              ? '在服务提供方完成授权。重新打开授权页面会查询原操作。'
              : '查询原授权操作以确认结果，不会再次兑换授权码。'
          }}
        </p>
        <UiButton
          v-if="oauthProgress?.phase !== 'completing'"
          :disabled="busy || !oauthProgress?.contextValid"
          @click="lookupOAuth"
          >{{ oauthProgress?.phase === 'authorizing' ? '继续原授权' : '查询原授权操作' }}</UiButton
        >
      </section>
      <section
        v-if="oauthProgress?.contextValid && oauthProgress.phase === 'stopped'"
        class="mcp-notice"
        role="status"
        aria-label="已停止的授权操作"
      >
        <h2>原授权操作已停止</h2>
        <p>远端结果无法确认，原操作不会再次发送。请检查当前连接，再决定是否重新发起授权。</p>
      </section>
      <section v-if="credentialPending" class="mcp-notice" aria-label="原凭据操作">
        <h2>
          {{ credentialProgress?.phase === 'submitting' ? '正在保存凭据' : '凭据保存结果尚未确认' }}
        </h2>
        <p>查询原操作以确认结果。凭据材料不会随查询再次提交。</p>
        <UiButton
          v-if="credentialProgress?.phase === 'uncertain'"
          :disabled="busy || !credentialProgress.contextValid"
          @click="lookupCredential"
          >查询原凭据操作</UiButton
        >
      </section>
      <UiState v-if="phase === 'error'" kind="error" title="无法读取 MCP 连接" />
      <UiState
        v-else-if="!configs.length"
        kind="empty"
        title="还没有可用服务"
        description="添加一个 HTTPS MCP 服务，连接后查看它提供的工具和资源。"
      />
      <div v-else class="mcp-layout">
        <div class="mcp-services" aria-label="可用 MCP 服务">
          <button
            v-for="config in configs"
            :key="config.id"
            type="button"
            class="mcp-service"
            :class="{ selected: selected === config.id }"
            :aria-pressed="selected === config.id"
            :disabled="busy"
            @click="select(config)"
          >
            <span class="mcp-service-name">{{ config.name }}</span
            ><span class="meta"
              >{{ config.scope === 'system' ? '系统配置' : '项目配置' }} ·
              {{ authenticationLabel(config) }}</span
            >
            <span>{{ stateLabel(connections.find((c) => c.config_id === config.id)) }}</span>
          </button>
        </div>
        <section v-if="currentConfig" class="mcp-detail" aria-label="连接详情">
          <header class="mcp-detail-heading">
            <div>
              <h2>{{ currentConfig.name }}</h2>
              <p class="meta mcp-endpoint">{{ currentConfig.endpoint }}</p>
            </div>
            <UiBadge :tone="currentConnection?.state === 'connected' ? 'success' : 'neutral'">{{
              stateLabel(currentConnection)
            }}</UiBadge>
          </header>
          <p v-if="currentConfig.description">{{ currentConfig.description }}</p>
          <div class="mcp-actions">
            <UiButton
              v-if="
                !currentConnection ||
                ['unconnected', 'auth_required', 'error', 'needs_reconnect'].includes(
                  currentConnection.state,
                )
              "
              variant="primary"
              :disabled="!canMutate"
              @click="connect(currentConfig)"
              >{{
                currentConfig.authentication.scheme === 'oauth'
                  ? '授权并连接'
                  : currentConfig.authentication.scheme === 'none'
                    ? '连接服务'
                    : '设置连接凭据'
              }}</UiButton
            >
            <UiButton
              v-if="
                savedCredential &&
                currentConfig.authentication.scheme !== 'none' &&
                currentConfig.authentication.scheme !== 'oauth' &&
                (!currentConnection ||
                  ['unconnected', 'auth_required', 'error', 'needs_reconnect'].includes(
                    currentConnection.state,
                  ))
              "
              variant="primary"
              :disabled="!canMutate"
              @click="connectSavedCredential"
              >使用已保存凭据连接</UiButton
            >
            <template v-if="currentConnection?.state === 'connected'">
              <UiButton
                variant="primary"
                :disabled="!canMutate"
                @click="
                  perform({
                    kind: 'connection.refresh',
                    target: currentConnection.id,
                    version: currentConnection.version,
                  })
                "
                >刷新发现</UiButton
              >
              <UiButton
                :disabled="!canMutate"
                @click="
                  perform({
                    kind: currentConnection.enabled ? 'connection.disable' : 'connection.enable',
                    target: currentConnection.id,
                    version: currentConnection.version,
                  })
                "
                >{{ currentConnection.enabled ? '停用连接' : '启用连接' }}</UiButton
              >
            </template>
            <UiButton
              v-if="currentConfig.scope === 'project'"
              variant="ghost"
              :disabled="!canMutate"
              @click="edit(currentConfig)"
              >编辑配置</UiButton
            >
            <UiButton
              v-if="currentConnection && currentConnection.state !== 'unconnected'"
              variant="ghost"
              :disabled="!canMutate"
              @click="
                confirm = {
                  title: '断开连接',
                  description: '此项目将停止使用该连接。已开始的调用继续按原运行记录处理。',
                  label: '断开连接',
                  mutation: {
                    kind: 'connection.disconnect',
                    target: currentConnection.id,
                    version: currentConnection.version,
                  },
                }
              "
              >断开</UiButton
            >
            <UiButton
              v-if="
                currentConfig.scope === 'project' &&
                (!currentConnection || currentConnection.state === 'unconnected')
              "
              variant="ghost"
              :disabled="!canMutate"
              @click="
                confirm = {
                  title: '删除配置',
                  description: '删除此项目的服务配置。',
                  label: '删除配置',
                  mutation: {
                    kind: 'config.delete',
                    target: currentConfig.id,
                    version: currentConfig.version,
                  },
                }
              "
              >删除配置</UiButton
            >
          </div>
          <p
            v-if="currentConnection?.state === 'connected' && !currentConnection.enabled"
            class="meta"
          >
            连接已停用，目录仅供查看。
          </p>
          <p v-if="currentConnection?.last_error_code" role="status">
            服务返回：<code>{{ currentConnection.last_error_code }}</code
            >。请核对配置与认证后再刷新。
          </p>
          <UiState v-if="catalogLoading" kind="loading" title="正在读取发现目录" />
          <UiState
            v-else-if="catalogError"
            kind="error"
            title="无法读取发现目录"
            :description="catalogError"
          />
          <template v-else>
            <section class="mcp-catalog" aria-label="Tool 目录">
              <h3>
                工具
                <span class="meta"
                  >{{ tools.length }} · {{ discoveryLabel(currentConnection?.tools_state) }}</span
                >
              </h3>
              <p v-if="!tools.length" class="meta">当前没有已发布的工具。</p>
              <ul v-else>
                <li v-for="tool in tools" :key="tool.tool_id">
                  <div class="mcp-catalog-title">
                    <strong>{{ tool.definition.name }}</strong
                    ><UiBadge v-if="!tool.active">不可用</UiBadge>
                  </div>
                  <p v-if="tool.definition.description">{{ tool.definition.description }}</p>
                  <details>
                    <summary>输入结构</summary>
                    <pre>{{ JSON.stringify(tool.definition.input_schema, null, 2) }}</pre>
                  </details>
                </li>
              </ul>
            </section>
            <section class="mcp-catalog" aria-label="Resource 目录">
              <h3>
                资源
                <span class="meta"
                  >{{ resources.length }} ·
                  {{ discoveryLabel(currentConnection?.resources_state) }}</span
                >
              </h3>
              <p v-if="!resources.length" class="meta">当前没有已发布的资源或模板。</p>
              <ul v-else>
                <li v-for="resource in resources" :key="resource.id">
                  <div class="mcp-catalog-title">
                    <strong>{{ resource.title || resource.name || resource.uri }}</strong
                    ><UiBadge v-if="resource.kind === 'template'">模板</UiBadge>
                  </div>
                  <p class="meta">{{ resource.uri }}</p>
                  <p v-if="resource.description">{{ resource.description }}</p>
                  <p v-if="resource.mime_type" class="meta">{{ resource.mime_type }}</p>
                </li>
              </ul>
            </section>
          </template>
        </section>
      </div>
      <UiDialog
        :open="editor.open && !pending"
        :title="editor.id ? '编辑 MCP 服务' : '添加 MCP 服务'"
        :close-on-outside="false"
        :close-on-escape="!busy"
        :fallback-focus="heading"
        @update:open="!busy && (editor.open = $event)"
      >
        <form id="mcp-config-form" class="mcp-form" @submit.prevent="saveConfig">
          <UiField v-slot="field" label="服务名称" required
            ><UiInput
              :id="field.id"
              v-model="editor.name"
              required
              maxlength="256"
              :disabled="busy"
          /></UiField>
          <UiField
            v-slot="field"
            label="MCP 地址"
            required
            hint="使用服务提供的 HTTPS Streamable HTTP 地址。"
            ><UiInput
              :id="field.id"
              v-model="editor.endpoint"
              type="url"
              required
              :aria-describedby="field.describedby"
              :disabled="busy"
          /></UiField>
          <UiField v-slot="field" label="说明"
            ><UiTextarea :id="field.id" v-model="editor.description" rows="3" :disabled="busy"
          /></UiField>
          <UiField v-slot="field" label="认证方式"
            ><UiSelect
              :id="field.id"
              v-model="editor.scheme"
              label="认证方式"
              :options="authenticationOptions"
              :disabled="busy"
          /></UiField>
          <UiField
            v-if="editor.scheme === 'custom_header'"
            v-slot="field"
            label="Header 名称"
            required
          >
            <UiInput
              :id="field.id"
              v-model="editor.header"
              required
              maxlength="128"
              :disabled="busy"
            />
          </UiField>
          <p v-if="editor.conflict" role="status">
            原输入已保留。请读取并核对当前配置，再采用当前版本。
          </p>
          <div v-if="editor.conflict" class="mcp-actions">
            <UiButton :disabled="busy" @click="readCurrent">读取当前配置</UiButton
            ><UiButton :disabled="!canMutate" @click="adoptVersion">采用当前版本</UiButton>
          </div>
        </form>
        <template #footer
          ><UiButton variant="ghost" :disabled="busy" @click="editor.open = false">取消</UiButton
          ><UiButton
            type="submit"
            form="mcp-config-form"
            variant="primary"
            :disabled="!canMutate || editor.conflict"
            >保存配置</UiButton
          ></template
        >
      </UiDialog>
      <UiDialog
        :open="credentialEditor.open && !pending"
        title="设置连接凭据"
        :close-on-outside="false"
        :close-on-escape="!busy"
        :fallback-focus="heading"
        @update:open="!busy && !$event && clearCredentialEditor()"
      >
        <p>为 {{ credentialConfig?.name }} 保存此项目专用的认证凭据。保存后可确认连接。</p>
        <form id="mcp-credential-form" class="mcp-form" @submit.prevent="saveCredential">
          <UiField
            v-if="credentialConfig?.authentication.scheme === 'basic'"
            v-slot="field"
            label="用户名"
            required
          >
            <UiInput
              :id="field.id"
              v-model="credentialEditor.username"
              required
              autocomplete="off"
              :disabled="busy"
            />
          </UiField>
          <UiField
            v-slot="field"
            :label="
              credentialConfig?.authentication.scheme === 'basic'
                ? '密码'
                : credentialConfig?.authentication.scheme === 'custom_header'
                  ? 'Header 值'
                  : 'Token'
            "
            required
            hint="仅用于此服务认证，保存后不会回显。"
          >
            <UiInput
              :id="field.id"
              v-model="credentialEditor.value"
              type="password"
              required
              autocomplete="new-password"
              :aria-describedby="field.describedby"
              :disabled="busy"
            />
          </UiField>
        </form>
        <template #footer
          ><UiButton variant="ghost" :disabled="busy" @click="clearCredentialEditor">取消</UiButton>
          <UiButton
            type="submit"
            form="mcp-credential-form"
            variant="primary"
            :disabled="!canMutate"
            >保存凭据</UiButton
          ></template
        >
      </UiDialog>
      <UiDialog
        :open="!!confirm && !pending"
        :title="confirm?.title ?? ''"
        :close-on-outside="false"
        :fallback-focus="heading"
        @update:open="!busy && !$event && (confirm = null)"
        ><p>{{ confirm?.description }}</p>
        <template #footer
          ><UiButton variant="ghost" :disabled="busy" @click="confirm = null">取消</UiButton
          ><UiButton
            variant="danger"
            :disabled="!canMutate"
            @click="confirm && perform(confirm.mutation)"
            >{{ confirm?.label }}</UiButton
          ></template
        ></UiDialog
      >
    </template>
  </section>
</template>

<style scoped>
.mcp-page,
.mcp-detail,
.mcp-form {
  display: grid;
  gap: var(--section-gap);
  min-width: 0;
}
.mcp-page h1,
.mcp-page h2,
.mcp-page h3,
.mcp-page p {
  margin: 0;
  overflow-wrap: anywhere;
}
.mcp-header,
.mcp-detail-heading,
.mcp-catalog-title {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: var(--content-gap);
}
.mcp-header {
  flex-wrap: wrap;
}
.mcp-header h1 + p {
  margin-top: 8px;
}
.mcp-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.meta {
  color: var(--muted);
  font-size: 13px;
  font-weight: 400;
}
.mcp-layout {
  display: grid;
  grid-template-columns: minmax(180px, 240px) minmax(0, 1fr);
  gap: var(--section-gap);
}
.mcp-services {
  border-right: 1px solid var(--border);
  padding-right: var(--content-gap);
}
.mcp-service {
  display: grid;
  gap: 6px;
  width: 100%;
  padding: 12px;
  border: 0;
  border-radius: var(--radius);
  text-align: left;
  background: transparent;
  color: var(--text);
  cursor: pointer;
  overflow-wrap: anywhere;
}
.mcp-service.selected {
  background: var(--nav-selected);
}
.mcp-service:hover {
  background: var(--surface-alt);
}
.mcp-service:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
.mcp-service-name {
  font-weight: 600;
}
.mcp-notice {
  display: grid;
  gap: var(--content-gap);
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.mcp-catalog {
  display: grid;
  gap: 12px;
}
.mcp-catalog ul {
  list-style: none;
  padding: 0;
  margin: 0;
}
.mcp-catalog li {
  display: grid;
  gap: 8px;
  padding: 14px 0;
  border-top: 1px solid var(--border);
}
.mcp-catalog pre {
  overflow: auto;
  max-height: 240px;
  padding: 12px;
  background: var(--surface);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.mcp-catalog summary {
  cursor: pointer;
  font-size: 13px;
  color: var(--muted);
}
@media (max-width: 760px) {
  .mcp-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .mcp-services {
    border-right: 0;
    border-bottom: 1px solid var(--border);
    padding: 0 0 12px;
  }
}
</style>
