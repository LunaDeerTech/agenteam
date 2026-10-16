# 新 Vue 基础验证

本入口验证新 `web/` 在账号功能关闭的预览构建中的共享 UI、AppShell、唯一 SettingsShell、仍约定为空的路由与本地主题。账号阶段已实现的四个公开入口和三个个人设置页改为验证明确的预览说明与禁用提交，不能继续要求其正文为空。真实认证、会话、权限与业务 API 验证见 [账号 HTTPS harness](../frontend-account-rebuild/README.md)，不在本 HTTP 预览输入凭据。新客户端从零实现，`web-crape/` 仅为归档。原型及旧 Vue 的检查不代证本轮结果，自动检查也不代替独立整页视觉审查。

组件测试位于 `web/tests/`，使用新工程锁定的 Vitest 与 Vue Test Utils。浏览器测试使用 `tests/account-captcha-web/` 锁定的 Playwright **1.56.1**。先按锁安装依赖并构建当前新工程，然后执行：

```sh
npm ci --prefix web
npm ci --prefix tests/account-captcha-web
npm run test:unit --prefix web
VITE_ACCOUNT_ENABLED=false npm run build --prefix web
export NODE_PATH="$PWD/tests/account-captcha-web/node_modules"
export VUE_TEST_PORT=5242
# 每轮用独立目录，保留失败现场；不要并行共享此端口或目录。
export VUE_TEST_OUTPUT="$PWD/output/ai/frontend-vue-rebuild-test/browser-run-01"
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-rebuild/playwright.config.cjs --list
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-rebuild/playwright.config.cjs
```

从仓库根执行。Node 要求 `>=24.12.0`；本轮实际 Node `24.19.0`、Chromium `151.0.7922.173`。默认浏览器 `/usr/bin/chromium`，可通过 `VUE_TEST_CHROMIUM` 指定已核版本；不要用会隐式下载 latest 的 `npx`。已有相同源码与依赖的有效检查可复用，集成 owner 已完成构建时不需要重复工程全套检查。

`preview-server.mjs` 将现有生产 `web/dist/` 复制到本轮输出目录，并把 `web/tests/browser/` 的共享组件夹具编译到预览根的 `/__test__/`。夹具只通过公开组件接口展示测试输入，不注册产品路由、不改写生产 dist、不进入默认产品构建。预览服务使用严格独占端口，Playwright 结束时发送 SIGTERM；`preview-lifecycle.json` 记录原预览子进程的 spawn/close、退出码与信号。测试结果为 `results.json`，失败时保留 screenshot/trace；检查实际 close 和端口状态后再报告资源退出。

如独立审查者需要同一夹具，可分配另一端口与输出目录，手动运行以下命令并在审查后 Ctrl+C，核对生命周期记录：

```sh
VUE_TEST_PORT=5243 VUE_TEST_OUTPUT="$PWD/output/ai/frontend-vue-rebuild-review/component-preview" \
  node tests/frontend-rebuild/preview-server.mjs
# 本机审查入口：http://127.0.0.1:5243/__test__/
```

覆盖范围：

- `foundation.spec.cjs`：真实生产 `/system/users` 导航与返回、菜单键盘操作、主题持久化和系统配色变化。
- `routes.spec.cjs`：40 个已定叶子的直达标题；七个已实现账号页的禁用预览，其余未讨论叶子仍严格要求空正文；25 个设置叶子共用一个设置壳、可选详情参数、范围默认跳转、保留路径、窄屏目录切换及项目导航局部滚动。
- `controls.spec.cjs`：Input 描述与输入；Select 禁用项、键盘选择、取消和 typeahead；Tabs 键盘选择；Menu 关闭与外部焦点；原生 Dialog 焦点循环、背景不可交互、内层浮层与嵌套 Dialog、滚动锁释放、390px 深色与 reduced motion、长选项和遮罩点击/拖拽。

原生 200% 缩放由独立视觉审查者另行执行，不属于本 Playwright 测试条目。原生 Chromium profile 的 zoom level 方法沿用 [原型交互检查](../../docs/frontend-design/redesign/prototype/review/interact.mjs)；须同时核对 inner/outer 尺寸、DPR 与根 CSS zoom，不能用 deviceScaleFactor、CSS zoom 或 pinch 代证浏览器页面缩放。

基础阶段历史证据：当时全部组件测试 14/14 实际通过（既有 10 项加 Input/Select/Tabs 4 项）；浏览器首链 1 项、控件 4 项及路由/目录 7 项分轮通过。路由首轮 6/7，剩余一项最初错误假定 390px 的项目导航必定溢出；实际 7 项全部容纳且页面无横溢，改用 320px 触发真实局部横滚后定向 1/1 通过。原始失败与定向结果分别保留在当轮输出目录，未删除失败或重跑无关场景。那些轮次的原 preview 子进程 close 已收集，exitCode 143，确认当时 5242 已无监听；后续运行状态须另核。

本入口只证明禁用账号的预览和已实现控件。后续账号阶段的真实旅程、安全边界、完整工程检查与服务状态由账号 harness 和当前工作项记录，不能用上述基础阶段数字代替。原生缩放及整页视觉结论由独立审查者另行报告，不能把自动检查通过写成用户已接受设计。
