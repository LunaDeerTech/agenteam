# 新 Vue 前端

本目录是独立重建的 Vue 工程，已有共享 UI、应用壳、唯一设置壳和40个已定叶子；当前按[账号合同](../docs/development/work-items/frontend-account-rebuild.md)实现四个账号入口与三项个人设置，其余正文保持骨架。旧前端完整保存在 `../web-crape/`；新工程不复用其客户端、Session、权限或业务实现，不提供假登录、业务列表或后端结果。

## 本地运行

Node 要求 `>=24.12.0`。从仓库根目录执行：

```sh
npm ci --prefix web
npm run dev --prefix web
```

默认入口为 `http://127.0.0.1:5173/system/users`；dev 仅绑定 loopback，端口冲突即退出。指定端口使用 `npm run dev --prefix web -- --port 5244`。本工程没有 `/debug`。默认 `VITE_ACCOUNT_ENABLED` 未开启，保留静态框架预览；不会发送账号API。账号实现只在显式开启且浏览器地址为HTTPS或loopback时启用，接口为同源的 `<BASE_URL>api/v1`。

真实账户本地联调使用独立端口，前端owner启动：

```sh
VITE_ACCOUNT_ENABLED=true npm run dev --prefix web -- --port 5245
```

浏览器访问后端owner启动的 `https://localhost:5250`；同源TLS入口把页面转到5245，账号API接长驻Go测试进程内的真实App装配及独立PG/MinIO/SMTP。它不是额外启动的cmd/Central二进制部署；直接打开5245不代表API已配置。真实提供方入口为[run-account-preview.sh](../scripts/run-account-preview.sh)，需后端owner准备固定工具链、MinIO、独立PG和私有运行目录；按[账号harness](../tests/frontend-account-rebuild/README.md)验证nonce/证书，不使用已有外部服务或公开HTTP传递密码。运行组合以[当前状态](../.agent-state/current.md)为准。

## 工程与浏览器检查

```sh
npm run format:check --prefix web
npm run test:unit --prefix web
npm run type-check --prefix web
npm run build --prefix web
```

`npm run check --prefix web` 依次运行格式、组件测试和build（含type-check）。默认false构建用于静态框架兼容检查；本轮真实账号旅程使用 `VITE_ACCOUNT_ENABLED=true` 的5245常驻dev经TLS代理，不能把false dist称为实际业务输入。`format` 会写文件，协作时仅格式化自己获授权的路径。`npm run preview --prefix web` 在 `127.0.0.1:4173` 检查 `web/dist/`，也可用 `-- --port <端口>` 指定独立端口。

账号旅程见[账号harness](../tests/frontend-account-rebuild/README.md)，共享骨架浏览器命令见[框架harness](../tests/frontend-rebuild/README.md)，实际结果与未验范围见[当前状态](../.agent-state/current.md)。旧 fixture、HTML 原型、类型检查与组件测试各有证据边界，不能相互代替。

## 远程 dev 预览

下列HTTP18081配置仅用于已有静态框架预览，保持 `VITE_ACCOUNT_ENABLED=false`，不传输真实账户材料。账号公网采用本地独立TLS5254加frp TCP直透18443，固定PublicOrigin为 `https://154.12.86.233:18443`；证书含IP SAN，临时自签信任提示已告知用户。frps无需配置HTTPS/证书，也不依赖远端Nginx；现443/18081保持。实际公开状态只在[当前状态](../.agent-state/current.md)记录；入口、响应归属、安全Cookie及真实登录旅程通过后才交付。启动、停止和frp命令见[后端预览说明](../docs/development/backend/account.md)。本地5250默认45分钟；独立public5254才可显式24小时，专用依赖证书25小时，实际截止以本轮runtime的 `expires_at` 为准，进程/环境退出可能提前结束。

通过已有 frp 将本地 dev 端口映射到公网时，需为 Vite 显式配置外部 hostname 和端口。例如本轮分配本地 5244、公网 18081：

```sh
AGENTEAM_PREVIEW_HOST=mcpc-terra.lunadeer.cn AGENTEAM_PREVIEW_PORT=18081 npm run dev --prefix web -- --port 5244
```

`AGENTEAM_PREVIEW_HOST` 是不带协议和端口的 hostname，加入 Vite allowedHosts 并作为 HMR host；`AGENTEAM_PREVIEW_PORT` 是外部 HTTP/WebSocket 端口。当前配置使用 `ws`，适用于本轮 HTTP frp 入口；本地监听仍是 loopback。这两个变量不会自动启动隧道，也不配置 API 代理。

本环境 frp 方式遵循 `/workspace/AGENTS.md` 和 `/workspace/agenteam-onboarding/frp/wss-start-instructions.md`：显式连接 `mcpc-terra.lunadeer.cn:443`，唯一映射名，不占用其他服务端口，保留证书验证。实际映射、进程、启动日志和可用性由服务owner在[当前状态](../.agent-state/current.md)维护。需保留的frp应在独立会话启动，stdin脱离tool、输出写本轮日志；核验非Z状态、PID起始ticks/会话身份以及实际转发，`kill(pid, 0)` 或旧启动日志不能证明仍运行。恢复后再核公网返回内容与页面交互，同时单列HMR和证书代理限制；用户连接错误不能未检查转发进程就归因环境。公网页面验证通过后向用户提供地址，并保留服务供体验；HMR 不可用时说明手动刷新方式，不在仓库记录凭据。

## 源码入口

路由和设置数据在 `src/router/routes.ts`，未实现正文共用 `src/views/SkeletonPage.vue`。新账户传输、会话、命令、链接与校验位于 `src/account/`，七页使用本轮独立实现；登录采用用户要求的居中轻边框面板，设置页保持开放表单。系统/个人/项目设置通过 `settingsScope` 使用 `src/layouts/SettingsShell.vue`；共享控件只从 `src/components/ui/index.ts` 导出。主题和 token 均由新工程自行实现。

[前端开发说明](../docs/development/frontend/README.md)维护范围与代码归属，[组件合同](../docs/development/frontend/components.md)维护 props、emits、插槽和布局职责。[可运行 HTML 原型](../docs/frontend-design/redesign/prototype/README.md)是视觉参照，其合成内容不会自动成为新 Vue 的业务页面。
