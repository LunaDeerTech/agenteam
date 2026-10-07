# D09 Project Owner 模型凭据 HTTP 规格验证

2026-10-07：[正式 rev2](../work-items/d09-project-model-credentials-http.md)已获完整限定独立 STATIC PASS（rev1 全审＋rev2 差量＋正式归位末件），root 已采纳；规格 `9a2a9a1a1d27d79fe00204147e58d5b66000ca35` 已推送且 root 核远端一致。固定产品为 [Owner Update 完整19](project-owner-update-http-verification.md) `61bed1fc`，归档 `c839f965`。本次仅接受规格，未授权凭据产品实施、Go、native 或 PostgreSQL。

## 1. 范围与唯一修订

规格规定同 Project Owner 的 Model-purpose 凭据 Create/Rotate/Delete、安全 metadata GET/HEAD、无材料的命令 lookup 和真实根绑定，22 技术路径（9 既有、13 新增），README 末件另授。材料所有权、当前权限、Project lookup 真正新增锁/事务契约、Unknown 零观察与实际 I/O 收尾，以及五个新 PG top、三个 native top 和旧兼容集合，都是后继实施验收要求，不是本次运行结果。

[rev1 完整独审](project-model-credentials-http-spec-verification-evidence/independent-rev1-review.md)保留原待修结论，唯一 finding 为 CRD-STATIC-01：deadline setter 在 start/callback/abort/reset 各阶段及 owned Body.Close 的 panic，必须分别安全捕获、继续剩余收尾并实际 join；能力 adapter 不得遮蔽原 tracked writer 的状态和防二写语义；trace 须在采集前限定无载荷 syscall 闭集。它是一项工程澄清，不是已运行产品失败。

[原 rev1→rev2 差量](project-model-credentials-http-spec-verification-evidence/rev1-to-rev2.diff)仅改页首、§5 和 §8，§1–4、§6–7及候选范围不变。[rev2 独立结果](project-model-credentials-http-spec-verification-evidence/independent-rev2-result.json)关闭该项并复用 rev1 全审形成完整限定 STATIC PASS；没有覆盖原 hold 记录。[正式归位差量](project-model-credentials-http-spec-verification-evidence/formal-rev2.delta.diff)只改行政页首，[末件独审](project-model-credentials-http-spec-verification-evidence/independent-formal-rev2-result.json)确认 §1–8 原字节保持。

## 2. 固定输入与实施门槛

[作者原 inputs-rev2](project-model-credentials-http-spec-verification-evidence/author-inputs-rev2.json)保存49项固定来源；原48项映射保持，仅补接受闭包中的 `httpapi/response_writer.go`。作者与独审均未把活动 Model Owner read #1–13 当作已验依赖。归档核49份私有固定 snapshot 的原 SHA，未读取活动业务源；[准备记录](project-model-credentials-http-spec-verification-evidence/independent-prep-checkpoints.md)仍仅代表此前准备，不计完整审查或动态通过。

接受卡 SHA 为 `2f0aa708e1f9ccbe5bf03a637ed7fc32f131d19633bb6b2f8d82c99c52186444`；技术 §1–8 SHA 为 `cd6ac3e4b8378452520d2cf4daca9d9882395258c0123e36392478d380cb702b`。实施必须先等当前 Model Owner read 完整产品接受，再由 root 正式移交 `account.go`、`project_usage.go`、`security.go` 及相关共享测试，并重冻实际联合输入；不能用旧根覆盖后继已接受增量。

## 3. 保存范围与自查

[来源映射](project-model-credentials-http-spec-verification-evidence/source-map.json)绑定14个小原件、共122,518 B：两版候选、两份原差量、49项来源清单、正式冻结和独立准备/全审/差量/末件结果。原 inputs-rev1 仅保指纹，原48项映射可在已保存 inputs-rev2 中逐项核对；正式卡复用规格提交。49份源码 snapshot、大型闭包/图、既有归档树、cache、binary 和私密 runtime 未复制；大型闭包仅引用原清单指纹，没有另造派生摘录。原件内部链接保持其原来源位置语义，未改写成归档链接。

两份原 diff 的上下文空白按原字节保留并逐行登记；其余新增正文/JSON和小原件的 UTF-8、LF、末尾换行及空白检查通过。归档核复制字节/SHA、JSON、两份精确差量、技术正文不变及正文相对链接；未执行 Git diff 检查、原脚本、Go、产品或资源，未补造原来源没有提供的 command/raw 输出。系统管理员统一会议 initial/update（含首轮标题）、Project 无 override/复制默认、compaction/Execution Summary 规则不改；production Resolution/Invocations 与 D24 未绑定，ready503、D08–D28/E01 未完、E01 未开始及 Object runtime join/OpenAI tools 独立验证/SPA concurrent-publication 三停止保持。
