# Project Owner Audit UI rev2 规格验收

限定接受[本卡rev2](../work-items/d27-project-owner-audit-ui.md)的**D1差量及最终组合SPEC STATIC**，无剩余规格必修。root已移交接受提交5320a187；[正式独审报告](project-owner-audit-ui-spec-verification-evidence/originals/independent/rev2/review.md) SHA为dc7d9c60974ec8852982821b86c976e28ccd75a9f7969a94e257ad5409c17c86，[evidence](project-owner-audit-ui-spec-verification-evidence/originals/independent/rev2/evidence.json)与[STOP manifest](project-owner-audit-ui-spec-verification-evidence/originals/independent/rev2/manifest.json)分别为0129b62b…、6d4459c9…。本档只接受规格，不代表产品、动态验收或资源授权；后继实现按root独立交接，不纳入本档结论。

最终卡为39,298字节，SHA1202290254b5b44e5e7bb67359b4fb294f9612ce3daa1279389d90ee614206eb。已提交卡通过[来源映射](project-owner-audit-ui-spec-verification-evidence/source-map.json)引用，无第二份全文复制；从冻结字节计算Git blob为37007fefbe8ef7031ea754c11aabeb81c3a4b49a，提交绑定来自root通知。本归档未执行Git、读取活动产品源或声称独立核验推送。

| 固定阶段 | 原结果与范围 |
| --- | --- |
| [作者初稿](project-owner-audit-ui-spec-verification-evidence/originals/author/rev1/rev1-before-selector-fix.md)／[selfcheck01](project-owner-audit-ui-spec-verification-evidence/originals/author/rev1/selfcheck01.json) | 原JSON明确FAIL：旧auth top少了Session；记录器进程exit0不能解释为规格通过 |
| [修正rev1](project-owner-audit-ui-spec-verification-evidence/originals/author/rev1/rev1.md)／[selfcheck02](project-owner-audit-ui-spec-verification-evidence/originals/author/rev1/selfcheck02.json) | 只将旧top改为TestAccountAuthenticationWebSessionLifecycle，作者STATIC PASS；原失败保留 |
| [rev1完整独审](project-owner-audit-ui-spec-verification-evidence/originals/independent/rev1/review.md) | 唯一D1，CHANGES_REQUIRED／STOP，未接受候选 |
| [rev1→rev2原差量](project-owner-audit-ui-spec-verification-evidence/originals/author/rev2/rev1-to-rev2.diff)／[rev2 freeze](project-owner-audit-ui-spec-verification-evidence/originals/author/rev2/freeze.json) | 仅补D1及一致性修订；作者冻结时仍待独审，不回写历史状态 |
| rev2最终独审 | 复用rev1全文已审部分，定点审D1与全卡组合，SPEC STATIC PASS／STOP |

D1源于原ProjectNav仅在当前path等于settings目标时标记“项目设置”；原settings目标固定为general，新audit叶不会选中，而原20技术路径未包含ProjectNav。修订仅追加 **#22 ProjectNav.vue**，原#1–21顺序不变，**#21仍是README末件**；合计21技术路径（#1–20/#22）＋1文档＝22。

当前项规则在规格层闭合：当前raw route.fullPath与既定settings目标都经projectRoute，canonical Owner／Project相同，目标为general，当前叶限general／audit。设置href/default保持general，首页、品牌、文字、样式与其他共享UI不变；不能用任意prefix匹配。不同Owner／Project、首页、未知后缀及非法raw path的不误选，以及组件／App／真实navigation对两叶当前项和原默认目标的断言均已进入规格。唯一写者与只读例外同步，不扩大其他源范围。

最终独审的纯文件比对核同安装卡与冻结卡、七个明确freeze引用的SHA／bytes及完整差量；HTTP／Session正文、精确top名称和退役预算未改。其他契约沿[rev1 evidence](project-owner-audit-ui-spec-verification-evidence/originals/independent/rev1/evidence.json)复用，包括31个精确11键输出分支与53 action／25 resource非空筛选值的区分、原caps和独立1MiB限制、Owner与Session身份／actual tail、稳定Get上下文和显式重读、原body／schema／公共client参数绑定，以及新3／旧10、7／SMTP9资源和原退役预算。此处记录已审规格，不表示这些未来用例已经执行。

[inputs01](project-owner-audit-ui-spec-verification-evidence/originals/author/inputs01.json)固定43个显式规格来源，另由rev2元数据固定唯一ProjectNav来源指纹；未复制或重读43源码，也不把该清单称为运行闭包。inputs01当时的Owner24 pending保持原文；[原作者freeze](project-owner-audit-ui-spec-verification-evidence/originals/author/rev1/freeze.json)另记root已接受前置Owner24／0a939a7f、独立九轮档f9fb58d1与Audit HTTP4089d131。不同阶段状态不构成技术漂移，不能把旧pending改写为当时已完成。

15个原件／133,979字节逐SHA和bytes核同，重复同SHA才合并实体，本批15个SHA均不同。两份历史卡与原diff保留原始字节，最终rev2卡以Git来源引用；未复制当前产品、依赖树、运行闭包或已有动态原件。[归档自查](project-owner-audit-ui-spec-verification-evidence/archive-checks.json)限原件指纹／JSON和生成文档链接；[格式登记](project-owner-audit-ui-spec-verification-evidence/format-exceptions.json)登记原diff的尾空白与EOF实际情况，不normalize历史证据。历史卡中的相对链接保留原work-items目录语境，不改写原文。

本规格及本档不授Go／Node／browser／build／schema-client或业务测试，不授资源、生产发布或三停止项；未接受完整Audit UI产品、完整D27或生产能力。归档仅写本报告与证据新目录，原卡、三根、开发计划及既有报告不变；所有档案写入完成后STOP。
