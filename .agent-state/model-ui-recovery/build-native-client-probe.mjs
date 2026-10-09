import { build } from '../../web/node_modules/vite/dist/node/index.js';
import { fileURLToPath } from 'node:url';
import { resolve, dirname } from 'node:path';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
await build({
  configFile: false,
  root,
  build: {
    outDir: resolve(root, 'output/ai/model-ui-recovery/client-probe'),
    emptyOutDir: true,
    sourcemap: false,
    minify: false,
    lib: { entry: resolve(root, '.agent-state/model-ui-recovery/native-client-probe.ts'), name: 'ProjectModelsNativeProbe', formats: ['iife'], fileName: () => 'native-client-probe.js' },
  },
});
