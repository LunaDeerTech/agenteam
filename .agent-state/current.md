# 单 read-file Operation / Attempt

2026-10-11。工作树 `/workspace/agenteam-runner-read-file-operation`，分支 `ai/runner-read-file-operation-20261011`，初始基线 main `1b10c29c`。root 负责全部 Git、集成与资源排期；本文件只维护本任务，不覆盖 main 全局协调索引。主目标为真实 ToolCall → 单次 read-file Operation/Attempt → 完整持久结果，不含下一轮 Model/Transcript 或自动重试。

- 正式范围见 [D18/D22 工作卡](../docs/development/work-items/d18-runner-read-file-operation.md)。Tool lead skill_package_review；Runner dispatcher 两文件由 knowledge_delete_fixture 实现；Execution 接缝由 skills_milestone_review 接入；runtime_recovery 为独立 principal。
- Tool 独立基础固定 `d3dc0b18`：consumer 接口、private codec/repository、00065 闭集迁移与三组 codec 控制源码，未编译/执行。旧 install 字节与源码未改，read-file 完整 payload 沿协议 512KiB 编码上限。principal 静审发现新迁移 trigger 局部 uuid 与原 safe_id text domain 类型不符，需按实际列型窄修；尚未 PG 复现，不称迁移可用。
- Runner 固定 `6f87bfff` 已静审，单独 gofmt 无差异；Prepare 固定 RID、单消费 Dispatch 与原 join 尾已落，新增控制未运行。Tool 真实 Attempt 已知提交并实际返回后的发帧门尚待实现。
- 本次合入已提交 `6e0b925c` 为未验收开发依赖，包含 ToolFacts 00063/00064、真实 ToolCall/protected input producer 及显式 Model capture；不是复制 WIP，也不是上游交付或生产绑定。上游 native01 实际 wholeFAIL，阻于 no-tools capture；native02 已越过真实 Model Close barrier，随后 PG requireNoCall 聚合 SQL 报错并 wholeFAIL，全部原退出尾已收。原失败证据保留在原 ToolFacts 树，当前只定位，未认定生产缺口或全部首因。
- 上游已有基础 race 分段34top/五包vet、后继 Model26top分段及vet、两轮PG编译/list通过记录，均限各自实际输入，不能代替本片编译或真实组合。两个自动合入 harness 的原 profile 集合保持并集；未运行 harness/native。
- 后续先闭合 Mount metadata-only 安全目标投影的同 Tx 捕获顺序，再在原真实 created 上 Resolve → Prepare → Attempt commit/实际返回 → Claim。Execution live witness 与借用参数清零、原 owner/Unknown 收束、terminal 结果引用和 producer 尚待实现；不造目标或替代授权。
- main 已确认清理累计26个worktree、27个local、57个remote，148个legacy待核及显式keep/protected保持；本分支不执行清理。当前无本片Go/cache writer、Node、服务、native或网络，后继验证由root统筹窗口。默认生产nil/全registry、OpenAI tools A/B、Object/SPA/Jina/Image和其他原STOP保持。
