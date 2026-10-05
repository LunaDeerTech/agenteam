# current-resolution 独立验收短计划

状态：基于已接受规格 `e81b029`、固定业务基线 `be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74` 准备；复用 rev1.1 静审报告 SHA `1f1946e347d10e3d91937fd165068220168469d6a0c9ee512adcc2f6b4b636a7`。尚未读作者活动实现，未写探针或构建输入。独立 Go/Docker 执行等待主线程另授。

先接作者停写的 9 个生产文件与 00017 SQL 原字节、基线差量和 SHA manifest。静审闭合 constructor/nil Chan/同 Store、当前权限前置、持久 DTO/约束、一次锁 union、canonical 查询全匹配、exact-Tx witness 生命周期与 fallback、真实提交后交付和 Unknown 原标识。其余 10 个测试文件待最终冻结后检查断言及复用证据，不读活动副本。

优先按风险选择下列组合，最终只保留作者有效证据未覆盖的最小 2–3 个独立顶层；冻结 selector 后再执行，不预先要求重复整张卡矩阵：

| 组合 | 最有辨别力的验证与所需证据 |
| --- | --- |
| 当前身份与同 Tx 证明 | 真实 Account/Project + 严格 consumer 事实。撤权/旧 Session 同时面对存在、缺失、prepared、committed 单位时，必须先命中当前授权；无新 preparation/snapshot/binding/lease。锁观察须记录一次完整 union、consumer Validate 的实际 Tx 和失败来源，不能只观察 Discover。跨 Tx/错 Store/弱或缺锁、过期 Acquire witness 要命中精确检查；分别记录 callback 内错误与最终 CommitResult/poison，保留 legacy Account/MCP fallback 的真实调用与原 binding 字节。 |
| 并发 canonical lease 与原子性 | 两个不同 resolution 单位共享同 execution/ref：原准备候选不同，一方建立唯一 lease，另一方必须 RESOURCE_BUSY 且无第二 binding/lease，重新规划后使用原 canonical ID。检查完整 owner/Project/consumer/ref/lease 行，而非只计数。外层 input capture 与 snapshot/binding/phase/lease 在同 Tx 成功；在发生真实写入后触发 Secret 或 callback 失败，核全部回滚，仅独立 prepared 可以存在。 |
| 当前权限下的历史重入与 Unknown | 同稳定 Human 新 Session 重新 Discover，旧 Actor/实例 plan 拒绝；合法当前 consumer 重放原 snapshot，不受 live disable/delete/selector/endpoint 变化重写，新单位仍走当前配置。分别在 preparing/final 实际 commit 或 rollback 后装饰 Unknown，核原返回 cause/attempt、零交付和真实持久后态；用普通真实 Tx 原 writer 锁证明未终局时不能凭缺行或旧 snapshot 返回，放行后才重验。Committed 后交付前取消须零结果且保留 committed 行。 |

作者原验收仍负责 7 个新真实顶层及适用旧 Model/Secret/Account/MCP 回归，00017 fresh 1..17、populated 1..16、失败回滚、旧相关 catalog 与数据保持，及所有约束/坏 DTO。独立验收依据固定日志去重：通过的合法 System/Project ref、no-ref、轮换、released 旧 lease、schema/旧兼容场景可复用；发现证据缺口才补最小探针，不为覆盖表再次运行全矩阵。

作者交付须绑定最终输入 SHA 与实际 argv/env/exit/log、顶层/子例结果；保留首次失败及修复差量。关键拒绝记录精确外层/内层码及触发步骤；并发需证明两个原准备及实际竞争，不能用预设 RESOURCE_BUSY；Unknown 装饰须分别记录被装饰前 CommitResult、原返回身份和 DB 后态，不称网络 ACK 丢失或物理 ROLLBACK 故障。日志不含材料、cookie、endpoint 或私有输入正文。数据库旁证只读固定、自有 fixture。

当前根 Resolution 仍 nil；严格测试 consumer 不代表生产 D22/Memory/Tool 绑定。无材料读取、Provider I/O、Invocation/Usage、retire、Project cleanup/root 交付；Summary 待决，Object/Artifact 原限制不变。资源清零/唯一窗口要求沿原 driver；本计划不新增网络故障方法。

本次仅写自有 tmp 计划，无仓库/Git/Go/Docker/SQL/网络操作，计划冻结后 all-stop，等待作者生产及 SQL 固定输入。
