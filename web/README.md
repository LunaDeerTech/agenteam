# 新 Vue 前端

本目录是独立重建的 Vue 工程，当前实现共享 UI、应用壳、唯一设置壳和 40 个已定叶子的标题/空正文。旧前端完整保存在 `../web-crape/`；新工程不复用其客户端、Session、权限或业务实现，不提供假登录、业务列表或后端结果。

## 本地运行

Node 要求 `>=24.12.0`。从仓库根目录执行：

```sh
npm ci --prefix web
npm run dev --prefix web
```

默认入口为 `http://127.0.0.1:5173/system/users`；dev 仅绑定 loopback，端口冲突即退出。指定端口使用 `npm run dev --prefix web -- --port 5244`。本轮没有 `/debug`、API 代理或后端连接；账号路由也是空骨架。

## 工程与浏览器检查

```sh
npm run format:check --prefix web
npm run test:unit --prefix web
npm run type-check --prefix web
npm run build --prefix web
```

`npm run check --prefix web` 依次运行格式、组件测试和 build（含 type-check）。`format` 会写文件，协作时仅格式化自己获授权的路径。`npm run preview --prefix web` 在 `127.0.0.1:4173` 检查 `web/dist/`，也可用 `-- --port <端口>` 指定独立端口。

真实浏览器命令和资源边界见[新工程 harness](../tests/frontend-rebuild/README.md)，实际结果与未验范围见[当前状态](../.agent-state/current.md)。旧 fixture、HTML 原型、类型检查与组件测试各有证据边界，不能相互代替。

## 远程 dev 预览

通过已有 frp 将本地 dev 端口映射到公网时，需为 Vite 显式配置外部 hostname 和端口。例如本轮分配本地 5244、公网 18081：

```sh
AGENTEAM_PREVIEW_HOST=mcpc-terra.lunadeer.cn AGENTEAM_PREVIEW_PORT=18081 npm run dev --prefix web -- --port 5244
```

`AGENTEAM_PREVIEW_HOST` 是不带协议和端口的 hostname，加入 Vite allowedHosts 并作为 HMR host；`AGENTEAM_PREVIEW_PORT` 是外部 HTTP/WebSocket 端口。当前配置使用 `ws`，适用于本轮 HTTP frp 入口；本地监听仍是 loopback。这两个变量不会自动启动隧道，也不配置 API 代理。

本环境 frp 方式遵循 `/workspace/AGENTS.md` 和 `/workspace/agenteam-onboarding/frp/wss-start-instructions.md`：显式连接 `mcpc-terra.lunadeer.cn:443`，唯一映射名，不占用其他服务端口，保留证书验证。实际映射、进程、启动日志和可用性由服务 owner 在[当前状态](../.agent-state/current.md)维护；恢复环境必须重新启动并核对公网返回内容、页面交互和 HMR。验证通过后再向用户提供地址，并保留服务供体验；不在仓库记录凭据。

## 源码入口

路由和设置数据在 `src/router/routes.ts`，所有空正文共用 `src/views/SkeletonPage.vue`。系统/个人/项目设置通过 `settingsScope` 使用 `src/layouts/SettingsShell.vue`；共享控件只从 `src/components/ui/index.ts` 导出。主题和 token 均由新工程自行实现。

[前端开发说明](../docs/development/frontend/README.md)维护范围与代码归属，[组件合同](../docs/development/frontend/components.md)维护 props、emits、插槽和布局职责。[可运行 HTML 原型](../docs/frontend-design/redesign/prototype/README.md)是视觉参照，其合成内容不会自动成为新 Vue 的业务页面。
