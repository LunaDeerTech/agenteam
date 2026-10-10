# 当前执行检查点

- 工作树 `/workspace/agenteam-agent-secret-prerequisites`，分支 `ai/agent-secret-prerequisites`，基线 main `69c13e5d`；secret 是本树唯一实现/current 写者，全部 Git 由 root 执行。
- 目标：实现现有 SecretDirectory 的同 Store、当前 Owner/Session、完整锁与安全 metadata 目录；References 仅沿现 `SecretReferenceOwnerAuthority`，真实 Agent canonical writer/witness 缺失时保持 unbound。无新迁移、HTTP、app、Secret material 或生产 Agent 绑定。
- 已读 Go/数据库/安全技能、`d10-secret-variables-owner.md` §5、Agent F1 §6–7与真实契约/00030/现 Owner CRUD。现无独立 ProjectVariableLock；Project SH 与所有变量写的 Project EX互斥，有效 Secret mapping 的计划另含原 CredentialRef SH，不新增锁种。原 Secret 删除对真实 references 的阻断不变。
- 当前独立新增 `internal/central/projectvariable/secret_directory.go`：NewSecretDirectory(Store, ProjectAuthority)；Discover 短事务原 Command EX/User EX/Project SH＋Owner Read；Require 在 caller 活 Tx 检原计划/完整锁与 Owner Mutate、同映射，再给 valid/removed/not_in_scope 一一对应安全事实。ordinary/foreign/missing 不可区分，≤256，不读材料或创建 lease。同步方法由 caller 拥有并 join，无 worker/独立生命周期声明。
- Directory 两个 Go 初稿已停写，4 top 基础控覆盖完整五种输入状态、当前门禁/跨issuer/session/缺锁/异Tx、空集/限额及读取失败/Unknown/取消零结果。gofmt与diffcheck实际0（45317b）；尚未编译/运行，不冒真实SQL或Session验收。References 生产实现尚未开始。root将唯一热cache先给Model；本树等待归还与容量授权，不重复预飞。
- Model/Secret/Skills 同意唯一 Agent canonical writer 提供原 planned command/revision、同 Store/原 Tx 私有 create/preimage/postimage witness；现 public contract 仅绑定 Actor、原 Agent Command、expected/resultVersion 和完整 Before/After，不是授权。没有真实 F1 provider时 References 连空集也不可成功；不新增 ActorCommand 或假 witness。
- 后继：冻结 Directory 正常/明确拒绝基础源后交 root checkpoint/独审；root 授容量后再一轮限定 pure/race/vet，随后准备真实 Directory 同 Store/撤权小链。完整 F1/Registry、foreign join/cleanup、Object Runtime join/OpenAI tools 等旧 STOP 均保持。
