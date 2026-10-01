# 仓库结构与代码归属

agenteam 使用一个仓库维护 Central、Runner、前端及配套文档。目录以稳定的程序边界和职责划分，业务模块内部结构在实际实现时逐步增加。

技术与部署边界以[系统架构](../architecture/README.md)为准。

## 目录骨架

```text
agenteam/
├── cmd/
│   ├── agenteam/
│   └── agenteam-runner/
├── internal/
│   ├── central/
│   ├── runner/
│   └── runnerprotocol/
├── web/
│   └── src/
├── db/
│   └── migrations/
├── deploy/
├── scripts/
├── tests/
├── docs/
├── README.md
└── .gitignore
```

## 目录职责

| 目录 | 职责 |
| --- | --- |
| `cmd/agenteam/` | Central 程序入口，负责启动和接入应用生命周期。业务实现放在 `internal/central/`。 |
| `cmd/agenteam-runner/` | Runner 程序入口，负责启动和接入节点生命周期。执行实现放在 `internal/runner/`。 |
| `internal/central/` | Central 的项目业务、调度、Agent 执行、API 与平台能力，作为一个整体后端运行。 |
| `internal/runner/` | Runner 的连接管理与文件、命令、进程、桌面等远程执行能力。 |
| `internal/runnerprotocol/` | Central 与 Runner 共享的通信契约，不承载 Central 业务实现。 |
| `web/src/` | Vue 前端源码；公共组件、应用骨架、主题与开发环境 Debug 已实现。 |
| `db/migrations/` | Central 的全局数据库迁移序列，不按业务模块建立独立迁移序列。 |
| `deploy/` | 部署配置与相关示例，后续承载 Docker Compose 等部署文件。 |
| `scripts/` | 开发、构建与维护脚本。 |
| `tests/` | 跨模块集成测试与端到端测试。Go 单元测试随被测源码放置。 |
| `docs/` | 架构、前端设计与开发说明；保留现有文档组织。 |

## 依赖与演进约定

- Central 与 Runner 后续共用根目录的一个 Go module；前端在 `web/` 独立管理依赖。
- Runner 不依赖 Central 业务包，也不拥有 Task、Meeting 等项目业务模型。
- 共享通信契约放在 `internal/runnerprotocol/`，两端各自的实现留在各自目录。
- 开始实现某项能力时，再增加相应模块和内部目录，避免提前固定尚未验证的包结构。
- 空目录使用 `.gitkeep` 保存到 Git；目录有实际文件后可移除占位文件。
- 本地敏感配置不入库，示例配置可以入库。根目录的 `secrets/` 被忽略，同名源码目录不受该规则影响。

前端已建立独立依赖清单、Vue 入口、路由骨架和组件库，见[前端开发说明](frontend/README.md)。后端、部署、API、协议类型和数据库实现仍待后续开发。
