import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import NotFoundView from '../views/NotFoundView.vue'
import { installAuthentication } from './auth'
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
    ...pages,
    { path: '/:pathMatch(.*)*', component: NotFoundView },
  ],
})
installAuthentication(router)
declare module 'vue-router' {
  interface RouteMeta {
    navigation?: { label: string; order: number }
    authentication?: boolean
    protected?: boolean
  }
}
