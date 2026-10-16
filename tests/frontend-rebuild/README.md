# 新 Vue 基础验证

本入口只验证从零建立的 `web/`：共享 UI、AppShell、单一 SettingsShell、既定路由骨架与本地主题。没有旧认证、客户端、权限、业务 API 或合成业务列表；`web-crape/` 仅为归档。旧 HTML 原型和旧 Vue 的通过记录不构成本轮证据。

组件测试放在 `web/tests/`，由新工程的 Vitest + Vue Test Utils 执行。浏览器测试使用锁定 Playwright 1.56.1，针对新的生产 `web/dist` 启动自有 Vite preview；默认端口 5242。独立视觉审查拥有 [review/](review/README.md) 和自有服务，不能用本 harness 自查代替其结论。

```sh
npm ci --prefix web
npm run check --prefix web
export NODE_PATH=/workspace/shared/node/account-captcha-ac60fa262f0c44ae-node24.19.0-linux-x64/node_modules
export VUE_TEST_PORT=5242
export VUE_TEST_OUTPUT="$PWD/output/ai/vue-rebuild-test/browser"
node "$NODE_PATH/@playwright/test/cli.js" test --config tests/frontend-rebuild/playwright.config.cjs
```

依赖目录须先与 `tests/account-captcha-web/package-lock.json` 核对；该绝对缓存路径是当前机器入口，不是仓库必需输入。没有缓存时按该锁安装，不能隐式下载 latest。Chromium 默认 `/usr/bin/chromium`，可用 `VUE_TEST_CHROMIUM` 指向明确版本。输出为可重建的 `output/ai/vue-rebuild-test/`，每轮记录 preview 子进程实际关闭结果，原始失败不覆盖。

当前状态：harness 准备中，新组件与路由接口尚未冻结；尚未运行或形成新 Vue 通过结论。后续只记录实际命令、稳定输入、有效结果、失败归因与未验边界。
