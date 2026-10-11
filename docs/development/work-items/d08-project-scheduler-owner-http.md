# Project Scheduler Owner HTTP

状态：基于 `54468a28`，固定技术候选 `1381eb77` 的 Owner HTTP 能力已有限独立接受；[设置 UI 后继](d08-project-scheduler-ui.md)已完成默认 App/browser 两场景的有限验收，待 root 整体保存。作者 `variables_review`，真实 fixture 作者 `knowledge_text_fixture`，独立审阅 `runtime_recovery`，Git 与真实资源窗口由 root 管理。

已有 initialized Project 的当前 Human Owner 可读取、暂停/恢复调度配置并设置并发上限。本片先交正式 HTTP 能力；不新增 Scheduler Runtime、Project 初始化、领域策略或迁移。正式语义沿 [Pause / Resume](../../architecture/scheduler/scheduler-loop.md#9-pause--resume)与 [Project 设置设计](../../frontend-design/layouts/project-settings.md)，实现消费现有 `project.Service.GetSchedulerConfig`、`UpdateProject` 和 `LookupCommand`。

## 固定 wire 与恢复边界

| 操作 | 请求与结果 |
| --- | --- |
| `GET /api/v1/projects/{project_id}/scheduler` | 无 body/query；200 恰 `{project_id,version,scheduler_enabled,scheduler_max_concurrency}`，表示当前同事务观察 |
| 同路径 `HEAD` | 与 GET 相同认证、服务读取、状态和响应长度，不发送正文 |
| 同路径 `PATCH` | 原 `Idempotency-Key`；body 恰 `{expected_version,scheduler_enabled,scheduler_max_concurrency}`，三个字段必需；200 恰 `{project_id,version}`，仅原命令确认 |
| 原 `POST /api/v1/projects/{project_id}/commands/lookup` | 保留既有 `{command:"update"}`、原 key、`state/result` 与历史 ProjectRef 回执，不新增 Lookup 路径或命令空间 |

版本、非空并发上限均为 canonical 正十进制字符串，范围 `1..9223372036854775807`；并发上限的显式 `null` 表示不限，`false` 表示暂停。PATCH 是完整 Scheduler 配置替换，不能省略字段、传 null enabled、混入 name/description 或默认为旧值。新请求/响应限 4 KiB；拒未知/重复/别名字段、非 UTF-8、非法 surrogate、尾随 JSON、query（含 bare `?`）、非 JSON/压缩体。旧 metadata HTTP 的 name/description-only wire 不变。

更新复用正式 `UpdateCommand="update"`、原 Project 版本及 Project 命令 key 空间，不自建配置版本或幂等空间；同 key/同意图沿原服务重放，不自动换 key/版本或重试。原 Lookup **只按 Project + Update + key** 查询，不能证明调用者传入的 Scheduler body；其历史 ProjectRef 不含 Scheduler 值。PATCH/Lookup 确认后，调用者须显式 GET 取得当前配置；不能拼接事后 GET 为历史回执，`in_progress/not_observed` 也不证明原写未提交。

每次请求经原 Account `CheckRequest`、`RequireHuman` 和服务当前 Session/Owner gate；管理员无 Owner 豁免。GET/HEAD 与原 Lookup 2s、PATCH 30s 总预算从认证前开始并继承更早期限；实际 body Read/Close、Write/Flush、取消回调和服务确认尾完成前不释放拥有者。HEAD 保留安全错误状态、响应长度与完整读取收尾。

暂停不终止已有 Execution，不自动启动 planned Sprint；恢复仍读当前事实。本片不启动 Runner、Provider 或任何自动调度，不改变生产 initializer unbound、ready=false/503 及 Object Runtime join、OpenAI tools、SPA concurrent publication、Jina/Image 既有 STOP。

## 写域与验收

新 `internal/central/project/schedulerhttp/` 私有 adapter、严格 wire 和专属测试；新 `api/openapi/project-scheduler.json`；`internal/central/app/project_update.go` 仅同实例 handler/分派及专属新测试；新 `tests/projectvariable/project_scheduler_http_test.go` 复用自有真实 fixture。领域服务、旧 metadata adapter、client/Session/router/useProjectTasks 均不修改。本 HTTP 片段不修改共享前端；后继设置 UI 已独立接线，范围与真实验收见其任务卡。

先完成 strict wire、边界/权限转发、原版本/key/Lookup、总预算及实际 I/O 尾的离线控制和 schema 检查，再在 root 的资源窗口执行正式服务/Account/PG 自有 fixture：GET/HEAD→PATCH→当前 GET，暂停/恢复、不限/有界/no-op、原 key 查询与重放、版本冲突、当前 Session/Owner/跨 Project 拒绝，核真实 Project/Audit/Outbox 与未新增 Task/Execution 事实及完整资源退出。受控 I/O 不冒真实网络或权限结论；作者自测不代独立接受。

离线 `unit03` 两包 15 个顶层 race 控制和两包 `vet01` 实际 exit 0，覆盖严格 wire、完整 int64、实际编码与本地 OpenAPI schema 对照、方法/身份与错误转发、预算/实际 I/O 收尾，以及原 App 分派。`compile01` 的 1,703 项输入在实际编译前后相同，race binary/list 均 exit 0。此前 `unit01` 的显式 null DTO 前置拒绝与 schema fixture 路径 FAIL、`unit02` 的资源路径前后缀重叠与错误 Request clone 观察 FAIL 均保留；修复仅本 adapter/专属控制，未改共享解码器。

`native01` 的 `TestProjectSchedulerOwnerHTTP` 两子例实际 PASS（19.36s）：自有 TLS 与真实 Bootstrap/Invitation/Redeem/Login、原 Account/Project/PG 组合，核 GET/HEAD、最大 int64、暂停/恢复及显式不限、no-op、当前 GET 与历史 ack/原 Lookup/同 key 重放分离、旧 metadata 共用 key 空间、版本冲突、当前 Owner/管理员/跨 Project/CSRF/已 Logout 拒绝，以及同 User 新 Session 恢复。原命令、精确 Audit/Outbox 与零 Work/Execution/dispatch 后验通过。原 Go/driver Wait0、七资源双 absence、private/runtime/descendants/HOST_TCP 双尾及 1,704 项运行输入未变齐；监督器 terminal0，原外层工具实际 exit0（110.192s）。独立审阅本人核源码、方法、原结果及最终资源尾后接受。

原件入口为 `output/ai/project-scheduler-http/{checks,compile01,native01}/`，`native01/invocation.json` 保存精确命令/环境，`outer.log` 指向原监督日志，`terminal.json` 保存实际终态。不称当前 HEAD 全量检查。真实 HTTP 使用显式自有装配；默认 App listener 动态、真实提交 Unknown 故障不由本 PG 组证明；后继 `TestProjectSchedulerWeb` 已在默认 App 的正常两写及提交后真实截断→原 key Lookup→独立 GET 两场景补齐有限证据，见 UI 卡与其 native02 原件。该后继保持 Lookup 仅历史 key 确认，无重发 PATCH，不代运行中 Execution 暂停行为或 Scheduler Runtime。生产 initializer、ready 与既有 STOP 保持。
