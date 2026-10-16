# agenteam

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/logos/agenteam-horizontal-dark.svg" />
  <img src="docs/logos/agenteam-horizontal-light.svg" alt="agenteam" width="320" height="84" />
</picture>

agenteam 是以项目为边界的 AI Agent 协作与执行平台，管理长期项目状态、任务、会议、Agent 执行与人类审核。

当前目标与里程碑见[开发计划](docs/development/development-plan.md#当前开发目标与里程碑)，全局开发状态见[任务台账](docs/development/agent-team/tasks.md)，跨设备继续当前任务见[自动保存与接续](.agent-state/README.md)，AI 协作规则见[团队流程](docs/development/agent-team/README.md)。README 保留项目介绍与使用入口，逐轮进度不再在此重复维护。

## 技术基线

- 前端：Vue 3 + 自定义组件。
- 后端与远程执行节点：Go，单个 Central + 多个 Runner。
- 存储：PostgreSQL / pgvector + MinIO。
- 本地开发：Docker Compose 管理 Central 与基础设施，nginx 提供前端并反代 API；生产前端嵌入尚未验收。

仓库包含新 Vue 前端骨架、共享组件及完整归档的 `web-crape/`，以及 Go Central/Runner、PostgreSQL/pgvector 和对象存储相关模块。已交付能力、尚未绑定端口与验收限制统一见任务台账；完整产品尚未就绪。

## 后端开发

### 本地 Docker Compose 开发环境

使用启用 BuildKit 的 Docker Engine 与 Docker Compose v2，在完整仓库根目录准备本地配置：

```sh
cp .env.example .env
```

`.env` 的本机默认配置无需修改：

```dotenv
COMPOSE_PROJECT_NAME=agenteam-dev
AGENTEAM_DEV_MODE=true
AGENTEAM_DEV_PORT=8080
AGENTEAM_DEV_DATA_DIR=./.dev
```

`AGENTEAM_DEV_MODE` 控制开发模式，`AGENTEAM_DEV_PORT` 设置网页端口；网页端口发布到宿主所有网卡，无需配置监听地址、public origin 或允许网段。`COMPOSE_PROJECT_NAME` 设置容器项目名，`AGENTEAM_DEV_DATA_DIR` 设置全部开发数据的宿主目录。相对数据路径以项目根目录解析，也支持绝对路径和含空格的路径，例如 `AGENTEAM_DEV_DATA_DIR='./local data'`。镜像版本、开发账号、内部密码和运行 UID/GID 均由开发配置自动处理，不需要额外初始化命令。

一条命令构建并启动：

```sh
docker compose -f docker-compose.dev.yaml up -d --build --wait --wait-timeout 1200
```

默认成功后，本机访问 **http://localhost:8080**，其他机器访问 **http://服务器IP:8080**；修改端口后使用对应端口即可。开发代理保留同源 `/api/v1` 入口；当前构建的是新 `web/` 空骨架，尚未连接后端账号与业务，不能沿下方归档版本的登录结果推定新工程组合已验收。数据库和 MinIO 均不公开宿主端口。Compose 自动读取根 `.env`，已有 shell 环境变量优先。首次下载镜像和 Go/npm 依赖需要网络与足够磁盘空间。Docker `vfs` 驱动还会为容器文件系统占用空间：此前四服务实测额外约 3GB，不含数据目录内缓存，不是所有环境的峰值或上限。

从其他机器直接通过服务器 IP 测试时，在服务器的 `.env` 中配置：

```dotenv
AGENTEAM_DEV_MODE=true
AGENTEAM_DEV_PORT=8080
```

然后在浏览器打开 `http://服务器IP:8080`，不需要配置证书。

`AGENTEAM_DEV_MODE` 通过 Compose 传给后端的 `AGENTEAM_CENTRAL_DEV_MODE`。这份开发 Compose 默认开启，允许通过任意服务器 IP 或域名访问 HTTP，跳过 Host/Origin 和 Fetch Metadata 来源匹配；登录、权限与 CSRF token 检查仍启用。设为 `false` 时恢复后端严格校验，本 Compose 的访问地址须匹配既有的 `http://localhost:<端口>`，但网页端口仍发布到宿主所有网卡。独立启动 Central 时开发模式默认关闭；正式部署保持关闭并使用 HTTPS。自动生成的邀请和找回密码链接仍采用既有的 localhost 基址。

远程 HTTP 支持本开发入口和登录，但不会获得浏览器安全上下文。依赖 `crypto.subtle` 的 Artifact 创建、查询、取消摘要流程，以及 Knowledge 子树删除预览校验仍需 HTTPS；本机 `localhost`/loopback 的安全上下文访问不受此限制。

只有 `database`（PostgreSQL/pgvector）、`minio`、`app`（Central）和 `web`（前端与 API 反代）四个服务。数据库使用原镜像入口初始化；基础服务的小启动脚本仅装载开发凭据，不执行改密或业务 SQL。新环境使用固定的内部开发凭据，app 将它们保存在数据目录的 `config/` 下；已有凭据文件始终优先，原随机凭据不会变成默认值。基础服务可独立用相同默认值首次启动，无初始化容器或跨服务初始化等待协议。

app 入口仅以 root 准备本地目录、五组独立随机密钥和固定机器标识，然后立即降为非 root 用户。已有私有密钥或 spool 的文件所有者优先；新目录采用源码目录的非 root 所有者，源码由 root 持有时使用 UID/GID 1000。不会递归修改数据库或 MinIO 数据的所有者。密钥和机器标识只创建一次，损坏、不一致或已有数据缺少原凭据时会明确失败，不自动重生或轮换。

**bucket 是 MinIO 内存放对象的容器，不是额外服务。** Central 仅在新数据库的存储身份仍为 `reserved` 时创建缺失的配置 bucket；已确认身份的旧数据库缺桶会拒绝启动。已有 bucket 仍须通过完整安全检查，不改策略、版本管理或生命周期规则，也不依赖 mc。

工具链镜像包含 Go 1.27.1 / Node 24.12.0，不打包项目源码；未变的镜像构建会复用缓存。app 每次启动均从只读挂载的当前源码编译 Central，安装或复用锁定的唯一前端依赖目录并构建页面，再运行真实入口。编译失败停止应用，页面返回 503，不运行旧二进制。工具链未变时，修改源码只需：

```sh
docker compose -f docker-compose.dev.yaml restart app
```

MinIO 服务也由同一条 `up --build` 命令构建并缓存本地镜像，不再拉取 `quay.io/minio/minio` 或 `minio/minio`。构建从官方 GitHub Release 下载固定的 `RELEASE.2025-09-07T16-13-09Z` Linux 二进制，并按仓库固定的官方 SHA-256 校验；支持 amd64、arm64 和 ppc64le，其他架构直接拒绝。入口脚本和 AGPL 许可证取自对应官方源码提交 `07c3a429bfed433e49018cb0f78a52145d4bedeb`，运行载体复用 web 所需的固定官方轻量镜像，容器仅启动 MinIO。首次构建仍需访问 GitHub 和基础镜像仓库；校验或下载失败直接停止，不切换其他来源，不是离线安装或 MinIO 版本升级。后续镜像未变时复用缓存，app 重启不重建 MinIO。

`.env` 改动应重新执行上面的 `up` 命令，重建配置发生变化的容器；`restart` 不重新读取容器配置。反代保留 Host/Origin 并跟随重建后的 app。更换数据目录会选择另一套完整数据，不自动搬迁或复制旧目录；独立 Compose 项目应使用不同数据目录，不要同时启动两套容器写同一目录。

数据库、对象存储、配置和密钥、spool/ProcessGuard、Account 恢复日志、Go/npm 缓存、前端依赖、构建页面和二进制全部位于 `AGENTEAM_DEV_DATA_DIR`（默认 `.dev/`）。默认目录已被 Git 忽略；自定义目录建议放在仓库外，在仓库内时须自行加入本地 Git 忽略规则，避免提交数据或凭据。首次管理员邮箱为 `admin@mail.com`，随机口令仅写入该目录的 `account-recovery/recovery.jsonl`，请在本机查看，不粘贴进普通日志。

从前一版 Compose 升级时，保留整个原 `.dev`，按上述配置整理根 `.env` 即可。前版已经把数据库与 MinIO 凭据写入 `config/database-password`、`minio-user`、`minio-password`；现在直接复用这些私有文件，不再从 `.env` 读取密码。旧 `central.env` 中的五组密钥会严格解析并保留，不作为 shell 执行。若原凭据文件丢失，先从原备份恢复，不能填开发默认值冒充旧数据库密码。旧 `.env` 不应在确认原凭据完整前丢弃。

```sh
docker compose -f docker-compose.dev.yaml ps
docker compose -f docker-compose.dev.yaml logs -f app
docker compose -f docker-compose.dev.yaml down
```

普通 `down` 保留数据目录，下一次 `up` 继续使用。只有明确丢弃全部本地数据时，才在停止容器后整体删除对应目录；不要单独删除密钥或某一侧存储，也不要把整份数据复制到开发 worktree。

若网络需要额外受信 CA，可先构建同一工具链镜像，再执行正常启动命令：

```sh
docker build --no-cache --secret id=dev_ca,src=/path/to/trusted-ca.crt -f Dockerfile.dev -t agenteam-dev-toolchain:go1.27.1-node24.12.0 .
docker build --secret id=dev_ca,src=/path/to/trusted-ca.crt --target minio -f Dockerfile.dev -t agenteam-dev-minio:RELEASE.2025-09-07T16-13-09Z-07c3a429bfed .
```

使用不含私钥的 PEM 受信 CA；该步骤添加系统信任，不关闭 TLS 校验。普通环境不需要此步骤。

构建使用 BuildKit 内置 Dockerfile 前端，不再额外拉取 `docker/dockerfile:1`。需要支持 `RUN --mount=type=secret` 的前端（Dockerfile 1.2 或更新），不支持旧版 legacy builder；建议使用当前受支持的 Docker 版本。`app` 和 `minio` 的本地镜像均由 Compose 构建，不尝试从仓库拉取这些本地标签。Go、Node、nginx、PostgreSQL 基础镜像及官方 GitHub 资产仍需要网络；本修改不解决 Docker Hub 的 DNS、认证或网络连接问题。

官方社区版现采用源码分发，但上述历史 GitHub Release 的官方二进制资产仍可获取。本环境对同版本的官方 Quay 和 Docker Hub manifest 在匿名鉴权后仍收到访问拒绝；这不证明该 release 不存在，也不能代替用户本机错误诊断。当前 MinIO 构建已在 Linux/amd64 完成官方资产完整下载、固定 SHA-256 校验及镜像构建，实际版本、源码提交、原入口和许可证均已确认；其他支持架构尚未实际运行。本环境构建使用上述可选受信 CA。

归档前端版本的开发组合曾使用新构建的 MinIO 镜像和标准 `up -d --build --wait --wait-timeout 1200` 命令，复用原有数据及构建缓存，通过四服务启动、真实浏览器管理员登录、Session 与 CSRF 校验，Central 以非 root 运行，没有镜像或入口覆盖。重启 app 后 Go 与前端重新构建，原数据库、三份凭据、五组密钥及机器标识保持不变；最后正常停止并 `down`，MinIO 收到 SIGTERM 后以状态 0 退出，容器和网络均已清退，原数据保留。此前源构建和首次启动曾因本环境磁盘空间保护主动取消，不计为通过。

开发内网数据库与对象存储使用正式 `TLS_MODE=disable` 配置，不改变模型出站、DNS 或权限规则。此开发组合不启用 Execution Runtime，不发起模型请求，不新增 Runner 或 SMTP。nginx 开发托管不代表生产 SPA 嵌入完成；`/livez` 检查真实监听进程，当前产品的 `/readyz` 未绑定状态仍保留。

使用 Go 1.27.1；脚本核对精确版本并禁止自动切换工具链。

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/build-go.sh
./bin/agenteam --check-config
./bin/agenteam
```

运行 Central 前须显式配置数据库 URL、五用途独立随机 cursor/Secret/download/account/Knowledge confirmation keyring、私有 MinIO bucket 的 endpoint/凭据及部署所需 CA，见 [Central 环境示例](deploy/central.env.example)。首次 reserved 存储身份可创建缺桶，已有 confirmed 身份缺桶拒绝。新增必填 `AGENTEAM_CENTRAL_ACCOUNT_KEYRING` 和 `AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG`；后者为专用绝对文件路径，父目录须预建为服务 UID 所有的 0700 目录，文件须为该 UID 的 0600 常规文件且禁止符号链接，文件不存在时由启动创建。spool 默认 `/var/lib/agenteam/object-spool`。`PUBLIC_ORIGIN` 正式部署须为 HTTPS，仅字面 localhost/loopback 允许本地 HTTP，反向代理须保留 canonical Host。

程序不会自动加载示例文件。`--check-config` 验证包括 Account 两项在内的当前配置，不连接、创建目录或打开恢复日志；CLI 仍保留 `scope=d05` 和 D05 help/version 标识。`--help/--version` 无需配置。首次管理员初始密码只向受限恢复日志尝试输出一次，重启不重印；仅 SMTP 未配置时邀请/reset 走该日志，配置后投递失败不回退。部署与敏感日志权限见[账号说明](docs/development/backend/account.md)。普通 Go 检查无需 Docker；真实隔离 PG/MinIO/SMTP/HTTP 与 Account 固定分组命令见[后端验证命令](docs/development/backend/README.md#构建与验证)。

Central 默认监听 `127.0.0.1:8080`，连接、迁移、首次读写检查、共享安全初始化和 Account/Mail 恢复通过后才开始监听。`/api/v1` 的账号、本人资料/头像和账户系统管理接口见 [OpenAPI](api/openapi/account.json)。`/livez` 仅报告进程存活，`/readyz` 仍返回 503；`/diagnostics` 报告各能力的实际绑定状态；接口范围及后续接入以相应 OpenAPI、开发说明和任务台账为准。`./bin/agenteam-runner` 运行未连接的 Runner，接到 SIGINT/SIGTERM 后退出。配置、日志、Mail 先于 Core 收尾和实际 join/DB 最后关闭边界见[后端开发说明](docs/development/backend/README.md)。

## 前端开发

```sh
npm ci --prefix web
npm run dev --prefix web
```

访问 [系统设置骨架](http://127.0.0.1:5173/system/users)。新 `web/` 从零实现共享 UI、AppShell、唯一 SettingsShell 与已定空路由；旧 `web-crape/` 仅为归档。当前没有 Debug、认证/API 或业务页面接入。启动、检查与远程预览配置见 [web 运行说明](web/README.md)，范围和组件使用见[前端开发说明](docs/development/frontend/README.md)。

## 文档

- [系统架构与模块设计](docs/architecture/README.md)
- [仓库结构与代码归属](docs/development/repository-structure.md)
- [正式开发计划与 AI 执行顺序](docs/development/development-plan.md)
- [主线程统筹的开发团队](docs/development/agent-team/README.md)
- [当前任务、阻塞与接续步骤](docs/development/agent-team/tasks.md)
- [前端设计](docs/frontend-design/README.md)
