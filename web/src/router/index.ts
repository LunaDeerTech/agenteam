import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import NotFoundView from '../views/NotFoundView.vue'
import { installAuthentication, projectRoute } from './auth'
import { installAccountLinkCapture, type AccountEntryMode } from './account-link'
installAccountLinkCapture()
export const pages: RouteRecordRaw[] = import.meta.env.DEV
  ? [
      {
        path: '/debug',
        name: 'debug',
        component: () => import('../views/debug/DebugView.vue'),
      },
    ]
  : []
export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: HomeView,
      meta: { authentication: true, protected: true },
    },
    { path: '/login', name: 'login', component: LoginView, meta: { authentication: true } },
    {
      path: '/invite',
      name: 'invitation',
      component: () => import('../views/auth/InvitationView.vue'),
      meta: { authentication: true, accountEntry: 'invitation' },
    },
    {
      path: '/forgot-password',
      name: 'forgot-password',
      component: () => import('../views/auth/ForgotPasswordView.vue'),
      meta: { authentication: true, accountEntry: 'forgot' },
    },
    {
      path: '/reset-password',
      name: 'reset-password',
      component: () => import('../views/auth/ResetPasswordView.vue'),
      meta: { authentication: true, accountEntry: 'reset' },
    },
    {
      path: '/system',
      component: () => import('../views/system/SystemSettingsView.vue'),
      meta: {
        authentication: true,
        protected: true,
        systemAdmin: true,
        navigation: { label: '系统设置', order: 20 },
      },
      redirect: '/system/users',
      children: [
        { path: 'users', component: () => import('../views/system/SystemUsersView.vue') },
        {
          path: 'invitations',
          component: () => import('../views/system/SystemInvitationsView.vue'),
        },
        { path: 'providers', component: () => import('../views/system/SystemProvidersView.vue') },
        { path: 'models', component: () => import('../views/system/SystemModelsView.vue') },
        {
          path: 'model-selection',
          component: () => import('../views/system/SystemModelSelectionView.vue'),
        },
        { path: 'audit', component: () => import('../views/system/SystemAuditView.vue') },
        {
          path: 'account-security',
          component: () => import('../views/system/SystemAccountSecurityView.vue'),
        },
        { path: 'smtp', component: () => import('../views/system/SystemSMTPSettingsView.vue') },
        {
          path: 'outbound-policy',
          component: () => import('../views/system/SystemOutboundPolicyView.vue'),
        },
        {
          path: 'runtime-information',
          component: () => import('../views/system/SystemRuntimeInformationView.vue'),
        },
      ],
    },
    {
      path: '/settings',
      component: () => import('../views/settings/PersonalSettingsView.vue'),
      meta: { authentication: true, protected: true },
      redirect: '/settings/profile',
      children: [
        { path: 'profile', component: () => import('../views/settings/ProfileSettings.vue') },
        { path: 'appearance', component: () => import('../views/settings/AppearanceSettings.vue') },
        { path: 'password', component: () => import('../views/settings/PasswordSettings.vue') },
      ],
    },
    {
      path: '/projects',
      name: 'projects',
      component: () => import('../views/projects/ProjectListView.vue'),
      meta: {
        authentication: true,
        protected: true,
        projectWorkspace: true,
        navigation: { label: '项目', order: 10 },
      },
    },
    {
      path: '/:username/:project_name',
      component: () => import('../views/projects/ProjectWorkspaceView.vue'),
      meta: { authentication: true, protected: true, projectWorkspace: true },
      children: [
        {
          path: '',
          name: 'project-home',
          component: () => import('../views/projects/ProjectHomeView.vue'),
        },
        {
          path: 'settings',
          component: () => import('../views/projects/ProjectSettingsView.vue'),
          redirect: (to) => {
            const address = projectRoute(to.fullPath)
            return address
              ? address.path + '/general'
              : { name: 'not-found', params: { pathMatch: to.fullPath.slice(1).split('/') } }
          },
          children: [
            {
              path: 'general',
              name: 'project-general',
              component: () => import('../views/projects/ProjectGeneralSettings.vue'),
            },
            {
              path: 'audit',
              name: 'project-audit',
              component: () => import('../views/projects/ProjectAuditView.vue'),
            },
          ],
        },
      ],
    },
    ...pages,
    { path: '/:pathMatch(.*)*', name: 'not-found', component: NotFoundView },
  ],
})
installAuthentication(router)
declare module 'vue-router' {
  interface RouteMeta {
    navigation?: { label: string; order: number }
    authentication?: boolean
    protected?: boolean
    projectWorkspace?: boolean
    systemAdmin?: boolean
    accountEntry?: AccountEntryMode
  }
}
