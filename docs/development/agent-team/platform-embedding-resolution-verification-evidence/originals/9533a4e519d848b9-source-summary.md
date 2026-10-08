# rev3 来源与规范澄清

root 转交 fixture_recovery 的静态发现并核实授权：仅澄清 Embedding 卡 §4 的同 Call 改义错误优先序，不修改 Resolver/store 或放宽 canonical lease 身份。完整候选仍待独立差量与最终组合复核。

必要来源均属于原 49 项，5 项当前指纹与原清单相同：

- resolver.go:110–136：committed 取原 draft/ref；候选 canonical 错误先记录为 readErr。
- resolver.go:145–194、285–298：Consumer.Discover 不等于授权；完整锁下 ValidateInTx、原 writer 下 preparation 重读一致性、ctx/readErr 在 semantic 冲突返回前完成。
- resolution_store.go:60–110：无 CredentialID 不查 canonical；查询 exact owner/ref 的完整 committed facts，Project/AgentID/ExecutionID 不相容为 Forbidden，不能借新 LeaseID 消解。
- resolution_plan.go:30–58：model_call 单位是 Project+owner，semantic 包含 consumer/purpose/Selection 与稳定 initiator；Human Session 不进入持久语义。
- 原 Resolver 卡 §3/§4/§6 与正式验收中的当前授权优先证据保持，未替换或重写。

| 场景 | 本卡应有分类边界 |
| --- | --- |
| 结构非法、旧 opaque plan 改义、consumer 当前事实失效、writer 变化或其它更早错误 | 沿既有阶段实际错误；不能强求 semantic KeyReused。 |
| 有 ref 且 exact owner/ref 的完整合法 committed canonical binding 已存在，Project/Agent/Execution 不相容，且无更早错误 | Forbidden；Memory 换 Agent、Knowledge/Memory 切换使 Agent 有无变化属于此例。 |
| 无 ref、prepared 尚无 committed canonical binding，或 canonical 身份相容 | 不是由 ref 有无直接决定错误；current facts/config/writer 等前置全部通过且同单位 semantic 变化，才到 IdempotencyKeyReused。 |
| committed 后 live selector/ref 改变 | 继续使用原 snapshot/ref 判断 canonical，不以 live ref 绕过旧绑定。 |
| 相同 model_call ID 但另一 Project | 解析单位不同；同 owner/ref 的跨 Project canonical 拒绝规则仍成立，不增全局 CallID 唯一约束。 |

差量严格只有3行：rev3 状态、§4目标段落、原 Replay top 的该断言说明；路径/public API/其他契约/测试top/预算不变。原 rev2 卡、diff、freeze、来源摘要、自查及 rev1 历史原件均保留。原49来源清单逐字复制，仅5必要源重新核指纹，不扫描当前其他源或 Audit 活动文件。

本轮只有静态读文、卡片/scratch写入和指纹自查；未执行Go/Node/binary/SQL/网络/资源/Git，未实施产品。原 Audit Go 候选与独立报告未读写。
