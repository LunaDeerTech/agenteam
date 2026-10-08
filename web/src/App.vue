<script setup lang="ts">
import { computed, onMounted, onUnmounted, provide, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppShell from './components/layout/AppShell.vue'
import UiButton from './components/ui/UiButton.vue'
import UiState from './components/ui/UiState.vue'
import UiDialog from './components/ui/UiDialog.vue'
import { useSession, type PersonalIdentity } from './composables/useSession'
import { createPersonalSettings, personalSettingsKey } from './composables/usePersonalSettings'
import {
  installPersonalNavigation,
  installAccountEntryNavigation,
  installInvitationNavigation,
  installProviderNavigation,
  installModelNavigation,
  installModelSelectionNavigation,
  installAccountSecurityNavigation,
  installSMTPSettingsNavigation,
  installOutboundPolicyNavigation,
  installProjectNavigation,
  installProjectModelSettingsNavigation,
} from './router/auth'
import { createAccountEntry, accountEntryKey } from './composables/useAccountEntry'
import { createSystemInvitations, systemInvitationsKey } from './composables/useSystemInvitations'
import { createSystemProviders, systemProvidersKey } from './composables/useSystemProviders'
import { createSystemModels, systemModelsKey } from './composables/useSystemModels'
import {
  createSystemModelSelection,
  systemModelSelectionKey,
} from './composables/useSystemModelSelection'
import {
  createSystemAccountSecurity,
  systemAccountSecurityKey,
} from './composables/useSystemAccountSecurity'
import {
  createSystemSMTPSettings,
  systemSMTPSettingsKey,
} from './composables/useSystemSMTPSettings'
import {
  createSystemSMTPDelivery,
  createSMTPSections,
  systemSMTPDeliveryKey,
  smtpSectionsKey,
} from './composables/useSystemSMTPDelivery'
import {
  createSystemOutboundPolicy,
  systemOutboundPolicyKey,
} from './composables/useSystemOutboundPolicy'
import { createProjectWorkspace, projectWorkspaceKey } from './composables/useProjectWorkspace'
import {
  createProjectModelSettings,
  projectModelSettingsKey,
} from './composables/useProjectModelSettings'
const auth = useSession(),
  state = auth.state,
  route = useRoute(),
  router = useRouter()
const projects = createProjectWorkspace(auth, (path) => router.replace(path))
provide(projectWorkspaceKey, projects)
const stopProjectNavigation = installProjectNavigation(router, projects)
const projectModels = createProjectModelSettings(auth, projects)
provide(projectModelSettingsKey, projectModels)
const stopProjectModelNavigation = installProjectModelSettingsNavigation(router, projectModels)
const settings = createPersonalSettings(auth)
provide(personalSettingsKey, settings)
const stopPersonalNavigation = installPersonalNavigation(router, settings)
const entry = createAccountEntry(auth)
provide(accountEntryKey, entry)
const stopEntryNavigation = installAccountEntryNavigation(router, entry)
const invitations = createSystemInvitations(auth)
provide(systemInvitationsKey, invitations)
const stopInvitationNavigation = installInvitationNavigation(router, invitations)
const providers = createSystemProviders(auth)
provide(systemProvidersKey, providers)
const stopProviderNavigation = installProviderNavigation(router, providers)
const models = createSystemModels(auth)
provide(systemModelsKey, models)
const stopModelNavigation = installModelNavigation(router, models)
const selection = createSystemModelSelection(auth)
provide(systemModelSelectionKey, selection)
const stopSelectionNavigation = installModelSelectionNavigation(router, selection)
const accountSecurity = createSystemAccountSecurity(auth)
provide(systemAccountSecurityKey, accountSecurity)
const stopAccountSecurityNavigation = installAccountSecurityNavigation(router, accountSecurity)
const smtp = createSystemSMTPSettings(auth)
provide(systemSMTPSettingsKey, smtp)
const smtpObservationIdentity = shallowRef<PersonalIdentity | null>(null)
const stopSMTPAvailability = watch(
  () => smtp.observation.value,
  (value) => {
    smtpObservationIdentity.value = value ? auth.personalContext.identity : null
  },
  { flush: 'sync' },
)
const smtpAvailability = computed(() => {
  const observed = smtpObservationIdentity.value,
    current = auth.personalContext.identity
  return Object.freeze({
    ready:
      smtp.observation.phase === 'ready' &&
      !!observed &&
      !!current &&
      observed.userID === current.userID &&
      observed.sessionID === current.sessionID &&
      observed.epoch === current.epoch,
    configured: smtp.observation.value?.configured === true,
  })
})
const smtpDelivery = createSystemSMTPDelivery(auth, () => smtpAvailability.value)
provide(systemSMTPDeliveryKey, smtpDelivery)
const smtpSections = createSMTPSections(auth, smtp, smtpDelivery)
provide(smtpSectionsKey, smtpSections)
const stopSMTPNavigation = installSMTPSettingsNavigation(router, smtpSections)
const outboundPolicy = createSystemOutboundPolicy(auth)
provide(systemOutboundPolicyKey, outboundPolicy)
const stopOutboundNavigation = installOutboundPolicyNavigation(router, outboundPolicy)
async function logout() {
  if (
    (await settings.confirmLeave()) &&
    (await invitations.confirmLeave()) &&
    (await providers.confirmLeave()) &&
    (await models.confirmLeave()) &&
    (await selection.confirmLeave()) &&
    (await accountSecurity.confirmLeave()) &&
    (await smtpSections.confirmLeave()) &&
    (await outboundPolicy.confirmLeave()) &&
    (await projects.confirmLeave()) &&
    (await projectModels.confirmLeave())
  )
    await auth.logout()
}
async function refreshVisible() {
  if (
    document.visibilityState === 'hidden' ||
    !route.meta.authentication ||
    route.meta.accountEntry ||
    state.busy
  )
    return
  await auth.restore()
}
let navigating = false
watch(
  () => [state.phase, state.busy] as const,
  async ([phase, busy]) => {
    if (phase === 'anonymous' && !busy && route.meta.protected && !navigating) {
      navigating = true
      try {
        await router.replace('/login')
      } finally {
        navigating = false
      }
    }
  },
)
onMounted(() => {
  projects.afterNavigation(route.fullPath, '')
  projectModels.afterNavigation(route.fullPath, '')
  entry.afterNavigation(route.fullPath, '')
  invitations.afterNavigation(route.fullPath, '')
  providers.afterNavigation(route.fullPath, '')
  models.afterNavigation(route.fullPath, '')
  selection.afterNavigation(route.fullPath, '')
  accountSecurity.afterNavigation(route.fullPath, '')
  smtpSections.afterNavigation(route.fullPath, '')
  outboundPolicy.afterNavigation(route.fullPath, '')
  document.addEventListener('visibilitychange', refreshVisible)
  window.addEventListener('pageshow', refreshVisible)
})
onUnmounted(() => {
  document.removeEventListener('visibilitychange', refreshVisible)
  window.removeEventListener('pageshow', refreshVisible)
  stopProjectModelNavigation()
  projectModels.dispose()
  stopProjectNavigation()
  projects.dispose()
  stopPersonalNavigation()
  stopEntryNavigation()
  stopInvitationNavigation()
  stopProviderNavigation()
  stopModelNavigation()
  stopSelectionNavigation()
  stopAccountSecurityNavigation()
  stopSMTPNavigation()
  stopOutboundNavigation()
  outboundPolicy.dispose()
  stopSMTPAvailability()
  smtpSections.dispose()
  accountSecurity.dispose()
  selection.dispose()
  models.dispose()
  providers.dispose()
  invitations.dispose()
  entry.dispose()
  settings.dispose()
  auth.leave()
})
</script>
<template>
  <AppShell v-if="route.meta.protected" class="authentication-shell">
    <template #account>
      <div class="account-actions">
        <RouterLink
          v-if="state.user"
          class="account-name"
          to="/settings/profile"
          :title="state.user.display_name || state.user.email"
          >{{ state.user.display_name || state.user.email }}</RouterLink
        >
        <UiButton
          v-if="state.phase === 'authenticated' || state.phase === 'signing-out'"
          variant="ghost"
          :state="state.phase === 'signing-out' ? 'loading' : 'idle'"
          :disabled="state.busy"
          @click="logout"
          >退出登录</UiButton
        >
      </div>
    </template>
    <RouterView v-if="state.phase === 'authenticated'" />
    <div v-else class="session-check">
      <UiState
        :kind="state.busy ? 'loading' : 'error'"
        :title="state.busy ? '正在确认会话' : '会话尚未确认'"
        :description="state.notice"
      >
        <div class="ui-row">
          <UiButton :disabled="state.busy" @click="auth.restore">检查当前会话</UiButton>
          <UiButton v-if="state.canRetryOriginal" :disabled="state.busy" @click="auth.retryOriginal"
            >重试原请求</UiButton
          >
          <UiButton variant="ghost" :disabled="state.busy" @click="auth.restart"
            >放弃原请求，重新开始</UiButton
          >
        </div>
      </UiState>
    </div>
  </AppShell>
  <RouterView v-else />
  <UiDialog
    :open="projectModels.confirmation.open"
    :title="projectModels.confirmation.title"
    @update:open="!$event && projectModels.cancelConfirmation()"
  >
    <p>{{ projectModels.confirmation.message }}</p>
    <template #footer>
      <UiButton variant="ghost" @click="projectModels.cancelConfirmation">继续编辑</UiButton>
      <UiButton @click="projectModels.confirm">{{ projectModels.confirmation.label }}</UiButton>
    </template>
  </UiDialog>
  <UiDialog
    :open="projects.confirmation.open"
    :title="projects.confirmation.title"
    @update:open="!$event && projects.finishConfirmation(false)"
  >
    <p>{{ projects.confirmation.message }}</p>
    <template #footer>
      <UiButton variant="ghost" @click="projects.finishConfirmation(false)">继续编辑</UiButton>
      <UiButton @click="projects.finishConfirmation(true)">{{
        projects.confirmation.label
      }}</UiButton>
    </template>
  </UiDialog>
  <UiDialog
    :open="settings.confirmation.open"
    :title="settings.confirmation.title"
    @update:open="!$event && settings.finishConfirmation(false)"
  >
    <p>{{ settings.confirmation.message }}</p>
    <template #footer
      ><UiButton variant="ghost" @click="settings.finishConfirmation(false)">继续编辑</UiButton
      ><UiButton @click="settings.finishConfirmation(true)">{{
        settings.confirmation.label
      }}</UiButton></template
    >
  </UiDialog>
  <UiDialog
    :open="entry.confirmation.open"
    title="放弃本页输入？"
    @update:open="!$event && entry.finishConfirmation(false)"
  >
    <p>{{ entry.confirmation.message }}</p>
    <template #footer>
      <UiButton variant="ghost" @click="entry.finishConfirmation(false)">继续编辑</UiButton>
      <UiButton @click="entry.finishConfirmation(true)">放弃并离开</UiButton>
    </template>
  </UiDialog>
</template>
<style scoped>
.authentication-shell :deep(.system-nav) {
  flex-wrap: wrap;
  row-gap: 6px;
  padding-block: 6px;
}
.account-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-inline-start: auto;
  min-width: 0;
  max-width: 100%;
}
.account-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 32ch;
}
.session-check {
  padding: var(--space);
}
@media (max-width: 450px) {
  .account-name {
    max-width: 12ch;
  }
}
</style>
