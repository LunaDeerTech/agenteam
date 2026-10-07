# D09 Project Model 配置写入 HTTP 规格验证

2026-10-07：[正式 rev2](../work-items/d09-project-model-configuration-write-http.md)已通过完整限定独立 STATIC（rev1全审＋rev2差量＋正式归位末件），root已采纳；规格 `f2ab9c4cc54ce140f586c8131a97b4289d27749e` 已推送且root核远端一致。固定产品为[Model Owner read](project-model-owner-read-http-verification.md) `a0b012ce`，归档 `6bd11cda`。当前[凭据HTTP](project-model-credentials-http-spec-verification.md)仅规格 `9a2a9a1a`／归档 `b498c0bc` 接受，22技术产品仍未接受；本次不把其离线阶段结果当成完整产品前置，也不授权配置写入实施、Go、native或PG。

## 1. 规格范围与原问题

规格定义当前Human Owner的六项Project Provider/Model mutation、显式命令lookup、默认根组合和异常验收，最终14技术＋另授README15，共15路径（10新、5旧）。完整typed输入与原业务policy分界、历史receipt与expected+1、原ctx的Unknown确认、写后坏receipt的新私有分类、1MiB输入/1KiB安全receipt、30s写/2s查证及实际I/O收尾均为后继契约；本次没有执行这些产品行为。

[rev1完整独审](project-model-configuration-write-http-spec-verification-evidence/independent-rev1-review.md)原为NEEDS_REVISION。唯一技术阻断B1是：必须真实执行的旧root仍以Owner POST models的nil body期待405，而新卡开放该POST，旧文件却不在白名单。原helper仅为非nil body添加Content-Type/CSRF/key，不能只改405→400。D1另将“跨User/Project不能串行”修正为不能串用identity/receipt，不增加命令执行次序规则。

[原B1单行拟差量](project-model-configuration-write-http-spec-verification-evidence/B1-proposed-product-test.patch)和[rev1→rev2原patch](project-model-configuration-write-http-spec-verification-evidence/rev1-to-rev2.patch)保留闭合过程：新增技术#14旧root测试，仅将nil/405改为空JSON对象/400 InvalidArgument，保持helper、其余强断言和整top实际执行要求；原#1–13不变，README从#14顺延#15。[rev2独审](project-model-configuration-write-http-spec-verification-evidence/independent-rev2-review.md)核11处替换及B1段的逆向逐字还原，关闭B1/D1；不覆盖原阻断，不把尚未应用的测试提案写成产品修复。

## 2. 正式归位、固定来源和开工门槛

[正式归位末件](project-model-configuration-write-http-spec-verification-evidence/independent-formal-review.md)确认原patch只作页首/正式链接/状态替换及新增§10定位，没有技术漂移；[原结果](project-model-configuration-write-http-spec-verification-evidence/independent-formal-result.json)为完整限定PASS，产品/资源接受仍为false。受审正式卡SHA为 `e37d98f588290b86f95621abcfc76a8b479ca3046150bdad0d9c03d87b3f49a2`；本归档只将页首待复核更新为已复核，并替换§10归档定位，§1–9保持原字节。

[32项原来源](project-model-configuration-write-http-spec-verification-evidence/recommendation-inputs.json)与[13项补充来源](project-model-configuration-write-http-spec-verification-evidence/author-additional-inputs.json)共45个不同路径，均核私有固定snapshot SHA；不读取活动凭据实现，不复制源码树。原索引中的两次只读Git路径探测exit128保持：read归档不在产品提交中，后改取接受归档；schema原猜名不存在，后定位实际已接受路径。它们是来源发现失败，不是产品失败，归档没有重跑这些命令。

实施必须等待凭据完整产品含README独立接受、root采纳与实际根/依赖正式交接，再按后继接受基线重冻本卡唯一候选和实际图；不能用旧根覆盖凭据增量。三根只读边界和优先在project_models组合的既定范围保持；README15仍须完整14技术独审通过后另授。

## 3. 保存范围与自查

[来源映射](project-model-configuration-write-http-spec-verification-evidence/source-map.json)保存26份小原件、168,509 B，覆盖原rev1、两版候选/冻结/自查、B1说明、两份修订patch及独立三轮review/result/checks。rev2全文和原正式卡不重复保存：从原rev1依次应用两份原patch可逐字重建，完整SHA与规格提交均保留；原inputs-rev1与推荐正文仅保指纹。45份source、完整依赖图、产品树、cache、binary和私密runtime未复制。原件内部引用保持原位置语义，正文和卡链接另核。

归档核原件字节/SHA、JSON、45份固定指纹、两份严格patch重建、§1–9不变、UTF-8/LF与相对链接/fragment。原diff的上下文尾空白/space-before-tab按原字节登记，不作格式修复或忽略；没有运行Git diff检查、原脚本、Go、产品或资源。系统管理员统一会议initial/update含首轮标题、Project无override/复制默认、compaction/Execution Summary规则、production Resolution/Invocations与D24未绑定、ready503、D08–D28/E01未完、E01未开始及原三停止均保持。
