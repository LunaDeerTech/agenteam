import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import EmptyView from '../views/EmptyView.vue'
import NotFoundView from '../views/NotFoundView.vue'
export const pages: RouteRecordRaw[] = import.meta.env.DEV
  ? [
      {
        path: '/debug',
        name: 'debug',
        component: () => import('../views/debug/DebugView.vue'),
        meta: { navigation: { label: 'Debug', order: 0 } },
      },
    ]
  : []
export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/', ...(import.meta.env.DEV ? { redirect: '/debug' } : { component: EmptyView }) },
    ...pages,
    { path: '/:pathMatch(.*)*', component: NotFoundView },
  ],
})
declare module 'vue-router' {
  interface RouteMeta {
    navigation?: { label: string; order: number }
  }
}
