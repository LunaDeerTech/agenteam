import { createApp } from "vue";
import ComponentFixture from "./ComponentFixture.vue";
import "../../src/styles/index.css";
import { initTheme } from "../../src/composables/useTheme";

initTheme();
createApp(ComponentFixture).mount("#app");
