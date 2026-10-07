# 平台基础设施与部署架构

## 1. 模块范围

本模块描述 agenteam Central 跨业务模块共享的平台能力与部署边界：

- Model Management；
- PostgreSQL / pgvector；
- Object Storage / MinIO；
- Human Inbox；
- Internal Domain Events；
- Realtime；
- Secret Management；
- Outbound Network Policy；
- Authentication / SMTP Delivery；
- Authorization / Approval / Audit 的平台边界；
- Central Deployment Runtime。

agenteam Central 是一个整体后端。

Project、Task、Meeting、Agent、Tool、Runner Management 等 Domain 只是后端内部的业务职责与代码组织边界，不对应独立微服务或独立部署单元。

当前正式拓扑：

~~~text
Single agenteam Central
  + PostgreSQL / pgvector
  + MinIO
  + Multiple Remote Runners
~~~

当前不引入 Redis，也不设计多个 Central 实例。

跨模块的 ID、时间、错误、分页、版本、幂等、DTO、事务及生命周期约定见[基础契约约定](foundation-contracts.md)。HTTP 使用 net/http + ServeMux，数据库使用 pgx + 显式 SQL；这些已定方向仍须 D01 和责任模块完成具体规格及验收。

## 2. 总体结构

~~~mermaid
flowchart TB
    Browser["Browser"]

    subgraph Central["agenteam Central"]
        API["Web / API"]
        Domain["Project / Task / Meeting / Agent Domains"]
        Executor["Agent Executor / Agent Loop / Tool Runtime"]
        Models["Model System"]
        Governance["Governance / Audit / Secret Management"]
        Events["Internal Domain Events"]
        Realtime["WebSocket Realtime Gateway"]
        Inbox["Human Inbox Projection"]
        Objects["Object Storage Service"]
        Outbound["Outbound Network Policy"]

        API --> Domain
        Domain --> Executor
        Executor --> Models
        Domain --> Governance

        Domain --> Events
        Executor --> Events
        Governance --> Events

        Events --> Inbox
        Events --> Realtime
        Inbox --> Realtime

        Domain --> Objects
        Executor --> Objects

        Models --> Outbound
        Executor --> Outbound
    end

    subgraph Storage["Infrastructure"]
        PG["PostgreSQL + pgvector"]
        MinIO["MinIO"]
    end

    subgraph Remote["Remote Machines"]
        Runner["agenteam-runner"]
    end

    External["Model Providers / MCP Servers"]

    Browser --> API
    Realtime --> Browser

    Domain --> PG
    Executor --> PG
    Governance --> PG
    Events --> PG
    Inbox --> PG
    Objects --> MinIO

    Runner -->|Outbound WSS| API
    Outbound --> External
~~~

## 3. Model Management

系统允许接入多个 Provider 和多个 Model。

模型配置分为：

- System Provider / Model；
- Project Provider / Model；
- Platform Model Selection，包括系统统一的独立 Meeting Summary 用途；
- Project-specific model selection，仅用于原有允许的项目配置，不包含 Meeting Summary override。

所有 enabled 的 System chat Model 对所有 Project 可见。Project 也可以配置自己的 chat Model。

非 chat Model：

- embedding；
- reranker；
- image generation；

第一阶段只允许 System Provider 配置。

业务模块不直接适配每家 Provider。

统一关系：

~~~text
Agent Loop / Meeting Summary / Memory / Builtin Tool
    ↓
Model Resolver
    ↓
Resolved Model
    ↓
Model Adapter
    ↓
Provider
~~~

第一阶段 Chat Provider Adapter：

- OpenAI Chat Completions / OpenAI-compatible；
- Anthropic Messages。

完整设计见 [Model System](./model-system/README.md)，其中长期配置、解析快照与 Chat Runtime 分别见 [Model Configuration](./model-system/model-configuration.md)、[Model Resolution](./model-system/model-resolution.md)、[Chat Model Runtime](./model-system/chat-model-runtime.md)。

Model Invocation Token Usage 的事实模型与聚合见 [Model Token Usage 详细设计](./model-token-usage.md)。

## 4. 存储职责

### PostgreSQL

PostgreSQL 是主要业务 Source of Truth。

保存：

- Project / Sprint / Task / TaskEvent / Blocker；
- Meeting；
- Agent；
- Agent Execution；
- Approval；
- Runner metadata；
- Model config；
- Platform Model Selection；
- Model Invocation Usage；
- Audit metadata；
- Human Inbox projection；
- Domain Event Outbox / Delivery；
- 其他结构化业务状态。

### pgvector

pgvector 是 PostgreSQL 内的向量能力，用于：

- Knowledge embedding；
- Agent Memory embedding。

向量索引属于可重建派生数据。

### MinIO

MinIO 是 ObjectStorageService 的第一阶段 backend。

适合保存：

- attachment；
- canonical Knowledge Document；
- Tool Artifact；
- MCP Resource materialization；
- generated file；
- 大型 Execution / Runtime Log payload。

业务模块不直接访问 MinIO。

### 不使用 Redis

当前单 Central 架构不使用 Redis。

原先设想的职责分别收敛为：

~~~text
cache
-> no cache / in-process cache if needed

realtime pubsub
-> in-process Realtime Hub

distributed lock / scheduler coordination
-> unnecessary for single Central

correctness-critical coordination
-> PostgreSQL transaction / row lock / advisory lock / version
~~~

如果未来明确演进为多个 Central，再重新设计跨实例协调与 Realtime backplane。

## 5. Object Storage

平台统一对象实体：

~~~text
StoredObject
├── id
├── storage_key
├── media_type
├── size
├── checksum
├── status
└── created_at
~~~

统一访问：

~~~text
Business Entity
    ↓ stored_object_id
StoredObject
    ↓
ObjectStorageService
    ↓
MinIO
~~~

Agent 不直接调用 ObjectStorageService。

Agent-facing 文件能力通过 Tool System 中具有业务语义的 ToolArtifact / Artifact Builtin Tools 暴露。

完整数据模型、写入一致性、Signed URL、引用与删除规则见 [Object Storage 详细设计](./object-storage.md)。

## 6. Human Inbox

Human Inbox 是跨业务模块的统一人类待处理 projection。

典型来源：

- pending Approval Request；
- proposed Meeting；
- pending DecisionRequest；
- review request；
- waiting_for_human / blocker；
- technical warning / failure。

Human Inbox 拥有自己的 projection lifecycle：

~~~text
open
resolved
dismissed
~~~

但不复制 Approval、Meeting、Task 等源业务对象的权威状态。

稳定 identity：

~~~text
(source_type, source_id, action_type)
~~~

必须处理型事项，例如 Approval / Review / Decision，不允许 dismiss。

默认排序优先考虑 blocking / priority，再按更新时间倒序。

Projection 由 Internal Domain Event 驱动，支持幂等消费和基于当前 Source of Truth 的专用 rebuild。

完整数据模型、projection lifecycle、Action Routing、Rebuild 和 Realtime 集成见 [Human Inbox 详细设计](./human-inbox.md)。

## 7. Internal Domain Events

Internal Domain Events 是 Central 内部模块解耦机制。

代码侧使用明确 typed Event + Handler 模型，但 Event 语义固定为：

~~~text
immutable
non-cancellable
after-the-fact business fact
~~~

所有 Domain Event 都写 PostgreSQL Transactional Outbox。

~~~text
BEGIN
  mutate business state
  insert DomainEventOutbox
COMMIT
~~~

Commit 后：

~~~text
Domain Event Dispatcher
    ↓
In-process Event Bus
    ↓
Typed Handlers
~~~

第一阶段：

- at-least-once；
- Consumer 幂等；
- Handler 独立失败；
- retry / failed delivery；
- 同一 aggregate 内有序；
- 不保证全局顺序；
- 不提供 Listener Priority；
- 不做 Event Sourcing；
- 不做通用 Event Replay。

Scheduler correctness 不依赖 Event Bus。

完整 envelope、Outbox、Dispatcher、Handler Delivery、Retry、Ordering 与 Replay 边界见 [Internal Domain Events 详细设计](./internal-domain-events.md)。

## 8. Realtime

Realtime 与 Domain Event 是两套不同 contract。

~~~text
Domain Event
= durable backend business fact

Realtime Event
= transient UI delivery / invalidation / stream
~~~

第一阶段统一使用 WebSocket Realtime Gateway。

当前单 Central 直接使用进程内 fan-out：

~~~text
Realtime Publisher
    ↓
In-process Realtime Hub
    ↓
WebSocket Gateway
    ↓
Browser
~~~

不使用 Redis Pub/Sub。

Realtime Event 可以丢失。断线后客户端重新加载 authoritative Snapshot / Read Model，再恢复 subscription。

典型能力：

- Agent Execution Runtime View；
- Meeting Timeline；
- Human Inbox；
- Task / Review UI update。

完整 subscription、Authorization、Snapshot race、Reconnect、Backpressure 与 Slow Consumer 处理见 [Realtime 详细设计](./realtime.md)。

## 9. Governance / Audit

随着 Agent 获得项目写能力，Governance 是正式平台能力。

包括：

- authentication；
- Project Owner boundary；
- resource scope；
- Agent capability；
- Approval；
- Secret Management；
- Audit。

最终权限校验必须发生在服务端。

Audit 与 TaskEvent / Execution Log 分层：

~~~text
TaskEvent
= 用户可见 Task 业务历史

Execution Log
= Agent 实际运行记录

Audit
= 谁以什么权限执行了什么敏感操作
~~~

Audit 详细设计见 [Audit](../security-governance/audit.md)。

## 10. Secret Management

Project Secret 与平台级 Credential 都通过统一 Secret Management abstraction 使用加密存储。

第一阶段采用 AES-256-GCM 应用层 envelope encryption，数据密钥加密 Secret，环境主密钥保护数据密钥：

~~~text
Secret plaintext
    ↓
AES-256-GCM with data key
    ↓
ciphertext in PostgreSQL

data key
    ↓
protected by deployment master key
    ↓
wrapped data key + key version in PostgreSQL
~~~

master key：

- 不进入 PostgreSQL；
- 仅通过部署环境变量注入带版本的密钥环和当前写入版本；
- 不进入 Prompt / Tool argument / ordinary log；
- 不暴露给 Agent。

Model Provider Credential、Project Secret 等上层模型仍保存 credential_ref / Secret identity，而不是复制 plaintext。

新写入使用当前主密钥版本，后台分批重新保护存量数据密钥。数据库只保存密文、受保护的数据密钥、版本及迁移进度/状态等元数据；迁移可中断恢复，旧主密钥在存量迁移完成后才退役。环境更新通过部署重启加载，不由平台自动生成或回写主密钥。

Project Variable / Secret 的领域模型见 [Project Environment Variables 与 Secret](../project-work-management/project-environment-variables.md)。

部署级 master key 与配置来源见 [Deployment Runtime 详细设计](./deployment-runtime.md)。

## 11. Outbound Network Policy

Central 中由业务配置、用户输入或外部数据决定目标地址的出站网络访问统一经过 Outbound Network Policy。

典型消费者：

- MCP；
- Model Provider；
- SMTP（使用非 HTTP 安全拨号适配）；
- 未来 Webhook / HTTP Integration。

统一处理：

- URL validation；
- DNS / IP classification；
- SSRF；
- private CIDR；
- DNS rebinding；
- redirect；
- Credential forwarding；
- TLS；
- timeout / response guardrail。

内网放行规则由系统管理员通过 UI / 管理 API 维护，保存在 PostgreSQL，按网段与可选端口限制，并可显式选择全部端口。默认不放行内网，固定禁止地址不能被规则解除；内网 HTTP 还须管理员显式允许。新请求、重试和重定向立即使用当前规则，已发出的在途请求继续，连接复用不能绕过校验。Project / Agent / MCP Config 不能自行扩大网络访问范围。

完整设计见 [Outbound Network Policy 详细设计](./outbound-network-policy.md)。

## 12. 配置层次

平台配置分为两类。

### Deployment Config

由部署环境提供：

- PostgreSQL；
- MinIO；
- master key ring / 当前写入版本；
- TLS / trust store；
- server runtime；
- 其他基础设施与安全边界。

技术部署配置从 Docker 部署环境变量读取；原始主密钥不进入数据库。受信任证书等文件的定位由部署规格明确，敏感环境值不在普通日志或 UI 回显。

### Runtime Platform Config

保存在 PostgreSQL，可通过平台 UI / 管理 API 修改。

例如：

- System Model Provider；
- ModelConfig；
- PlatformModelSelection；
- SMTP 配置（凭据由 Secret Management 保存）；
- 管理员出站内网放行规则；
- Session / 密码重置期限、登录挑战阈值和 SMTP 重试参数。

运行配置不能绕过固定禁止地址、TLS 验证或原始主密钥的部署隔离。内网规则是明确授权的管理员运行配置，不再作为只可经部署修改的 CIDR 列表。

## 13. Deployment Runtime

正式部署：

~~~text
Single Central
+ PostgreSQL / pgvector
+ MinIO
+ Multiple Remote Runners
~~~

PostgreSQL 和 MinIO 都是 mandatory dependency。

当前不实现 degraded mode：

> 任一必需基础设施依赖不可用，Central 不进入 ready 状态。

数据库 migration 统一由 Goose 在 Central 中管理一个全局、有序的 SQL migration sequence；锁、事务和失败恢复由 D03 真实验证。

Health 分为：

- liveness；
- readiness；
- dependency diagnostics。

Runner、单个 Model Provider、MCP Server 等外部资源不属于 Central startup mandatory dependency。

完整启动顺序、migration、health、graceful shutdown 与 Docker Compose 边界见 [Deployment Runtime 详细设计](./deployment-runtime.md)。

## 14. Runner 边界

Runner 是远程执行环境。

Runner 主动向 Central 建立出站 WSS；Central 不要求反向访问 Runner。

Runner 是否在线不影响 Central readiness。

Runner 的 Device、Protocol、Data Channel、Execution Runtime、Desktop / Tunnel 等完整设计见 [Runner 架构](../runner/README.md)。

## 15. Authentication 与 SMTP Delivery

Authentication 负责人类账号初始化、邮箱密码登录、Web Session、邀请注册、资料与密码恢复；系统管理员与 Project Owner 授权分离。SMTP 是可选 Runtime Platform Config，未配置时邀请 / 重置链接输出受限后台日志，已配置发送失败不自动降级。密码和恢复链接不进入 Audit。

完整设计见 [账号认证与邮件投递](./authentication/README.md)。首次初始化与 mandatory dependency 的关系见 Deployment Runtime。

## 16. 详细设计索引

按本模块的设计关系：

1. [Model System](./model-system/README.md)
2. [Model Token Usage](./model-token-usage.md)
3. [Object Storage](./object-storage.md)
4. [Human Inbox](./human-inbox.md)
5. [Internal Domain Events](./internal-domain-events.md)
6. [Realtime](./realtime.md)
7. [Deployment Runtime](./deployment-runtime.md)
8. [Outbound Network Policy](./outbound-network-policy.md)
9. [账号认证与邮件投递](./authentication/README.md)
10. [基础契约约定](./foundation-contracts.md)
