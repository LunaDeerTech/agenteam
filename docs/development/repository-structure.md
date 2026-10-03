# 仓库结构与代码归属

agenteam 使用一个仓库维护 Central、Runner、前端及配套文档。目录以稳定的程序边界和职责划分，业务模块内部结构在实际实现时逐步增加。

技术与部署边界以[系统架构](../architecture/README.md)为准。共同的 ID、时间、错误、分页、版本、幂等、事务与生命周期方向见[基础契约约定](../architecture/platform-infrastructure/foundation-contracts.md)，具体端口与依赖矩阵已在 [D01](work-items/d01-contracts/README.md) 固定；后续模块逐项实现与绑定。

## 目录结构

```text
agenteam/
├── cmd/
│   ├── agenteam/
│   └── agenteam-runner/
├── internal/
│   ├── central/
│   ├── runner/
│   ├── platform/
│   └── runnerprotocol/
├── api/
│   └── openapi/
├── web/
│   └── src/
├── db/
│   └── migrations/
├── deploy/
├── scripts/
├── tests/
├── docs/
├── go.mod
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
| `internal/platform/` | 无业务依赖的日志与生命周期工具，供两个独立组合根使用。 |
| `api/openapi/` | 当前包含已实现的公共标量、Problem 与分页 schema；业务 API 片段随模块加入。 |
| `web/src/` | Vue 前端源码；公共组件、应用骨架、主题与开发环境 Debug 已实现。 |
| `db/migrations/` | Central 由 Goose 管理的全局 SQL 迁移序列，不按业务模块建立独立迁移器，不由 ORM 自动修改生产结构。 |
| `deploy/` | 部署配置与相关示例，后续承载 Docker Compose 等部署文件。 |
| `scripts/` | 开发、构建与维护脚本。 |
| `tests/` | 跨模块集成测试与端到端测试。Go 单元测试随被测源码放置。 |
| `docs/` | 架构、前端设计与开发说明；保留现有文档组织。 |

## 依赖与演进约定

- Central 与 Runner 共用根目录 Go module，固定 Go 1.27.1；D03 已固定 pgx/Goose 依赖，并将 PostgreSQL/pgvector 初始化、健康采样与关闭顺序绑定到 Central 入口；前端在 `web/` 独立管理依赖。
- Runner 不依赖 Central 业务包，也不拥有 Task、Meeting 等项目业务模型。
- 共享通信契约放在 `internal/runnerprotocol/`，两端各自的实现留在各自目录。
- Central 跨模块端口按稳定职责建立独立契约包，由调用方和实现方共同依赖；不包含业务实现、不循环引用，不建立万能 `contracts/shared` 包或大量无必要的空包。实际包路径按照 D01 依赖矩阵随消费模块按需增加。
- 各模块只经正式端口访问对方事实，不直接改对方表。需要原子性的跨模块端口显式接收统一事务上下文，业务契约不依赖 pgx 等驱动类型。
- HTTP 采用 `net/http` + `ServeMux`，handler 与 HTTP DTO 转换按模块组织；数据库基础已采用固定 pgx + 显式 SQL，迁移为 Goose SQL-only Provider 和单一全局序列。后续 repository adapter 消费已有 Tx/锁端口，不暴露原始驱动事务给领域层。
- 开始实现某项能力时，再增加相应模块和内部目录，避免提前固定尚未验证的包结构。
- 空目录使用 `.gitkeep` 保存到 Git；目录有实际文件后可移除占位文件。
- 本地敏感配置不入库，示例配置可以入库。根目录的 `secrets/` 被忽略，同名源码目录不受该规则影响。

前端已建立独立依赖清单、Vue 入口、路由骨架和组件库，见[前端开发说明](frontend/README.md)。后端已有基础类型、HTTP 边界、数据库连接/迁移/事务/锁、环境配置、两个独立入口和停机验证，见[后端开发说明](backend/README.md)和[数据库说明](backend/database.md)。Central 在数据库与 Audit/cursor/Secret/出站策略初始化后只提供非 ready 的诊断，Runner 未连接且未认证；业务协议、对象存储、身份和实际出站消费者仍待后续实现。Central 环境示例覆盖 D04，必需独立 CURSOR_KEYRING 与 SECRET_KEYRING，可选 OUTBOUND_CA_FILE；Runner 保持 D02。Audit 同事务写入、授权读取/清理端口与签名 cursor 见 [Audit 说明](backend/audit.md)，身份/Project 授权适配仍未绑定。

Secret 的 envelope、稳定引用/lease、加密命令 receipt、nonce 预留、可恢复重保护及 gate 清理由 `internal/central/secret/` 拥有，使用全局迁移 `00003_secret.sql`。正式端口和后续绑定责任见 [Secret 说明](backend/secret.md)。维护 worker 已装配到 Central，业务读取/变更授权和实际执行 binding 仍未绑定。

`internal/central/outbound/` 拥有 DB 策略/Audit/receipt、完整 DNS wire 解析、保守 IP 分类、提交/实际首写门禁、H1 keep-alive client 和 SMTP 受控 Conn，使用 `00004_outbound.sql`。`tests/testsupport/outbound/` 与 `scripts/test-security.sh` 拥有隔离网络 fixture；具体接口、SDK 错误包装、DNS 平台限制和未来 D07/D09/D20 责任见[出站说明](backend/outbound.md)。不提供未鉴权策略 HTTP 或已集成 SMTP/Provider 的假状态。
