# Deployment Runtime 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](./README.md)
>
> 相关设计：
> - [Object Storage](./object-storage.md)
> - [Outbound Network Policy](./outbound-network-policy.md)
> - [Internal Domain Events](./internal-domain-events.md)
> - [Realtime](./realtime.md)
> - [Model System](./model-system/README.md)

## 1. 定位

本文定义 agenteam Central 的部署与运行时基础设施契约。

agenteam 的正式部署模型是：

~~~text
Single agenteam Central
  + PostgreSQL / pgvector
  + MinIO
  + Multiple Remote Runners
~~~

当前不设计多个 Central 实例，也不引入 Redis。

Project、Task、Meeting、Agent、Tool、Runner Management 等 Domain 只是单后端项目内部的业务职责 / 代码模块边界，不对应独立后端服务或独立部署单元。

## 2. 部署拓扑

~~~mermaid
flowchart TB
    Browser["Browser"]

    subgraph Central["agenteam Central"]
        Binary["agenteam Binary"]
        Frontend["Embedded Web Assets"]
        API["Go Backend"]
        EventBus["Event Dispatcher / In-process Event Bus"]
        Realtime["WebSocket Realtime Gateway"]
        ObjectService["Object Storage Service"]
        Outbound["Outbound Network Policy"]
    end

    subgraph Infra["Docker Compose Infrastructure"]
        PG["PostgreSQL + pgvector"]
        MinIO["MinIO"]
    end

    subgraph Remote["Remote Machines"]
        Runner1["agenteam-runner"]
        Runner2["agenteam-runner"]
        RunnerN["agenteam-runner"]
    end

    Providers["Model Providers"]
    MCP["MCP Servers"]

    Browser --> Binary
    Binary --> Frontend
    Binary --> API

    API --> PG
    API --> ObjectService
    ObjectService --> MinIO

    API --> EventBus
    EventBus --> Realtime
    Realtime --> Browser

    API --> Outbound
    Outbound --> Providers
    Outbound --> MCP

    Runner1 -->|Outbound WSS| API
    Runner2 -->|Outbound WSS| API
    RunnerN -->|Outbound WSS| API
~~~

### 2.1 前端资源与 HTTP 入口

正式构建将前端静态资源嵌入 Central，由同一应用提供页面与 API。前端 History 路由（包括 `/{username}/{project_name}` 深链接）在适用页面请求上回退到应用入口；API、缺失静态资源和服务端错误不得回退为前端 HTML。具体路径划分、缓存和构建方式由 D02/D26/D28 规格固定并用真实构建验收，Debug 不进入生产资源。

## 3. 必需基础设施

Central 第一阶段要求：

- PostgreSQL + pgvector；
- MinIO。

两者都是 mandatory dependency。

确认不设计 degraded mode。

如果任一必需依赖不可用：

> Central 不进入 ready 状态，并拒绝完成正常启动。

不为了“部分功能还能用”设计复杂 fallback。

## 4. Redis

当前架构明确删除 Redis。

不再使用 Redis 承担：

- cache；
- realtime pub/sub；
- distributed lock；
- scheduler coordination；
- lease acceleration。

替代关系：

~~~text
ordinary cache
-> no cache / in-process cache if needed

realtime pubsub
-> in-process Realtime Hub

distributed Central lock
-> not needed because single Central

correctness-critical coordination
-> PostgreSQL transaction / row lock / advisory lock / version

scheduler ownership
-> PostgreSQL Source of Truth
~~~

未来如果产品明确支持多个 Central，再重新设计跨实例协调和 realtime backplane。

## 5. Deployment Config 与 Runtime Platform Config

平台配置分两层。

### 5.1 Deployment Config

由部署环境控制，不能作为普通运行时业务配置由 Project UI 任意修改。

包括：

- PostgreSQL connection；
- MinIO endpoint / credential / bucket 等 backend config；
- Secret master key ring / 当前写入版本；
- TLS / trust store；
- server bind / public origin 等部署参数；
- migration / health 相关运行参数；
- Event retry / worker 等 implementation config。

技术部署配置从 Docker 部署环境的环境变量读取；原始主密钥与密钥环也只由环境注入，不保存到业务数据库或普通配置 UI。证书 / trust store 等文件的定位与加载方式由部署规格明确，敏感环境值不进入仓库、普通日志或诊断输出。

### 5.2 Runtime Platform Config

保存在 PostgreSQL，由平台 UI / 管理 API 管理。

包括：

- System Model Provider；
- ModelConfig；
- PlatformModelSelection；
- SMTP 配置（认证凭据通过 Secret Management 保存）；
- 管理员出站内网放行规则（网段、端口及显式允许 HTTP）；
- 账号 Session / 密码重置期限、登录挑战阈值及 SMTP 重试参数；
- 其他属于产品行为而不是部署边界的平台配置。

Runtime Platform Config 不保存原始主密钥，也不能绕过固定禁止地址、TLS 验证等平台安全约束。出站内网放行规则明确由管理员在系统 UI / 管理 API 维护并持久化数据库，更新无需重启；Project、Agent、MCP Config 不能自行扩大网络权限。技术配置使用环境变量的约定不把这些已定运行配置搬出数据库。

## 6. Secret Management 与 Envelope Encryption

平台级 Secret 与 Project Secret 均采用 AES-256-GCM 的应用层 envelope encryption：数据密钥加密 Secret，部署主密钥保护数据密钥。优先使用 Go 标准加密库，不自行发明算法，并确保同一密钥下 nonce 不重复。

数据库保存：

~~~text
Secret ciphertext
wrapped data key
algorithm / format / key version
migration progress / state / safe error metadata
~~~

Central 运行时通过部署级 master key 解开受保护的数据密钥，再处理 Secret。数据库中用于分批迁移恢复的状态不包含明文主密钥或明文数据密钥。

核心边界：

~~~text
Plaintext Secret
    ↓ AES-256-GCM with data key
Secret ciphertext in PostgreSQL

Data key
    ↓ protected by deployment master key
Wrapped data key in PostgreSQL

Deployment master key ring
    ← environment only
~~~

master key：

- 不进入业务数据库；
- 不进入普通日志；
- 不进入 Prompt；
- 不暴露给 Agent；
- 不通过普通 Platform UI 返回。

未来可以把底层 Secret provider 替换为：

- KMS；
- Vault；
- 其他 external key management；

但上层仍通过统一 Secret Management abstraction 访问。

## 7. Master Key 来源

第一阶段原始 master key ring 通过 Docker 部署环境变量注入，包含带版本的有效密钥及当前写入版本；不以数据库持久化替代环境配置。

不允许：

- 与 ciphertext 一起存 PostgreSQL；
- 自动生成后写入普通可读项目配置；
- 通过 Project Config 保存。

Central 启动时必须能够读取有效密钥环，能够使用当前写入版本并解析仍需读取的旧版本。

如果 Secret Management 需要 master key 而配置缺失 / 无效：

> startup fail。

轮换时新写入使用新版本，后台分批重新保护已有数据密钥，并在数据库持久化非敏感的迁移进度、状态和错误以便中断恢复。只有确认存量迁移完成、旧版本已无必要引用后，部署方才能移除旧主密钥。

更新环境变量需要更新部署并重启 Central；启动后读取新的密钥配置并恢复迁移。自动分批迁移不生成、轮换或回写部署主密钥。密钥环编码、认证附加数据、nonce 生命周期、并发写入与迁移恢复检查点由 D04/D28 规格落实，不能仅凭算法和轮换方向宣称安全实现已验收。

## 8. Schema Migration

agenteam Central 只有一个后端应用和一个 PostgreSQL schema lifecycle。

第一阶段采用：

> Goose 管理的一个全局 SQL migration 序列 / 目录 + 一个统一 migration runner。

不需要按 Domain 构建独立 migration runner。

数据库访问采用 pgx + 显式 SQL，不由 ORM 自动修改生产表结构。常规迁移使用事务，不支持事务的 SQL 必须显式标记；Goose/pgx 固定版本、配套连接方式、文件规范、锁和失败恢复由 D03 通过真实 PostgreSQL 验证。不能由库默认行为推断所有迁移可安全自动回滚。

某个 migration 可以同时调整多个模块相关表，只要 migration 本身：

- 有明确版本；
- 顺序稳定；
- 可重复判断已执行状态；
- 失败不会让应用误以为 schema 已 ready。

## 9. Migration Lifecycle

建议 Central 启动阶段：

~~~text
connect PostgreSQL
    ↓
acquire migration guard
    ↓
read schema version
    ↓
apply pending migrations in order
    ↓
migration success
    ↓
continue application startup
~~~

当前单 Central 不要求分布式 migration election。

但 migration runner 仍应使用 PostgreSQL lock / advisory lock 等机制防止误启动多个进程时并发执行 migration。

## 10. Startup Sequence

建议：

~~~text
1. load Deployment Config
2. validate config
3. load / validate master key
4. initialize PostgreSQL
5. verify pgvector capability / migration prerequisites
6. initialize MinIO / Object Storage backend
7. run schema migrations
8. initialize Secret Management
9. load Runtime Platform Config
10. initialize Outbound Network Policy
11. register Domain Event handlers
12. start Domain Event Dispatcher
13. start Realtime Gateway
14. start Scheduler / Agent Executor / Runner Gateway / HTTP API
15. run readiness checks
16. readiness = ready
~~~

具体 package 初始化顺序可以调整，但不能违反依赖关系。

## 11. Startup Failure

任何 mandatory dependency：

- config invalid；
- PostgreSQL unavailable；
- MinIO unavailable；
- migration failed；
- master key invalid；
- required platform component init failed；

都会导致：

~~~text
startup failed
readiness != ready
process exits or remains explicitly non-ready according to launcher policy
~~~

第一阶段不设计：

~~~text
PostgreSQL down but UI still starts
MinIO down but partial mode continues
~~~

这种 degraded mode。

## 12. Liveness

Liveness 回答：

> 当前 Central 进程是否仍然活着并能够执行基本 event loop。

Liveness 不应该因为某个短暂外部 Model Provider 不可用而失败。

它也不应该执行昂贵业务查询。

概念 endpoint：

~~~text
/liveness
~~~

具体 route 命名属于 API 实现。

## 13. Readiness

Readiness 回答：

> 当前 Central 是否满足接受正常业务流量的基础条件。

至少检查：

- PostgreSQL；
- schema migration state；
- MinIO / Object Storage backend；
- Secret Management master key availability；
- 必需内部 component 初始化完成。

如果任一 mandatory dependency unhealthy：

~~~text
readiness = not ready
~~~

## 14. Dependency Diagnostics

除 aggregate readiness 外，应提供细分状态。

概念：

~~~text
postgresql: ready
pgvector: ready
minio: ready
migrations: ready
secret_management: ready
event_dispatcher: ready
realtime_gateway: ready
~~~

Diagnostics 面向部署 / 运维。

不能把：

- database password；
- MinIO credential；
- master key；
- Secret plaintext；

写入 diagnostic response。

## 15. 外部资源不是 Startup Dependency

以下外部资源不是 Central mandatory startup dependency：

- 某个 Model Provider；
- 某个 MCP Server；
- 某台 Runner；
- Git remote；
- Project configured integration。

它们可以处于：

- offline；
- unavailable；
- auth error；

而 Central 本身仍然 ready。

这与 mandatory infrastructure dependency 不同。

## 16. Runner

Runner 由远端主动向 Central 建立出站 WSS。

Central 启动不要求所有 Runner 在线。

Runner 的在线状态属于业务 runtime state，不属于 Central readiness。

Runner Protocol 见 [Runner 架构](../runner/README.md)。

## 17. Object Storage

MinIO 是第一阶段 ObjectStorageService backend。

Central readiness 要求 Object Storage backend 可用。

业务模块仍然：

~~~text
Domain
-> ObjectStorageService
-> MinIO
~~~

不直接使用 MinIO SDK。

完整设计见 [Object Storage](./object-storage.md)。

## 18. PostgreSQL / pgvector

PostgreSQL 是主要业务 Source of Truth。

pgvector 与 PostgreSQL 同部署能力，用于：

- Knowledge embeddings；
- Agent Memory embeddings。

Central startup / migration 应确保需要的 extension / schema 已准备好。

向量数据是可重建派生数据，但数据库本身仍是 mandatory dependency。

## 19. Internal Domain Events

Domain Event durable state 位于 PostgreSQL。

Central restart 后 Event Dispatcher：

- 重新读取 pending / retry delivery；
- 继续处理；
- 不依赖 Redis / external broker。

完整设计见 [Internal Domain Events](./internal-domain-events.md)。

## 20. Realtime

Realtime Gateway 运行在当前 Central 进程内。

Central restart 会：

- 断开所有 Browser WebSocket；
- 丢失 in-memory subscription；
- Browser 自动 reconnect；
- reload Snapshot；
- restore subscriptions。

这不影响业务 Source of Truth。

完整设计见 [Realtime](./realtime.md)。

## 21. Graceful Shutdown

Central 收到正常 shutdown signal 时建议：

1. readiness -> not ready；
2. 停止接受新的业务请求 / launch；
3. 停止新 Scheduler dispatch；
4. 关闭 / drain HTTP 与 WebSocket；
5. 停止 Event Dispatcher 领取新的 delivery；
6. 等待当前安全可结束的数据库事务；
7. flush 必要日志 / metrics；
8. 关闭基础设施 connection。

Agent Execution 的 crash / restart recovery 由 Agent Executor 自己的 checkpoint / lifecycle 设计处理。

Graceful shutdown 不能依赖“所有 Agent 都必须自然执行完”。

## 22. Process Crash

非优雅 crash 后：

- PostgreSQL business state 保留；
- Domain Event Outbox 保留；
- Event delivery 可恢复；
- Runtime View Snapshot 按其 Store 恢复；
- Realtime connection 丢失后由 Browser 重连；
- Scheduler 根据 PostgreSQL Source of Truth reconcile；
- MinIO payload 不受 Central process crash 影响。

这也是不把 correctness-critical state 放在进程内 memory 的原因。

## 23. Docker Compose 边界

第一阶段使用 Docker Compose 管理：

~~~text
PostgreSQL + pgvector
MinIO
agenteam Central
~~~

Runner 通常位于远程设备，不要求与 Central Compose 同机。

部署文件负责：

- service dependency；
- volume；
- network；
- 环境变量与敏感部署配置注入；
- health check；
- persistent data path。

业务 Domain 不感知 Compose service name。

## 24. Persistent Volumes

至少：

### PostgreSQL

持久化业务 Source of Truth。

### MinIO

持久化 StoredObject payload。

Central binary 自己不应把唯一业务事实保存在容器 ephemeral filesystem。

## 25. Upgrade

正常升级流程：

~~~text
stop / drain old Central
deploy new binary
start Central
run migrations
readiness ready
accept traffic
~~~

当前单 Central 不设计 zero-downtime rolling multi-instance upgrade。

升级期间 Browser / Runner 可以断线并在服务恢复后重连。

## 26. Observability

部署层至少需要：

- startup logs；
- migration logs；
- dependency health；
- Event Dispatcher backlog；
- WebSocket connection count；
- PostgreSQL connection pool；
- Object Storage error rate；
- process resource metrics。

敏感配置必须 masking。

## 27. 第一阶段实现边界

第一阶段实现：

1. single Central；
2. PostgreSQL + pgvector；
3. MinIO；
4. no Redis；
5. Deployment Config；
6. Runtime Platform Config；
7. AES-256-GCM envelope encryption、环境主密钥环与可恢复迁移；
8. Goose global SQL migration runner；
9. startup dependency validation；
10. mandatory dependency fail-fast；
11. liveness；
12. readiness；
13. dependency diagnostics；
14. graceful shutdown；
15. Docker Compose deployment；
16. embedded frontend assets 与受限 History fallback。

第一阶段不实现：

- multi-Central；
- Redis；
- distributed lock service；
- external event broker；
- degraded mode；
- zero-downtime multi-instance rolling deploy；
- Kubernetes operator；
- KMS / Vault mandatory dependency。

## 账号首次初始化与可选 SMTP

必需基础设施与 migration 完成后，执行幂等的人类账号首次初始化，创建邮箱 admin@mail.com、username 为 admin 的管理员，并输出初始随机密码到 Central 后台日志。普通重启不覆盖账号或密码；日志丢失沿统一忘记密码流程恢复，不提供专用管理员密码重设 CLI。详细事务与恢复边界见 [账号生命周期](./authentication/account-lifecycle.md)。

SMTP 缺省不阻断 ready；未配置时邀请和密码重置链接输出后台日志。已配置但投递失败不切换渠道，不影响已提交账号 / 邀请的业务事实。该行为不改变 PostgreSQL / MinIO mandatory dependency，不引入 degraded mode。敏感后台日志与不含 Secret 的 Audit 分离，部署需明确日志访问范围；见 [SMTP Delivery](./authentication/smtp-delivery.md)。
