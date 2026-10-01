# 组件展示已迁移

旧独立 HTML 展示已由 Vue 开发环境的 Debug 页面替代，旧 HTML、脚本和重复 CSS 已移除。

公共组件位于 `web/src/components/ui/`，应用骨架位于 `web/src/components/layout/`；Debug 仅引用组件，数据与展示逻辑位于 `web/src/views/debug/`。后续正式页面使用同一套公共组件。

在仓库根目录运行 `npm ci --prefix web`、`npm run dev --prefix web`，访问 [Debug](http://127.0.0.1:5173/debug)。默认跟随系统，主题选择不写入账号设置。Debug 不进入生产构建。

启动和使用见[前端开发说明](../../development/frontend/README.md)，接口见[公共组件说明](../../development/frontend/components.md)，参数见[样式规范](../styles/README.md)，验证见[验证记录](../../development/frontend/verification.md)。
