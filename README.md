# agenteam

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/logos/agenteam-horizontal-dark.svg" />
  <img src="docs/logos/agenteam-horizontal-light.svg" alt="agenteam" width="320" height="84" />
</picture>

agenteam 是以项目为边界的 AI Agent 协作与执行平台，管理长期项目状态、任务、会议、Agent 执行与人类审核。

全局开发状态见[任务台账](docs/development/agent-team/tasks.md)，跨设备继续当前任务见[自动保存与接续](.agent-state/README.md)，AI 协作规则见[团队流程](docs/development/agent-team/README.md)。README 保留项目介绍与使用入口，逐轮进度不再在此重复维护。

## 技术基线

- 前端：Vue 3 + 自定义组件。
- 后端与远程执行节点：Go，单个 Central + 多个 Runner。
- 存储：PostgreSQL / pgvector + MinIO。
- 部署：前端资源嵌入 Central 二进制，Docker Compose 管理 Central 与基础设施。

仓库包含 Vue 前端、共享组件与开发环境 Debug 展示，以及 Go Central/Runner、PostgreSQL/pgvector 和对象存储相关模块。已交付能力、尚未绑定端口与验收限制统一见任务台账；完整产品尚未就绪。

## 后端开发

### 本地 Docker Compose 开发环境

安装 Docker Engine 与 Docker Compose v2，在完整仓库根目录执行：

```sh
docker compose -f docker-compose.dev.yaml build app
docker compose -f docker-compose.dev.yaml up -d
docker compose -f docker-compose.dev.yaml logs -f app
```

若开发网络需要额外受信 CA，可将上面的镜像构建命令换为 `docker build --no-cache --secret id=dev_ca,src=/path/to/trusted-ca.crt -f Dockerfile.dev -t agenteam-dev-toolchain:go1.27.1-node24.12.0 .`。输入应为 PEM 格式的受信 CA 证书，不含私钥；它会加入开发镜像的系统信任库，供包管理器、Go 和 Node/npm 校验证书。`--no-cache` 确保证书变更实际应用。普通环境无需该 secret，仍使用默认信任库。

首次构建的是 Go 1.27.1 / Node 24.12.0 工具链镜像。应用每次启动都从挂载的当前源码编译 Central、安装或复用锁定的前端依赖并构建页面，再运行真实 Central 入口；首次下载镜像和依赖需要网络与足够磁盘空间。编译失败会停止应用，页面返回 503，不运行旧二进制。工具链或 `Dockerfile.dev` 未变时，无需重建镜像，修改源码后执行：

```sh
docker compose -f docker-compose.dev.yaml restart app
```

看到 Central 的监听日志后访问 **http://localhost:8080**。只有 nginx 的此端口绑定宿主机 loopback；数据库和 MinIO 不公开宿主端口。需要换端口时，在上述命令前设置 `export AGENTEAM_DEV_PORT=18080`，随后访问 `http://localhost:18080`，并用 `up -d` 更新容器配置。须使用该精确地址，Host/Origin 仍由原 HTTP 安全边界校验。页面由开发用 nginx 提供，API 原样转发到 Central；这不是生产 SPA 嵌入的验收。`/livez` 用于已监听进程的健康检查，当前产品的 `/readyz` 仍返回 503。

所有持久数据与开发产物位于已忽略的根目录 `.dev/`：PostgreSQL、MinIO、五组独立随机密钥、固定机器标识、Object spool/ProcessGuard、Account 恢复日志、Go/npm 缓存、唯一前端依赖目录、页面和二进制。初始化只在首次生成密钥，之后保留复用；原 Central 自行执行内嵌迁移及安全初始化，不另跑 SQL 种子。开发内网的数据库和对象存储显式使用各自正式 `TLS_MODE=disable` 配置，不改变模型出站、DNS 或应用权限规则，也不启用 Execution Runtime 或发起模型请求。

容器内应用默认使用 UID/GID 1000；其他普通用户可先设置 `AGENTEAM_DEV_UID` 和 `AGENTEAM_DEV_GID` 为自己的正整数 UID/GID。初始化容器仅负责目录权限与本地随机配置。首次管理员邮箱为 `admin@mail.com`，随机口令仅记录在 `.dev/account-recovery/recovery.jsonl`；请在本机查看并妥善保护该文件，不要粘贴到普通日志或提交仓库。`.dev/config/central.env` 和数据库、对象数据须配套保留，不能只重置其中一项。

```sh
docker compose -f docker-compose.dev.yaml ps
docker compose -f docker-compose.dev.yaml logs --tail=100 app
docker compose -f docker-compose.dev.yaml down
```

普通 `down` 只停止并移除本组容器和网络，保留 `.dev/`，下次 `up -d` 可继续使用。不要将 `.dev/` 复制进其他工作树；为独立开发环境选择各自的 Compose project name 和数据目录，避免两个 Central 同时使用同一 spool。此配置没有新增 Runner、SMTP 或对外上传/模型能力，功能范围仍以当前任务台账为准。

当前已验证工具链镜像构建、重复初始化保留原配置，以及宿主构建的 Central 配置校验。本环境的 MinIO 客户端镜像拉取被拒，尚未完成整组启动、浏览器访问和应用重启验收。

使用 Go 1.27.1；脚本核对精确版本并禁止自动切换工具链。

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/build-go.sh
./bin/agenteam --check-config
./bin/agenteam
```

运行 Central 前须显式配置数据库 URL、四用途独立随机 cursor/Secret/download/account keyring、预建私有 MinIO bucket 的 endpoint/凭据及部署所需 CA，见 [Central 环境示例](deploy/central.env.example)。新增必填 `AGENTEAM_CENTRAL_ACCOUNT_KEYRING` 和 `AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG`；后者为专用绝对文件路径，父目录须预建为服务 UID 所有的 0700 目录，文件须为该 UID 的 0600 常规文件且禁止符号链接，文件不存在时由启动创建。spool 默认 `/var/lib/agenteam/object-spool`。`PUBLIC_ORIGIN` 正式部署须为 HTTPS，仅字面 localhost/loopback 允许本地 HTTP，反向代理须保留 canonical Host。

程序不会自动加载示例文件。`--check-config` 验证包括 Account 两项在内的当前配置，不连接、创建目录或打开恢复日志；CLI 仍保留 `scope=d05` 和 D05 help/version 标识。`--help/--version` 无需配置。首次管理员初始密码只向受限恢复日志尝试输出一次，重启不重印；仅 SMTP 未配置时邀请/reset 走该日志，配置后投递失败不回退。部署与敏感日志权限见[账号说明](docs/development/backend/account.md)。普通 Go 检查无需 Docker；真实隔离 PG/MinIO/SMTP/HTTP 与 Account 固定分组命令见[后端验证命令](docs/development/backend/README.md#构建与验证)。

Central 默认监听 `127.0.0.1:8080`，连接、迁移、首次读写检查、共享安全初始化和 Account/Mail 恢复通过后才开始监听。`/api/v1` 的账号、本人资料/头像和账户系统管理接口见 [OpenAPI](api/openapi/account.json)。`/livez` 仅报告进程存活，`/readyz` 仍返回 503；`/diagnostics` 报告各能力的实际绑定状态；接口范围及后续接入以相应 OpenAPI、开发说明和任务台账为准。`./bin/agenteam-runner` 运行未连接的 Runner，接到 SIGINT/SIGTERM 后退出。配置、日志、Mail 先于 Core 收尾和实际 join/DB 最后关闭边界见[后端开发说明](docs/development/backend/README.md)。

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
- [当前任务、阻塞与接续步骤](docs/development/agent-team/tasks.md)
- [前端设计](docs/frontend-design/README.md)
