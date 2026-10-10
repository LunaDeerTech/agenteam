# D18 固定安装 profile 的 Operation 持久核心

状态：实现中；未编译、未执行 SQL，未提供生产 Tool 派发。首 created 核心及 canonical-v1 窄修已有限独立静审接受；后继单次派发和 D19 固定分支已落源码，尚待检查。真实 Execution/Registry 代码来源、标准 schema validator 与 Skill AgentRun 组合未绑定，不能把接口/纯控当真实成功链。

依据：[D01 Tool 调用和 Operation](d01-contracts/model-tool.md#operationattempt-与结果)、[Tool Execution](../../architecture/tool-system/tool-execution.md)、[D21 install-skill](d21-skill-install-builtin.md)。本切片不修改 Agent/Model/Runner/app，不解除既有 STOP。

## 责任和首接口

Tool 独占 `tool/contract/operation.go`、`tool/runtime/` 与迁移 `00037_tool_operations.sql`；Execution 作者独占真实身份、Snapshot/round/input/payload 及未来 00038。Skill 仍独占真实安装与最后领域授权，Registry 保持 immutable spec 和当前代码绑定。没有任何模块通过读取其他领域私有表制造授权。

首片 `PrepareInstall(ctx, ToolCallBinding, rawArguments)` 与同原输入的 `LookupInstall` 使用实际 Store/短 Tx 写读原 Operation，只产生 `created` 事实。规范包构造、派生 key、首次固定 SkillID 和完整输入绑定不依赖运行内存。后继真正单次 Executor 的实现/依赖边界见下节。

`OperationExecutionAuthority.DiscoverToolCall` 返回自有 issuer 计划/完整锁；`RequireToolCallInTx` 在同 Store 活 Tx 下核原 Actor、Project/Agent/Execution、Snapshot 精确名称/spec/binding、原已提交 Model call/input reference 和当前 Capability∩Policy。完整请求变化、外来计划、停止/取消、未绑定真实来源均拒绝。Tool 的基础锁为 Command EX、Registry SH、Project SH、Agent SH、Execution EX，与提供方计划一次 union；已有 Operation 的后继变更另持 Operation EX，不在 Tx 内补低序锁。

ToolCallBinding 的 Round/Snapshot/InputBinding/Payload UUID 字符串仅投影上游 ID；Model Call/Invocation 沿用现 model/contract typed ID。`NewAgentRun` 只验证值，不能证明身份。没有真实 owner，连首 created 行也不能写入。

## 输入和元数据

仅支持既定 `skill.install` revision 1、`text_files/create`。Builtin 严格解码与 Skill 原 BuildPackage 负责固定 profile 的语法及包规则；不冒通用 2020-12 schema 校验。当前 go.mod/go.sum 没有标准 Go validator，未申请下载。后续实际派发前必须补标准 validator 与真实 Registry/Execution/D19 提供方。

原 arguments ≤1 MiB，必须与上游受保护 payload 的长度/摘要一致。规范化使用解码后的固定字段 JSON 投影，文件顺序保留，不猜测修改参数；规范摘要参与 fingerprint。PG 只保存有界身份、规范摘要、包/manifest 摘要、规范名和原 payload 引用，不保存文件正文。上游 payload 未真实持久化/关联时必须拒绝，不能将无正文记录称可重放。

唯一输入为 `(execution_id, logical_call_id, invocation_id, call_id)`。首次创建固定 OperationID 与目标 SkillID；key 固定为 `tool.skill.install:<operation_id>`。同一输入任何受信身份/spec/binding/正文或规范参数漂移均为幂等冲突，新模型 call 即使参数相同也产生新 Operation。Lookup 只观察原输入；不存在不证明旧 Unknown 请求死亡，不自动创建或重发。

## 37 与 38 的关系

37 只建 ToolOperation/ToolAttempt 两张本域表，不引用不存在的 Execution 表，也不建假 Execution 占位行。Operation 的 Project/Agent/Execution 标量和复合唯一键固定；所有生产插入先过同 Tx 真 Execution owner。未来 38 创建 Execution 的 `(id,project_id,agent_id)` 唯一键后，再添加且验证 37→38 的复合 FK。38 的 Execution 创建不能反过来要求已有 ToolOperation。

Attempt 独立表绑定完整父 tuple，不内嵌 JSON 数组；同 Operation 的 active attempt 唯一。原 attempt/cause 与 known/unknown 结果不能被取消、重连或无 row 覆盖。此首片不创建实际 attempt，不声称已发送或已退休；后继单次同步 adapter 调用必须由 Runtime 私有 active handoff 发起，并在原调用真实返回后持久完整合法 InstallReceipt 与安全 ToolResult。晚取消不抹掉真实已提交 receipt。

## 后继真实生产者

Execution owner 以正式身份/不可变 snapshot/持久 round input 提供上述反向口；不要求完整 Loop 先完成，但缺真实 Model-call writer 时明确未绑定。D19 按原 `AuthorizeTool` allow/deny/waiting 语义提供当前授权，默认无 risk 规则不替代基础权限。等待无自动过期；首 profile 不伪造批准或自动重试。

Skill 将定义消费口，Runtime 提供原 Operation/attempt/spec/binding/key/目标/package/manifest 与当前 Execution/授权的同 Tx 证明，并绑定实际尚未退出的私有 dispatch。计划和最终发布都重验；普通 DTO 或可构造 Call 不是 witness。完整 receipt 的 Runtime 持久终态、标准 schema 校验和真实 Agent 安装链均仍待后继实现/联调。

## 验证与恢复

首基础检查聚焦唯一输入/漂移、无 owner 零 SQL、原 Tx/完整锁、Unknown 不返回新成功及安全元数据，不重复旧 Builtin/Skill 矩阵。真实 00037 迁移与竞争须连续前缀 1–37 和隔离 PG，当前未运行。所有 Git 操作由 root 完成；无 Go、Docker、网络资源在途。

## 单次派发、Skill 消费口和固定 D19 分支

实际构造顺序是 `NewInstallAuthority(core, authorizer, bound ProcessGuard)` → 真实 Skill Service 接该 `InstallExecutionAuthority` → `NewInstallExecutor(authority, SkillService, standard SchemaValidator)`。无可变晚 Bind，没有公开构造 active handoff 的口。ProcessGuard 必须由实际 Object 初始化绑定，composition 持有至 Runtime 和所有原调用真 Joined；只读 CurrentProcess 不证明死亡或替代 join。

Executor 重验原输入/固定目标，取得同 issuer 的当前授权计划，原完整锁和同 Tx 验证标准 input schema 后，原子写一条 ordinal/fence=1 的 Attempt 与 running Operation；running 仅是发送意图。只有物理提交已知成功才用私有 context 同步调用原 Builtin adapter 一次。Skill Discover/Require 对全部 Actor/Command/RequestID/Intent/目标/名称/包和 manifest/字节数绑定；原调用 Tx 内重验完整锁、真实 active Attempt、当前 Execution/Registry/授权。公开 DTO、旧计划、返回后的 context 和移除 cancellation 的 context 均不能自造权限。当前不新增重试或重启接管。

原 Service（含物理 cleanup）实际返回后才撤 active handoff。完整合法 receipt 通过显式私有投影写入 Operation/Attempt 终态，模型仅三字段输出；标准 output schema 失败保 receipt 但形成 backend_contract_violation。已开始调用的普通取消错误不证明无副作用，按 unknown_outcome 保守记录；原 Skill/Runtime CommitUnknown 的 transaction attempt/cause 另存安全 lookup 投影。晚取消后的结果会用独立最多3s、同步等待的最后 metadata Tx 记账，不开 goroutine、不重试。若这次完成写没有确认，原调用已经返回的事实不等于持久退休：原 cause 与候选 receipt 保留，Drain 返回失败且 Joined=false，不能提前释放 guard。

`tool/authorization` 是唯一固定决策实现，不由 Runtime 再写一份规则。它沿原 Execution 计划核当前基础权限，再由 Registry 正式同 Tx 口核 immutable selected revision 与当前真实 code binding；使用已审 Builtin 的真实 scope/resolver 和空 risk classifier，再从同原计划取当前 Agent ApprovalPolicy。此 publish-only v1 的 default、auto 的第一段 default、allow 都直接执行；没有推断「当前无 Approval」，也没有造 Approval/Inbox 记录。需要审批的未来分支必须真实绑定工作流，不能变 deny 或假 waiting。此包不是完整 D19。

Registry 新口由其作者负责；标准 validator 仍无生产实现，缺失时 Executor 强依赖构造拒绝，固定 parser 不冒全 2020-12。Execution 的真实成功 Model input proof、完整 canonical Transcript writer、终态查询/重放与停止后恢复也未交付。本轮新增两项 authorizer、两项 handoff/receipt 控，加原四项 created 核心控，均未执行；这些受控端口不证明 SQL、flock 或真实 Agent 授权。
