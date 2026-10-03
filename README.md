# agenteam

agenteam 是以项目为边界的 AI Agent 协作与执行平台，管理长期项目状态、任务、会议、Agent 执行与人类审核。

## 技术基线

- 前端：Vue 3 + 自定义组件。
- 后端与远程执行节点：Go，单个 Central + 多个 Runner。
- 存储：PostgreSQL / pgvector + MinIO。
- 部署：前端资源嵌入 Central 二进制，Docker Compose 管理 Central 与基础设施。

当前仓库已包含可运行的 Vue 前端基础、公共组件和开发环境 Debug 展示，以及 Go 工程基础、PostgreSQL/pgvector 迁移与事务库、Central 诊断入口和 Runner 独立进程。Central 在真实数据库初始化后只提供诊断，Runner 保持未连接、未认证；完整产品尚未就绪。

## 后端开发

使用 Go 1.27.1；脚本核对精确版本并禁止自动切换工具链。

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/build-go.sh
./bin/agenteam --check-config
./bin/agenteam
```

运行 Central 前须显式配置 `AGENTEAM_CENTRAL_DATABASE_URL` 及部署所需 CA，见 [Central 环境示例](deploy/central.env.example)；程序不会自动加载示例文件。`--check-config` 只验证 D03 配置、不连接数据库；`--help/--version` 无需配置。普通 Go 检查无需 Docker；`AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-postgres.sh` 单独运行真实隔离数据库与 Central 进程验证。

Central 默认监听 `127.0.0.1:8080`，连接、迁移和首次读写检查通过后才开始监听。`/livez` 仅报告进程存活，`/readyz` 始终返回 503；`/diagnostics` 报告定期采样的数据库子状态与其余未绑定能力。`./bin/agenteam-runner` 运行未连接的 Runner，接到 SIGINT/SIGTERM 后退出。配置、日志、停机边界与隔离测试见[后端开发说明](docs/development/backend/README.md)。

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
