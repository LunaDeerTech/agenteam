# 配置写入之后的下一完整结果建议

2026-10-07，只读 architecture 分析；不是正式规格、实施授权或产品验收。固定产品 `e4b1b89197da0e9027018fdb41f6ab3e04050b9d`，40 份定点来源及 Git blob/SHA-256 见 `sources.json`。未读当前配置写入 #1–14 活动实现，未运行 Go/Node/测试/服务/网络或 Git 写操作；仅写本 scratch。

**推荐：Project Owner Audit 列表/详情 HTTP、同事务当前授权读入口及默认根组合。** 完整结果是当前 Human Owner 能通过真实 HTTP 查看本项目正式 producer 已持久化的安全审计事实，支持原结构化过滤/cursor 和安全详情；不能修改/清理记录、导出正文或查询其他 scope。建议两个资源 `/api/v1/projects/{project_id}/audit` 与其 `/{audit_id}`，GET/HEAD、方法和错误闭集由新卡一次冻结。不是把“新增几个方法”独立当交付，也不要求先做 UI。

## 既定产品含义，没有新问题需要用户决定

- `docs/architecture/security-governance/audit.md` §3.1 明确“Project Owner 可以查询当前 Project 的 Audit”；§14/16 明确项目统一列表、结构化详情及资源引用，§15/17 明确结构化过滤和 cursor。这是第一阶段已有范围。
- §20 要求 `current_user == project.owner_user_id`；Agent 普通 Tool 不可浏览 Audit。新 HTTP 仅当前 Human Session，System 管理员没有跨 Owner 豁免，Owner 身份也不授予 System Audit 权限。记录中的 Actor 可以是正式 Human/AgentRun/Service，**查看者身份与历史 Actor 分支不可混淆**。
- §2/9：不回显 Secret/credential、API key、private key、原 command/stdout/stderr/Tool Result/Prompt/Response 或未脱敏参数。只显式投影安全 ID、时间、action/outcome、resource、关联 ID、typed metadata 与固定 summary；不序列化完整 Actor、数据库 row、任意 JSON、私有 witness 或错误 cause。原 ActorSummary 不含认证 SessionID；已有 typed resource/metadata 中合法 Session 标识与 Cookie/token 区分，不武断全删或全开放。新卡须按当前 Project 合法 action/actor/resource/metadata 逐分支闭合，不照抄 System 的37动作/两Actor/五关联限制。
- §13：不自动过期、没有 Project retention 配置；归档保留历史，永久删除沿真实生命周期清理 Project Audit，System Audit 不随之删除，也不复制 Project 正文规避删除。新读口不接管该删除流程。当前正式 `CheckOwnerGate` 明确 initialized 且非 deleting 才可 Read，archiving/archived 可读，pending/deleting 拒绝；不另设“为看初始化失败日志而绕 gate”的权限。
- 系统管理员统一 Meeting Summary initial/update（含首次标题）、无 Project override/复制默认已决，不重新询问。HTTP 命名、预算、schema 和私有拆包属于工程规格工作。

## 真实已有能力与尚缺工作

| 依据 | 可直接消费的事实 | 不可冒称已经满足 |
| --- | --- | --- |
| `audit/query.go`、contract、cursor；D04/System Audit 验收 `b124650` + `fa2d775` | 参数化 scope/Project 谓词；原 filter digest/order；created_at DESC/id DESC、limit+1；typed scanner；可带逐行 check 的完整行/哨兵验证和 Close 后 Err；默认50、上限200；签名 cursor 绑定 scope/filter/order，limit 可变 | 旧 `Service.Get/List` 先用 `Tx{}` authorize 后在裸 Store 查询，**不能直接挂 HTTP 当作新同事务读口**；旧 System 实轮也没有物理读 COMMIT Unknown，不能移植该结论 |
| `project.Authority.current/RequireOwnerInTx/AuthorizeProject` | 有效原 Tx 中核 User/Project 既持锁、当前 Session、真实 Owner、初始化/lifecycle gate；跨 Owner 隐匿及 Read/Mutate 区分已定 | 事务外 AccessGrant/预认证不能覆盖随后数据库读取；只调纯 gate 不是权限证明 |
| `app/security.go`、`account.go`、`project_usage.go` at e4b1b891 | `createSecurity` 已沿同 Account Store 注入 Sessions/System/Models 和唯一真实 Project Authority；同 auditor 被 Project/Secret/Model 使用，真实 Secret AuditFacts 已接上 | 不需要另建 auditor/Project Authority，不需要新 initializer/worker；新 handler 与精确路由尚未存在 |
| Project Update、凭据 e4b1b891；Model 配置库 de00c610 | 已有实际 Project/Secret/Model typed Audit producer、同事务事实和回滚；凭据默认根真实链已接受 | 当前配置写入 HTTP #1–14 仍实施中；其完整接受后才能计入新根 HTTP 配置变更→Audit 的组合证据 |
| System `system_query.go`/HTTP、近期 Owner read/凭据 IO | 同 Tx 候选隔离、完整扫描、终局后发布的算法；近期安全 deadline/Close/callback/透明 writer 的已验模式 | 新 Project facet、安全 DTO/schema、实际 IO 与根业务行为须自己验证；System 私有 reader 不是可直接复制的 Project 公共契约 |

新卡核心应为私有 `ListProject/GetProject` 一类窄 facade：继承父期限，在同一真实 Tx 先取 User SH→Project SH、当前授权，再查询/验证全部行（含哨兵）、关闭 rows/检查错误、签 cursor，只有实际 Committed 且 ctx 有效才返回候选。使用原 Store Acquire 能力即可，无新增通用事务框架。读 Unknown/失败/取消零候选，原 Fault/commit_state/cause 边界不被重读、缓存或 `found=false` 覆盖。方法名称与公开 Go 形状由新规格确定，不声称已有端口。

建议沿 Audit 3s 总读预算（含 Account/实际 IO，较早 parent 优先），列表1–200和原 cursor 语义；成功 JSON 可先以1MiB为工程候选，必须用 **Project 全合法分支**重新计算并实际编码最大合法页，不能借 System 975420B 算术宣布通过。scope/Project/Actor/条件 metadata 错配或坏哨兵整页拒绝，不静默过滤。GET/HEAD 均完成完整表示验证/编码；HEAD 无实体。deadline/Flush 能力有限预检、每个 setter/Close panic 安全转错、callback 真 join、keepalive reset、tracked writer code/已提交防二写按当前已验要求明确，不能直接复用旧 System IO 的历史限制。

## 最小候选文件和所有权

下表是可实现范围建议，尚不授写；预计 **14技术＋2文档末件**，无需迁移、依赖升级或公共 Contract 扩写。新卡如需额外文件，先按实际接口证明并由 root 冻结清单。

| 候选路径 | 职责 |
| --- | --- |
| 新 `internal/central/audit/project_query.go`、`project_query_test.go` | 专用 Tx 读入口与受控授权/扫描/终局测试，调用本包原查询算法，旧 Get/List/System 不改 |
| 新 `internal/central/audit/http/project.go`、`project_wire.go` | 严格 Project resource/query、Human 边界、私有实际 IO、Project DTO/条件 metadata |
| 新同目录 `project_test.go`、`project_wire_test.go`、`project_native_test.go` | 受控行为/标准 schema/最大合法页、真实 socket 三组 |
| 新 `api/openapi/project-audit.json` | 独立 Project schema，local common refs；不扩大 System schema |
| 新 `internal/central/app/project_audit.go`、`project_audit_test.go`；旧 `app/account.go` | 原 auditor/core 构造与精确路径分流，保留现 root/初始化/关闭顺序 |
| 新 `tests/model/project_audit_http_fixture_test.go`、`project_audit_http_test.go`、`project_audit_http_root_test.go` | 复用该包正式 Account/Project fixture 与 root 设施，真实权限/终局/producer/root组合；不新开第21个fixture包 |
| `docs/development/backend/audit.md`、`README.md` | 技术完整接受后单独授末件 |

不写配置卡当前的 `app/project_models.go`、新 model HTTP、`project-models.json` 或其测试。不改 Audit append/lookup/cleanup、Project Authority、三根已验授权构造或 Object。`account.go` 虽不在配置卡14表，仍须 root 明确交给新唯一作者；两个文档末件共享由 root 排期。以上新文件无当前实现者，本分析不替 root 指派作者。

**可并行的真实边界：这个 Audit 结果本身并不以配置写入 HTTP 为功能前置。** e4b1b891 已有同 root 的 Project Update/凭据 producer，Model 配置库也早已接受。若 root 要并行，可独立授本卡新 Audit 包/HTTP/schema 的固定 e4 基线实施与受控检查；真实根可以先以已接受 Update/凭据 producer验证。不过共享 app/tests/model 编译包包含活动配置源，必须用固定快照隔离或等共同 freeze，资源只能按独占窗口串行，最终在配置写入接受后的共同产品输入补该 producer/路由兼容。建议当前按“配置写入完整接受→Audit完整根验收”排期，减少两次组合；这是所有权/验收调度依赖，不伪装成缺失产品依赖。

## 必须完成的验收概念

1. 纯/受控：Project 所有合法 Actor/metadata 分支与反例、最大合法200条实际 JSON/反解/无截断、严格 query/HEAD、分页绑定与坏后项/哨兵/Close后Err、服务 error 优先；方法能力不足/Unwrap/setter/Close panic、阻塞 callback/Close 真 wait、tracked Problem/committed 不二写。标准 Draft2020-12＋FormatChecker解析同安全形状。
2. 真实 PG 权限和终局：正式 Bootstrap/Invitation/Redeem/Login/Logout；Owner 与另一Owner/非Owner管理员、archive Read/pending/delete gate；User/Project SH 对 Logout/Project writer EX 两种提交顺序；跨 scope/filter cursor、同 timestamp 稳定分页；目标**同 Audit 读事务** COMMIT ACK 丢失及 ctx 取消零表示。仅结果装饰不能替真实 Unknown 证据。
3. 默认 `app.Run`：不注入 Store/handler，正式 Update/凭据（以及前卡接受后的 Model mutation）产出的准确安全 Audit 经新列表/详情可见，重放不新增记录、失败不新增/漏半事实、历史只读不更改业务；一次RequestID/日志、原body＋source/hash经新schema、System Audit/Ownerread/Model/Usage路由保持。正式 Project 创建和Skills仍未绑定，测试持久Skills/canonical前置须明示，不能转称新建项目可用。
4. 三native代表：自然3s/较早parent慢body、真实Write/Flush、keepalive reset与完整Close/callback；只追踪安全无buffer syscall。沿最近已验fixture/runtime工具最小delta，先冻实际图与输入再运行，不新增外网/Provider调用。
5. 每纯命令45s有界、精确selector避免全app资源；真实top120s含Cleanup/包6m；完整原20包fixture4容器3网络，逐轮fresh≥5GiB/当前PID-starttime/本地digest/自有空Docker配置，subreaper actual wait和7精确资源/owned双清；native另端口含TIME_WAIT双空。daemon/PID1差集非owned未wait单列。这里没有运行任何上述检查或预验环境可用。

## 为什么不把其他未满足依赖当下一完整结果

- D10 Skills 只有 P1真实builtin/确定性包已接受；初始化PG服务、D05专用发布/Audit事实与D08绑定仍缺。D12完整Knowledge同样有真实Object/Audit/source和生命周期缺口；不借这两项恢复或绕过 Object runtime join 停止任务。
- Project创建/归档删除生产链不能用已验测试fixture、空initializer、固定allow填空。Model配置写完也不自动满足Skills初始化。
- current Resolution＋Summary S3是已验库；生产 ConsumerAuthority/Runtime canonical、Invocation/Usage发送资格与lease退役仍未绑定。D24不能靠 Human Owner 权限替真实Meeting/Operation，Summary统一选择已决也不等于已生成会议标题/摘要。
- tools独立动态和SPA concurrent-publication继续停止；Anthropic正式卡明确依赖tools接受，不推荐另造text-only替代；Jina后继正式wire来源曾受代理拒绝，现无新官方证据。均不作为本轮“可立即完整交付”承诺。
- 新Audit接受也不意味着整D04/D08/D09/D27或E01完成，不改变ready503；UI/全文搜索/导出/任意保留期/外部模型调用均不在推荐结果。

固定计划、台账和AGENTS在e4b1b891页首仍保留“凭据实施中”历史状态；本次依据root已确认凭据e4b1b891接受和该提交产品/README，不把旧页首当新阻塞，也不篡改它们。配置写入当前实施事实来自root派工，未拿其活动代码补任何已验能力。已完成只读核对，建议供root采纳后另授正式新卡；本scratch冻结后停止写入。
