# agenteam

agenteam 是以项目为边界的 AI Agent 协作与执行平台，管理长期项目状态、任务、会议、Agent 执行与人类审核。

## 技术基线

- 前端：Vue 3 + 自定义组件。
- 后端与远程执行节点：Go，单个 Central + 多个 Runner。
- 存储：PostgreSQL / pgvector + MinIO。
- 部署：前端资源嵌入 Central 二进制，Docker Compose 管理 Central 与基础设施。

当前仓库已包含可运行的 Vue 前端基础、公共组件和开发环境 Debug 展示；后端仍为目录骨架。

## 前端开发

```sh
npm ci --prefix web
npm run dev --prefix web
```

访问 [Debug 组件展示](http://127.0.0.1:5173/debug)。公共组件独立于展示页，生产构建不包含 Debug。启动、检查及组件使用见[前端开发说明](docs/development/frontend/README.md)。

## 文档

- [系统架构与模块设计](docs/architecture/README.md)
- [仓库结构与代码归属](docs/development/repository-structure.md)
- [串行子 agent 开发团队](docs/development/agent-team/README.md)
- [前端设计](docs/frontend-design/README.md)
