# 平台 Embedding Resolver 规格验收

正式[工作项 rev2](../work-items/d09-platform-embedding-resolution.md)已完成完整 rev1 STATIC 加 D1/D2 差量组合审查，root 采纳并提交推送 `48f8293e2fbfde86b52b15df6054ec9643458bff`，远端一致由 root 确认。固定卡 SHA `67d372523d2ab94db5e60e2e85518171985e15a739e3812d564b18b6f61964d7`；[最终独审原报告](platform-embedding-resolution-spec-verification-evidence/originals/independent-rev2/review.md) SHA `0badabfd45098b31b1e58c6ba82894f6d934efda1bff28a9bad1b68b1825c261`。结论仅为 SPEC STATIC PASS，不表示产品、PG、模型调用或生产绑定通过。

[原 rev1](platform-embedding-resolution-spec-verification-evidence/originals/rev1/card.md) 与[首次独审](platform-embedding-resolution-spec-verification-evidence/originals/independent-rev1/review.md)保持原字节及 CHANGES_REQUIRED：D1 是误将既有 direct 收窄为 Agent；D2 是未限定 InTx I/O 禁令，和必要同事务 SQL/Secret Acquire 冲突。[原始修订 diff](platform-embedding-resolution-spec-verification-evidence/originals/rev2/rev1-to-rev2.diff)只有四处单行替换：修订状态、保留 C0 合法 agent/tool chat（含 ToolConsumer/ApprovalAuto）、明确同一调用者 Tx 内必要 SQL/Acquire 与禁止 Provider/外网 I/O、在既定新 pure 中补 Tool 兼容代表。[rev2 冻结](platform-embedding-resolution-spec-verification-evidence/originals/rev2/freeze.json)及[独审证据](platform-embedding-resolution-spec-verification-evidence/originals/independent-rev2/evidence.json)绑定 D1/D2 已关闭；其余已审契约、九路径、top、命令预算及停止边界不变。

规格的完整结果是两个 purpose 的 `platform.embedding` current-selection 库解析，严格当前 consumer 授权下取得不可变 snapshot/binding 和适用 canonical lease；Select 仅当前 Human/Project 安全读。prepared 配置 draft 改变才按原算法推进 Model plan_version，consumer mapping 的变化由 authority 重验使旧计划失效；committed 历史不随当前 selector 热换。现 format1/00017 支持无迁移的静态前提，实际实现与持久化仍须后续验收。8 技术路径及 README 第9末件只按 root 后续移交实施；本归档未读取已开始的产品候选。

维度/IndexProfile、serving generation、D13/D14 正式 consumer、业务 Embed、Provider 发送、Invocation/Usage 及生产 Resolution/Invocations 未因此绑定；三停止、Jina停止取证、ready503 与完整模块/E01边界保持。原规范和作者自查中的历史 pending 不回写。

[来源映射](platform-embedding-resolution-spec-verification-evidence/source-map.json)有 17 个逻辑原件定位、15 个按原字节保存的实体（72479 bytes）。已提交 rev2 卡只保固定 Git commit/path/SHA，不再复制；49 项来源仅保原 manifest 一份，不拷源码、全树或闭包。原件内部路径/链接保持历史语境，由映射定位，不重写为新文档路径。

本次只按文档技能核对必要 source/archive 字节和 SHA、JSON、生成报告相对链接及 UTF-8/LF/格式；[检查记录](platform-embedding-resolution-spec-verification-evidence/checks.json)、[格式记录](platform-embedding-resolution-spec-verification-evidence/format.json)与[归档清单](platform-embedding-resolution-spec-verification-evidence/manifest.json)保存结果。原 diff 的10行上下文尾空白逐行登记，末尾有一个 LF、无 EOF 空行，原件不为格式检查改写。root 明令本任务不运行 Git，`git diff --check` 留给主线程；本归档未执行 Git、Go/Node、SQL、网络、资源或任何产品验证，也未重审已接受 SPEC。
