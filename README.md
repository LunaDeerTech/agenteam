# agenteam

agenteam 是以项目为边界的 AI Agent 协作与执行平台，管理长期项目状态、任务、会议、Agent 执行与人类审核。

## 技术基线

- 前端：Vue 3 + 自定义组件。
- 后端与远程执行节点：Go，单个 Central + 多个 Runner。
- 存储：PostgreSQL / pgvector + MinIO。
- 部署：前端资源嵌入 Central 二进制，Docker Compose 管理 Central 与基础设施。

当前仓库已包含可运行的 Vue 前端基础、公共组件和开发环境 Debug 展示，以及 Go 工程基础、PostgreSQL/pgvector、对象/Artifact/下载与 typed transfer 库。Central 已真实装配账号、Session、邀请、密码恢复、个人资料/头像/偏好、账户系统设置 HTTP，以及 SMTP/受限恢复日志投递和唯一 `account.mail-enqueue` Outbox handler。System 身份与授权、本人头像对象授权已绑定；Project/Runner 等后续生产适配仍未绑定，Runner 保持未连接、未认证，完整产品尚未就绪。

## 后端开发

使用 Go 1.27.1；脚本核对精确版本并禁止自动切换工具链。

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/build-go.sh
./bin/agenteam --check-config
./bin/agenteam
```

运行 Central 前须显式配置数据库 URL、四用途独立随机 cursor/Secret/download/account keyring、预建私有 MinIO bucket 的 endpoint/凭据及部署所需 CA，见 [Central 环境示例](deploy/central.env.example)。新增必填 `AGENTEAM_CENTRAL_ACCOUNT_KEYRING` 和 `AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG`；后者为专用绝对文件路径，父目录须预建为服务 UID 所有的 0700 目录，文件须为该 UID 的 0600 常规文件且禁止符号链接，文件不存在时由启动创建。spool 默认 `/var/lib/agenteam/object-spool`。`PUBLIC_ORIGIN` 正式部署须为 HTTPS，仅字面 localhost/loopback 允许本地 HTTP，反向代理须保留 canonical Host。

程序不会自动加载示例文件。`--check-config` 验证包括 Account 两项在内的当前配置，不连接、创建目录或打开恢复日志；CLI 仍保留 `scope=d05` 和 D05 help/version 标识。`--help/--version` 无需配置。首次管理员初始密码只向受限恢复日志尝试输出一次，重启不重印；仅 SMTP 未配置时邀请/reset 走该日志，配置后投递失败不回退。部署与敏感日志权限见[账号说明](docs/development/backend/account.md)。普通 Go 检查无需 Docker；真实隔离 PG/MinIO/SMTP/HTTP 与 Account 固定分组命令见[后端验证命令](docs/development/backend/README.md#构建与验证)。

Central 默认监听 `127.0.0.1:8080`，连接、迁移、首次读写检查、共享安全初始化和 Account/Mail 恢复通过后才开始监听。`/api/v1` 的账号、本人资料/头像和账户系统管理接口见 [OpenAPI](api/openapi/account.json)。`/livez` 仅报告进程存活，`/readyz` 仍返回 503；`/diagnostics` 区分真实 Account/System/avatar 绑定与尚未绑定的 Project/Runner 能力。Artifact、通用下载、transfer HTTP 和 Provider/MCP 集成仍待后续模块；正式账号/个人页面属于 D26，系统设置页面属于 D27。`./bin/agenteam-runner` 运行未连接的 Runner，接到 SIGINT/SIGTERM 后退出。配置、日志、Mail 先于 Core 收尾和实际 join/DB 最后关闭边界见[后端开发说明](docs/development/backend/README.md)。

## 前端开发

```sh
npm ci --prefix web
npm run dev --prefix web
```

访问 [Debug 组件展示](http://127.0.0.1:5173/debug)。公共组件独立于展示页，生产构建不包含 Debug。启动、检查及组件使用见[前端开发说明](docs/development/frontend/README.md)。

## 文档

- [系统架构与模块设计](docs/architecture/README.md)
- [仓库结构与代码归属](docs/development/repository-structure.md)
- [正式开发计划与 AI 执行顺序](docs/development/development-plan.md)
- [主线程统筹的开发团队](docs/development/agent-team/README.md)
- [前端设计](docs/frontend-design/README.md)
