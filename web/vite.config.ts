import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vitest/config";

const previewHost = process.env.AGENTEAM_PREVIEW_HOST;
const previewPort = process.env.AGENTEAM_PREVIEW_PORT;

export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  cacheDir: ".vite",
  server: {
    allowedHosts: previewHost ? [previewHost] : [],
    hmr: previewHost
      ? {
          host: previewHost,
          clientPort: Number(previewPort || 80),
          protocol: "ws",
        }
      : undefined,
  },
  build: { outDir: "dist", emptyOutDir: true },
  test: {
    environment: "jsdom",
    include: ["tests/**/*.spec.ts"],
    clearMocks: true,
  },
});
