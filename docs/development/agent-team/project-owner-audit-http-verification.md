# Project Owner Audit HTTP 产品验收归档

**完整16路径（14技术＋两文档末件）独立限定 PASS，root 已接受。** 产品提交 `4089d13128da8680955005d9507c8a7da74af1f1` 已推送且 root 核远端一致；[正式卡](../work-items/d04-project-owner-audit-http.md)的规格 `9eb167e4`／[规格归档](project-owner-audit-http-spec-verification.md) `210bd733` 复用，固定前置产品为 `cc850b22`。最终结论见[final16](project-owner-audit-http-verification-evidence/originals/independent/final16/review.md)与[精确来源](project-owner-audit-http-verification-evidence/source-map.json)。这不等于 D04/D08–D28/E01 整体完成。

交付当前 Project Owner 的 Audit 库读取、列表/详情 GET与HEAD、安全 typed DTO/schema及默认根。每次同一事务按 User SH→Project SH 重新核验当前 Session/Owner；管理员不越权。完整页及哨兵检查、3s实际 I/O/同步尾部、原物理读事务 Unknown 的零候选/cause/attempt 与无自动确认保持。Unknown 不新增 HTTP `cause_id` 或命令 receipt。

| 验证 | 固定版本组合与实际范围 |
| --- | --- |
| 作者离线 | candidate01 的22步及 candidate02 两测试受影响 format/race compile/list/vet；普通/race query 8top46sub、HTTP 11top531sub、app 8top148sub，wire 1top31动作及341标准schema例。原图、命令、失败与重跑关系保留，未声称所有项在最后候选重跑。 |
| 独立受控 | query 普通/race各1top9sub、HTTP各1top2sub；40标准parser正反例，原schema三问题与三个极值映射修订组合闭合。受控不能替代物理事务/锁验证。 |
| native | 作者3实际轮，3top8sub/9listeners；direct3＋adopted3 actualwait均0，含TIME_WAIT的全TCP及owned进程双空。候选02不变范围复用。 |
| PG | 作者9轮含new1首红，新4top12sub/旧8top12sub通过；独立A01首红、私有修订A02与B01通过，共3轮、最终2top4sub。总12实际轮均actualwait、watchdog join、每轮7资源双清、输入同，无强制尾部动作。 |
| 原响应 | 作者4份GET原body＋2空HEAD；独立列表1389B、详情727B和HEAD0按相同原字节标准schema解析，未重编码。安全sidecar绑定实际来源。 |

作者 new1 原raw仅记录 line82 HTTP400/INVALID_ARGUMENT，没有具体迭代action；后续静态归因是两测试误用 `model.provider.*` 命令名，candidate02仅#13/#14改正式 `provider.*` Audit常量和公开action诊断，生产/schema/default limit=50不变。独立 candidate01 初次 STATIC 漏检与补单 AUD-TEST-01 均保留，不能倒填原日志。独立 A01 在私有 `metadata={}` 注入自身得到 DATABASE_SQL_FAILED，未到被测 List；原SQLSTATE/约束名未记录。修订私有测试为PG合法UUIDv4 `request_id`、scanner按UUIDv7拒绝，保原metadata/DDL/排序并恢复原值；A02/B01通过，原失败不覆盖。六个原离线工具/测试非零结果及最初图额外5源的门禁修正见映射，修复前后与版本组合分开。

容量分清：1,037,420B是规格静态上界；31 typed动作极值与有限可选分支的200行代表页，8192B cursor容量形状实编码598,220B；真实Signer token556B对应590,584B页，均低于1MiB。不是所有序列化组合穷举，也不把这些合成页当作真实HTTP原body。

**报告更正链：** [原final14](project-owner-audit-http-verification-evidence/originals/independent/final14/review.md)“跨Session与limit绑定cursor”不准确，原件保持。[final16结果](project-owner-audit-http-verification-evidence/originals/independent/final16/result.json)明确cursor仅绑定scope/filter/order，limit可变、不绑定Session；每次仍在同一Tx核当前Session/Owner，cursor不授权限。该更正和两处文档观测/静态归因、有限容量措辞收窄不改技术源，不重跑业务。

[原表派生核对](project-owner-audit-http-verification-evidence/derived/resource-crosscheck.json)实核84个不同资源ID（作者63＋独立21），不是以12×7推定唯一；48个不同新增daemon/PID1 shim身份（36＋12）非owned、未wait。原现场PID1 Z 388→437还含A01→A02间独立的git PID1267429/starttime4787157，单列不归任务。任务双清不等于全机零。100ms强制root返回与之后fixture/client/backend实际退役分别记账，不倒称所有root inner均joined；原历史基线不以后续环境替换。

保存236份去重原件、115份明确派生摘录；重复大输入图/纯list日志只保指纹，完整源复用已接受Git提交，未复制源码树、cache或binary。实际命令/raw/trace/actualwait/资源双清、private probe与最小delta保留；原件历史链接由source-map定位副本。原格式异常27文件，Git相关扫描124条（82条space-before-tab、41条尾空白、1条EOF空白行），另列15项原件无末换行；逐行位置与原hash见映射；原字节不改、不ignore。本轮仅原件hash/JSON/AST/链接/差量检查，无Git、Go、Node、产品测试或资源执行。

主链使用正式Account/Project身份与Audit生产者；持久Skills测试适配器不表示生产Skills绑定。管理员统一会议Summary initial/update含首轮标题，Project不override/复制初值；production Resolution/Invocations/D24未绑定、ready503、E01未开始及Object runtime join/OpenAI tools独立验证/SPA concurrent-publication三停止均保持。后续UI授权与恢复当前状态另见[恢复记录](recovery-2026-10-08-environment.md)。
