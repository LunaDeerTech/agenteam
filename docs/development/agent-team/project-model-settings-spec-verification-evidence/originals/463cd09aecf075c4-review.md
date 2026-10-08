# Project Owner 模型设置 rev0 草案独立规格审查

**草案 SPEC STATIC：可形成完整结果，未发现必须修正的产品语义或后端契约缺口；STOP。** 固定输入为 `spec-draft.md` SHA `8b78072a…`、freeze `71a927a5…`。这不是正式卡、实施许可或产品验收；Audit 整卡和后续精确交接仍待完成。

已按工程开工契约核职责、数据/引用、接口/错误、身份、事务/幂等/恢复、事件事实来源、依赖和验收。两个正式菜单叶子可覆盖 Provider、全 Project Models、独立 Credential 命令和安全可用目录。17 操作与正式 schema 精确对应（配置 12＋凭据 5）；29 唯一路径为 frontend 26＋新 Go 2＋README 末件。未发现必须追加生产后端、迁移或 workspace 写域；workspace 消费的 projectRoute 来自已列入候选的 router/auth，正式移交时须把两个后缀同时纳入该封闭解析集合。

高风险规则核对通过：配置的 Read→历史 receipt→首次 Mutate 与 Secret 的 Mutate→receipt 顺序确实不同，草案正确保留归档后配置原 Execute 与 Credential 仅 lookup 的差别；管理员无跨 Owner 旁路。两类 lookup 仅观察历史，不能确认完整输入/材料；未知粘性、原 key/body/版本恢复、删后重放及 KeyReused 都没有被当前 GET 或新 key 替代。Credential 严格确认只留下安全 ref，由用户另存 Provider，部分成功不回滚或自动重建凭据。

数据与请求界匹配原三 HTTP 契约：五读 8MiB、成功 receipt/lookup/metadata 1KiB、Problem 原600000B；配置七请求同1MiB，材料 create/update400KiB、decoded65536B，与 delete/lookup1KiB区分；配置MaxInt64和凭据Max−1不同。安全目录恰七字段，不补 System Get；全 Project Models 无 provider_id，首page/本地空表不证明某 Provider 无 Model。引用删除仍 DependencyUnbound，不造 Agent rewrite、preview 或 Summary override。合法读取对象与当前较窄写 policy分离，不能删字段/裁数组后假作合法。

身份与交互方案覆盖同 Session checking、真正身份变化、稳定 Project ID、晚尾隔离、实际 Cookie owner直到 body/cancel/finally结束，以及三个旧新草稿域的聚合确认。currentReadContext 只绑定当前读归属，不能变为写 grant。六个新 top、同安全原 body 的 schema/native-client、八布局和不同构造的独立恢复/权限组合能覆盖本卡意图；实际能力尚未执行验证。

以下是**正式卡前/执行前必须精确绑定的工程交接项，不是本 rev0 新发现的语义缺陷**：

- **T1：** Audit 完整接受且相关写入停止后的基线、共享源与具体 17 typed 方法/action 签名、Session 默认依赖追加位置、两个后缀解析及现有调用兼容。
- **T2：** 草案已列旧域对应的实际精确 selector/top 与复用理由，不能用变更前 shared-source PASS 代替本轮兼容。
- **T3：** 有版本的私有 IPC、ack/目标守卫、安全取证、schema/client/runtime/dist 输入及每轮资源/时间分配；45s case、120s top含cleanup、6m包、75s TCP与实际7ID/actualwait须落到可审执行件。
- **T4：** 正式卡路径/修订、固定来源、唯一写者与 README 最后交付；当前只保 scratch 草案。

没有动态检查、Git、仓库写入、网络或资源操作，也不消费活跃 Audit/Model 实现来形成通过结论。输入和必要正式来源、完整操作/路径映射及有限结论见 evidence.json。**无本草案必修；完成 STOP。**
