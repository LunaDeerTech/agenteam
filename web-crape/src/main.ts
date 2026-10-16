import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { initializeTheme } from './composables/useTheme'
import './styles/tokens.css'
import './styles/base.css'
import './styles/components.css'
initializeTheme()
createApp(App).use(router).mount('#app')
