# Project Owner Usage 只读 HTTP 规格归档

状态：规格已独立完整 **STATIC PASS** 并获主线程采纳；主线程已授阶段A五路径私有实施，作者已实际开始建立新scratch及RequireHuman/query/DTO/schema与纯测试编写；尚无Go/Node/schema执行、资源或产品验收。正式[工作卡 rev1](../work-items/d09-project-usage-read-http.md)技术§1–7保持原稿SHA `36a7fa5c64cbb8d6716f87851b2b5eea895d344319944f6d558c37c55f53a11d`；页首仅适配正式修订与来源。此次提交可由本文件及工作卡的Git历史定位，不预填尚未产生的提交值。

固定依据为 `4ae02e5c58f4efc3b360706cb2622df05a96cdf0`。[独立审查原报告](project-usage-read-http-spec-verification-evidence/objects/ccf1f861f5ba33c07b6a94ee105815df259a8544f37923d30a12a0fb2d80cbb0)与[36份有限Git来源清单](project-usage-read-http-spec-verification-evidence/objects/db4a4cddb3bb5815d05086a928c88cf0b664c5e08fa07ad9cb1182b1f93bbe6e)核了完整卡及[D09 §11](../work-items/d09-model-system-token-usage-design.md#11-http-与后续绑定)单段澄清；[原差量](project-usage-read-http-spec-verification-evidence/objects/6f5c96acaa4f4a97b2b54f573fd3e0f8b04f08781bbcf1175d1716f76ca09c58)之外的设计正文不变。本档保存七份小原件，逐路径/SHA/字节见[来源映射](project-usage-read-http-spec-verification-evidence/sources.json)，不复制固定源码树，不新增产品校验器。

既定职责是名称入口resolve当前ProjectRef，业务接口使用稳定ProjectID；名称解析与后续Usage List/Aggregate各自完成正式当前Session、Owner及项目状态授权，admin无跨Owner豁免。三个GET/HEAD资源使用既有Usage Reader、完整安全投影和预认证两秒预算，只在事务和实际I/O终局后发布；生产Invocations为真正nil。15路径的A/B/C/D实施与逐轮资源授权是后续门槛，STATIC不替代schema动态容量、native或PG证明。

关键fixture约束：旧 `fixture.human`、`systemHTTPFixture.addBrowser` 的直接INSERT身份准备不能复用；新胶水只能在正式bootstrap/邀请/redeem/login/Logout取得身份后复用既有assemble/bind与严格Facts/Skills测试构件，不能放入生产root。独审中初始猜测文件名不存在，仅按原报告叙述保留，不补造独立command/raw或将其标为产品失败。

这是既定稳定ID/resolve分工下的新可执行前沿，不等于全目标阻塞；其余wire依赖按团队台账中的另案记录处理。Root只读不交付生产Runtime Facts、Project初始化/生命周期、UI、Execution Summary或完整D08/D09。旧报告、接续§1–38保持；Summary待决、ready503、E01未开始及Object/tools/SPA publication三停止不变。

原件入口：[冻结原稿](project-usage-read-http-spec-verification-evidence/objects/cd15e12f62e5ed2056aabcf09f6b0f2da389be278f15e2b72e5fe69495239395)、[作者冻结记录](project-usage-read-http-spec-verification-evidence/objects/594685a361d62cfd38d5b1c34c3e301ce1bea364133a86b03d798fdd9a58a333)、[作者单段检查](project-usage-read-http-spec-verification-evidence/objects/4c76f10faf39aacc2290d2db9583fbd417405eeb3b066d71135da3c12c65582e)、[独审机器记录](project-usage-read-http-spec-verification-evidence/objects/afe9eb6263e0922efd5bde72d1320c86d9609b31a1799d93e5300b6926a41adc)。本次归位只做字节/固定Git/差量/新增链接和限定格式核对，没有执行产品或保存的原脚本；具体归位命令记录属于本次文档交付，不冒充独审原运行。
