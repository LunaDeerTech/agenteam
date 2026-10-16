<script setup lang="ts">
import { computed, ref } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { UiDialog, UiIcon, UiIconButton, UiMenu } from "../components/ui";
import { useTheme } from "../composables/useTheme";
import PageHeading from "./PageHeading.vue";
import ProjectNavigation from "./ProjectNavigation.vue";
import SettingsShell from "./SettingsShell.vue";

const route = useRoute();
const navigationOpen = ref(false);
const { theme, setTheme } = useTheme();
const themes = computed(() => [{id:"light",label:"浅色",checked:theme.value === "light"},{id:"dark",label:"深色",checked:theme.value === "dark"},{id:"system",label:"跟随系统",checked:theme.value === "system"}]);
const changeTheme = (value: string) => { if(value === "light" || value === "dark" || value === "system") setTheme(value); };
</script>

<template>
  <div class="app-shell">
    <a class="skip-link" href="#main">跳到主要内容</a>
    <header class="system-header">
      <RouterLink to="/" class="brand" aria-label="agenteam 首页"><span class="brand-mark" aria-hidden="true"><i></i><i></i><i></i><i></i></span>agenteam</RouterLink>
      <nav class="system-primary" aria-label="系统导航">
        <RouterLink to="/" :aria-current="route.name === 'home' ? 'page' : undefined">首页</RouterLink>
        <RouterLink to="/projects" :aria-current="route.name === 'projects' || route.meta.projectContext ? 'page' : undefined">项目</RouterLink>
      </nav>
      <div class="system-actions">
        <RouterLink class="desktop-link" to="/system" :aria-current="route.meta.settingsScope === 'system' ? 'page' : undefined">系统设置</RouterLink>
        <RouterLink class="desktop-link" to="/settings" :aria-current="route.meta.settingsScope === 'personal' ? 'page' : undefined">个人设置</RouterLink>
        <UiMenu label="外观" :items="themes" @select="changeTheme"><template #trigger><UiIcon :name="theme === 'dark' ? 'moon' : theme === 'light' ? 'sun' : 'monitor'" :size="16" /></template></UiMenu>
        <UiIconButton class="mobile-navigation-trigger" icon="menu" label="打开系统导航" @click="navigationOpen = true" />
      </div>
    </header>
    <main id="main" class="app-main" tabindex="-1">
      <ProjectNavigation v-if="route.meta.projectContext" />
      <SettingsShell v-if="route.meta.settingsScope"><slot /></SettingsShell>
      <section v-else class="standard-page" aria-labelledby="page-title"><PageHeading :title="route.meta.title || ''" :description="route.meta.description" /><slot /></section>
    </main>
    <UiDialog v-model:open="navigationOpen" title="导航" side="left">
      <nav class="mobile-navigation" aria-label="系统导航目录">
        <RouterLink to="/" @click="navigationOpen=false">首页</RouterLink>
        <RouterLink to="/projects" @click="navigationOpen=false">项目</RouterLink>
        <RouterLink to="/system" @click="navigationOpen=false">系统设置</RouterLink>
        <RouterLink to="/settings" @click="navigationOpen=false">个人设置</RouterLink>
      </nav>
    </UiDialog>
  </div>
</template>

<style scoped>
.app-shell { height:100dvh; display:flex; flex-direction:column; background:var(--surface-page); }
.system-header { display:flex; align-items:center; gap:27px; flex:none; min-height:48px; padding:0 26px; }
.brand { display:inline-flex; align-items:center; gap:9px; color:var(--text-primary); font-size:14px; font-weight:600; letter-spacing:-.025em; text-decoration:none; }
.brand-mark { display:grid; grid-template-columns:repeat(2,7px); gap:2px; transform:rotate(-9deg); }
.brand-mark i { width:7px; height:7px; border-radius:2px; background:currentColor; }
.brand-mark i:nth-child(2) { opacity:.6; }
.system-primary { display:flex; align-items:center; gap:6px; }
.system-primary a { padding:6px 11px; border-radius:5px; text-decoration:none; color:var(--text-secondary); font-size:12px; line-height:1.4; }
.system-primary a[aria-current="page"] { background:var(--surface-active); color:var(--text-primary); }
.system-actions { display:flex; align-items:center; gap:18px; margin-left:auto; }
.desktop-link { color:var(--text-secondary); text-decoration:none; font-size:12px; }
.desktop-link[aria-current="page"] { color:var(--text-primary); }
.app-main { display:flex; flex-direction:column; min-width:0; min-height:0; flex:1; background:var(--surface-content); margin:0 10px 10px; border:1px solid var(--border-subtle); border-radius:10px; overflow:hidden; }
.standard-page { min-width:0; min-height:0; flex:1; overflow-y:auto; padding:45px 48px 64px; }
.mobile-navigation-trigger { display:none; }
.mobile-navigation { display:grid; gap:6px; }
.mobile-navigation a { padding:10px; border-radius:5px; text-decoration:none; color:var(--text-primary); font-size:13px; }
.mobile-navigation a:hover { background:var(--surface-hover); }
.skip-link { position:fixed; left:12px; top:-80px; z-index:100; padding:10px 14px; border-radius:5px; background:var(--surface-content); color:var(--text-primary); }
.skip-link:focus { top:8px; }
@media(max-width:760px) { .system-header { min-height:48px; padding:0 16px; gap:14px; } .brand { font-size:13px; } .system-primary { gap:1px; } .system-primary a { padding:6px 8px; } .system-actions { gap:6px; } .desktop-link { display:none; } .mobile-navigation-trigger { display:inline-flex; } .app-main { margin:0 5px 5px; border-radius:9px; } .standard-page { padding:28px 19px 48px; } }
</style>
