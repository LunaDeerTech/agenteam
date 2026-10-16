import { createApp } from "vue";
import App from "./App.vue";
import { router } from "./router";
import { initTheme } from "./composables/useTheme";
import "./styles/index.css";
import "./styles/account.css";

initTheme();
createApp(App).use(router).mount("#app");
