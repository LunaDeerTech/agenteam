# agenteam

agenteam 是以项目为边界的 AI Agent 协作与执行平台，管理长期项目状态、任务、会议、Agent 执行与人类审核。

## 技术基线

- 前端：Vue 3 + 自定义组件。
- 后端与远程执行节点：Go，单个 Central + 多个 Runner。
- 存储：PostgreSQL / pgvector + MinIO。
- 部署：前端资源嵌入 Central 二进制，Docker Compose 管理 Central 与基础设施。

当前仓库包含设计文档和代码目录骨架，尚未初始化可运行工程或依赖清单。

## 文档

- [系统架构与模块设计](docs/architecture/README.md)
- [仓库结构与代码归属](docs/development/repository-structure.md)
- [前端设计](docs/frontend-design/README.md)
