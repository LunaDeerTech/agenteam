# current-resolution rev1 独立规格静审

结论：仅 R1 错误码需窄修后再接受；其余已审设计在既有端口与 20 路径内可实现，未发现需要扩公共 contract、Secret 实现或迁移范围的确定阻断。当前结论仅覆盖规格静审，不是代码或动态验收。

固定输入：`/tmp/agenteam-current-resolution-card-bt7_tirx/rev1.md`，SHA-256 `20637a89d03b3b2e4fc4d1749afc010f260b3eeaeae96b277a217ca6eebe80b7`；源码基线 `be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74`、迁移排期 `6a9c04ee741edcf8722a5d828a05eb6e3bc55891`。38 个提供的固定输入 SHA 全部匹配；20 个唯一路径为 4 旧、16 新。完整输入定位与检查事实见同目录 `checks.json`，不复制全树。

## R1：不存在的公开错误码

卡第 58、70 行把持久同单位异义固定为 `IDEMPOTENCY_CONFLICT`。既有 Foundation 闭集使用 `f.IdempotencyKeyReused` / `IDEMPOTENCY_KEY_REUSED`，不认识的 Code.Safe 会转为 INTERNAL_ERROR；提供的固定 Model `commands.go:237/329/381` 也使用既有码。20 路径不包含 Foundation 公共错误契约，不能靠新增码兑现这两句。

最小修订：只将两处字面量改为 `IDEMPOTENCY_KEY_REUSED`，同单位异义拒绝的规则和全部路径保持。补充错误闭集证据来自此前已验根的固定 `foundation/fault.go:29/50–68`（SHA `94a339976fb8f1e1173256aa840dd1273644cafd37dc6a3e96e33d1cb8ff09de`）；未读取活动 Foundation 或 Model 源。

## 已核的重要边界

- **当前权限先于存在性。** §3 第 56 行的必须规则覆盖 §5.1 的候选读取：Tx 外候选只可用于收集计划，不得将缺配置、坏 preparing 或 semantic 不同等可区分结果先交付。需要返回此类结果时，必须先使用 consumer 全锁、真实 Store/Tx 和正式 Validate 完成当前授权；`Discover` 本身不是授权。C0 的 ResolveConsumer 允许无 LeaseID 的独立前置请求，随后 ref/lease 已确定必须重新 Discover 完整请求；准备与最终 Tx 继续重验。此规则在卡内已有，不能将“必要时”解释为实现可省略授权。
- **Apply 调用链可闭合。** 实际 Secret `usage_plan.go` 先 validateUsage（调用 Model ValidateUsageInTx），然后 loadMetadata，最后 Acquire 分支 leaseGrant。故 §7 两阶段私有载体可先携带准备事实，再在这一正常 Validate 链签发 exact-Tx witness，供同次同步 leaseGrant 消费。§5.2 的“建立 witness”不能解释为提前写已授权 bool；§7 第 132、135 行已明确这一点。prepared→committed 的本域行、固定 draft/version 与当前 consumer 可在同 Tx 检验，无需改 Secret 或将 phase 猜成权限。单次载体失效、错 Tx/Store、缺/弱锁及 poison 均保留独立验收要求。
- **稳定单位、Actor 与共享 lease 分开。** 持久 digest 不含 Session 或首次 live 配置；内存 plan 仍绑完整 Actor、当前 consumer mapping 和实例 issuer。旧准备 stable SnapshotID/plan_version 与 canonical lease 重规划规则能够容纳 execution 跨用途共享同 ref lease，同时拒绝跨 Project/不相容 execution 借用。完整 union 后只重读/验证，不补低序锁；并发第二准备必须 RESOURCE_BUSY 后在 Tx 外重规划。
- **历史重放与当前 Secret 并不矛盾。** committed 分支从原 DTO 验快照，nullable live FK 不成为历史有效性的依赖；当前 consumer 授权仍必须成功。有 ref 时正式 planned Acquire 继续核 metadata purpose、exact owner/ref/lease、released 状态与 Secret 自身可写门禁。这些失败可拒绝返回旧结果，但不得转为 live Provider/Model 不存在或重写快照。`ApplyUsageInTx` 的实际 released 失败为 INVALID_STATE；rotation 不改变稳定 ref/lease。无 ref 为 C0 合法零 Secret 分支，仅产元数据，不声称 wire 可匿名发送。
- **Unknown 与生命周期限制真实。** 准备与最终分别保留原返回 cause/attempt，Unknown 零交付；重入经过同一 Command writer 屏障并重新核 canonical facts，不能把旧 snapshot 当新 binding 的提交证明。§8/验收表明确结果装饰只证真实后态、原锁与返回标识，不证物理 ACK 丢失或旧 ROLLBACK 终局；外层 InTx 返回仍 provisional。没有为此新增网络故障方法、后台确认或 join 声明。
- **范围与迁移可交付。** optional Resolution nil 保持当前根无正向 consumer，现有 keyed Authorizations 构造可兼容；strict fixture 消费真实 Account/Project/Secret，仅模拟尚不存在的 consumer canonical 域。00017 三表使用独立 preparation、immutable snapshot 与 exact binding，旧 SQL/Secret 私表不改，fresh/populated、旧约束、坏事实及 Up-only 验证边界明确。retire、孤儿清理、Project cleanup、Invocation/Usage/root 尚未交付，因此即使库验收通过也不能安装生产 Resolution provider。

## 验收建议与停止状态

实现后的重点仍是当前权限早于存在性反例、完整一次锁 union、双准备 shared-lease 竞争、同 Tx input/snapshot/binding/lease 回滚、真实 released/rotation、历史配置删除后的重放与最终 Unknown 原标识。7 个新真实顶层和旧 Model/Secret/Account/MCP 适用回归足以承接这些风险；本次未预写或执行探针，不将静审替代这些门槛。

Summary 待产品决定、production consumer/材料读取/退休/Invocation/Usage/Project cleanup/root 未绑定，以及原 Object/Artifact 限制均未解除。没有读取活动 Resolver/structured/D26/Artifact 实现，没有仓库、Git、Go、Docker、SQL 或网络操作。仅此自有临时报告与检查索引写入；报告冻结后停止写入，待 R1 固定差量复核。
