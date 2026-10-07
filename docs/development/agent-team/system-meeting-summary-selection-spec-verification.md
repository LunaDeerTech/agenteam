# 系统会议 Summary 模型选择规格归档

状态：[工作卡 rev1](../work-items/d09-system-meeting-summary-selection.md)与11份正式架构、布局及设计文档已通过独立完整有界 **STATIC PASS**，并获主线程采纳。受审时点 S1 尚未实施，没有构建、迁移应用、动态测试或产品验收；35条技术路径是后续完整结果的候选白名单，00020 仅唯一预留，尚未创建迁移源。提交定位使用本页、工作卡及11页的 Git 历史，不预填尚未产生的提交值。

用户原话是“应该是系统管理员统一配置用于summary的模型”，决定来源为[恢复记录的保留边界](recovery-2026-10-07-environment.md#保留边界)。本次确认范围是 **Meeting Rolling Summary initial/update，含首轮标题**：管理员显式选择 enabled System chat Model，项目的新逻辑生成消费 `platform.meeting_summary`，不复制创建默认，不提供 Project override。首轮标题与四字段 Summary 仍沿原同次普通 text 调用和同事务提交，`tools=[]`；Agent compaction 沿 Execution 固化 snapshot，Execution Summary read model 与其他用途保持原规则。

Summary 使用独立 singleton、稳定 ID、version 及未配置状态，与原四用途独立配置；旧 `PlatformSelection`、GET/PUT、整组保存及历史命令语义不追加第五字段。初始不选默认模型、不阻塞 Project 创建，生成明确失败；首次配置后本范围不增加清空，停用保引用，删除须合法替代且满足同模型所有用途。新逻辑 generation 读取当前选择，同一已接受调用与重试保持原 selector version/snapshot。旧 `project_summary` 仍是历史未绑定契约，不自动转成系统配置。

## 固定输入与审查经过

产品参考固定为 `6fa2ee721a75ea34a1ccd6523b8b25d68328c5b9`，用户决定与准备参考为 `175694bf1157b77ea13268bcd8a47787e1a28acd`；完整技术静审及11页差量使用 `2ab562a63b604be2745ca9d283bc1349977806be`。本档归位时 HEAD 为 `ba7ce7296b7c08d976d3fc3ce1e1d0b727a3f04f`，不把另案 Usage A 的接受外推为 Summary 实施证据。

| 原件与阶段 | 结论及精确绑定 |
| --- | --- |
| [独立准备](system-meeting-summary-selection-spec-verification-evidence/objects/20b0b663fa4a1041bcf5f5b2ad112e36ea8abd37fe33c301d3f69817ae4c872b) | 只定位固定实现接缝和风险，尚未读取活动新卡。37项输入清单中，175694bf 相对6fa2ee72仅 tasks.md 不同，其余36项相同；这是原准备报告的核对范围。 |
| [rev1完整技术静审](system-meeting-summary-selection-spec-verification-evidence/objects/c24e879cd9baf92a9e64ec5cc6a5440fd8be680bc7627498b96f31d3231a8a35) | 未发现技术契约阻断；35路径展开为20旧源与15新路径。原卡 SHA `046946ad0d4f23adeaa01511bb7e4e5bed076aee84469a74bf3b2caacd90e61c`，仍有决定来源链接 D1 待修，正式11页实际差量待审。 |
| [最终独立静审](system-meeting-summary-selection-spec-verification-evidence/objects/d9e6dc1bd1dbec33df5ae3960f8172b12034168011ff4c5d25b28ced7662e945) | 复用完整技术审，再核 D1 修复与11页实际差量，结论 PASS。受审卡 SHA `9b582d5df2b31742b34255ff1dcd42e6f37e47889908e64fba7d48abf9ec2ca0`；报告 SHA 即链接中的完整值。 |
| [采纳行政差量](system-meeting-summary-selection-spec-verification-evidence/objects/967243a5526b235a7eba36ed2f1c5e3086964b4a93d516f92526e1750ea864a0) | 最终卡 SHA `1f6f0022aef1e913eb09136d24ca7841319cdf0c02e04556afb7536054cc9cd1`。仅页首两处及最后行政句更新为已接受、11页已同步、未实施；技术契约不变，不另称动态复验。 |

D1 原错误将本次决定指向 `recovery-2026-10-06-continuation.md`；文件存在不能证明来源正确。[原卡](system-meeting-summary-selection-spec-verification-evidence/objects/046946ad0d4f23adeaa01511bb7e4e5bed076aee84469a74bf3b2caacd90e61c)与[修复记录](system-meeting-summary-selection-spec-verification-evidence/objects/b51a12d29f092a76e025e4ae18c6ad31152b8fb1b9eb970f6fd9ce7fcefc2c1a)保持原字节。最终独审确认正确目标为 `recovery-2026-10-07-environment.md#保留边界`，D1 已关闭；该次链接修复前后 §1 起的原文相同，SHA `d2e158c2aa95cb2bdf721ede8ba017c8f2b4e6b762f459649b9dd7212ffdf8be`。后来行政末句更新与这次技术原文比较分开记录。

11页前后指纹见[冻结清单](system-meeting-summary-selection-spec-verification-evidence/objects/e2567ddd78971e73b73c8a1398c5e21a56fb0a8e688352d6d4b745fb76b89313)，原[限定 patch](system-meeting-summary-selection-spec-verification-evidence/objects/1693c5856d0a2bbea31943fe3dfd585bdd2ec99537bcc817005bafcb0669995d)为60626 bytes、+82/-91。独审逐段核实际差量，并检查158个本地链接、8个 fragment；既有 compaction 与当时受保护治理文件保持原字节。历史受检时点的文件一致，不表示这些治理文件以后不能另案更新。

## 接受范围与后续责任

S1 必须一起交付真实持久化、独立技术初始化、当前管理员 get/update/lookup、完整 canonical/reference 校验、同事务权限/Audit/Event/receipt、双 singleton 原子替换及旧 semantic/receipt 兼容。同一 namespace/user/action/key 空间继续适用，新旧请求同 key 异义须拒绝。完整引用并集最多五边及第六哨兵，不能只检查被删除模型的局部引用；同模型承担 memory 与 Summary 时，两个 owner 的版本和事件随同一次事务正确推进。

现有 deletion-impact HTTP/schema/strict client 的第八种 group 及删除对话框标签兼容属于 **S1 联合交付**。早期[建议原件](system-meeting-summary-selection-spec-verification-evidence/objects/49b6da1ee7bb9c2141960cb2e5ca1adaccb0359307dfd9992500383336830ebc)曾把公开兼容安排在 S2；最终 rev1 已明确修正，不能以“新写 HTTP 尚未开放”证明现有页面能解析新事实。旧四用途 body 保持原形，新 Summary 配置 GET/PUT 在 S1 仍不存在。

S2 另卡负责正式 GET/PUT、同页独立编辑与不确定写入恢复、默认 root 显式初始化及真实浏览器；S3 另卡负责两个 Meeting Purpose 到新 selector 的 Resolution 映射、锁内版本读取、plain text profile 与不可变 snapshot/lease，均须消费已接受 S1。D24 再绑定真实 Meeting consumer、生成、首轮标题同事务、Usage 归属与业务重试。跨旧二进制真实 receipt 升级、Unknown 原 writer 终局、权限/事务/HTTP/client 行为均是后续验收门槛，本次没有运行。

## 归档与限制

本档保存13份小原件，共171253 bytes，来源、SHA、字节与对应阶段见[来源映射](system-meeting-summary-selection-spec-verification-evidence/sources.json)。37项输入只保存原清单，不复制依赖源码树、工具、代码或缓存；原准备、初稿、D1、最终审查与行政收尾分开保留，不补造原报告没有保存的 command/raw。原 patch 的123行上下文尾空格保持原 SHA，不能为格式检查改写原件。

本次归位只核源 SHA、JSON、固定 Git 前后指纹、实际限定 patch、行政差量、本页链接及格式，具体记录见[归档自查](system-meeting-summary-selection-spec-verification-evidence/archive-checks.json)。未执行产品、Go/Node/schema、构建、迁移、数据库、服务、监听、容器或浏览器。规格通过不代表完整 D09/D24 或 D08–D28/E01 完成；E01 未开始、ready503 及 Object/tools/SPA publication 三项停止保持。

后续协调注，不属于以上 STATIC 证据：主线程随后仅授权 `internal/central/model/contract/` 下新 `meeting_summary.go`、`meeting_summary_test.go` 与现有 `references.go`、`references_test.go` 四源纯契约首块，作者已实际 ACK 并启动，尚无测试或独立接受。该首块未授权 service、迁移、HTTP、UI 或 root；35路径整体仍未产品接受，00020 仍仅预留。冻结卡中的“尚未实施”保留原受审与行政收尾时点，不代表后续一直未获授权。
