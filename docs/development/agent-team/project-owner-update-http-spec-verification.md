# D08 Project Owner Update HTTP 规格验证

2026-10-07：[rev1规格](../work-items/d08-project-owner-update-http.md)已通过完整限定独立STATIC并由root采纳，规格提交 `03d1c107c2196445027351242c63bc0dbc6781d6` 已推送、root核远端一致。产品依赖固定已接受Owner-read `901eb54605d293d4308caadd278c2c3a7ae1b824`，见[只读切片归档](project-owner-read-http-verification.md)。这是规格接受，不是Update产品实现或动态验收；本档不授予实施、Go或资源权限。

## 1. 接受范围

[完整独审](project-owner-update-http-spec-verification-evidence/independent-review.md)与[原结果](project-owner-update-http-spec-verification-evidence/independent-result.json)接受19个候选路径：18技术加另授的README末件，11新/8既有。规格定义稳定ProjectID的名称/描述PATCH、仅update的原命令POST查证和默认root装配；当前Session/Owner权限、历史receipt与当前GET分离，presence-aware严格输入、十一字段安全结果、同key异义/no-op/重放沿既有业务库，不开放其它Project命令或它们的lookup。

规格闭合真实同ProjectAuthority的Audit/Outbox/producer/Reader/Usage、已有Account Activity端口及Project Service关闭adapter。实际Drain终局才能记Joined，不把cancel/Force等同join；无需扩大到app/resources.go或app/app.go。一次Update最多一次原Unknown确认观察；30秒PATCH/2秒lookup限制发布和实际I/O所有权，不声称context能强杀不合作端口。具体事务、原子事实、停止顺序和验收责任以卡技术正文为准，本页不复制另一套契约。

## 2. 原件与静态结论

[作者原冻结](project-owner-update-http-spec-verification-evidence/author-freeze.json)与[原rev1全文](project-owner-update-http-spec-verification-evidence/author-rev1.md)绑定受审SHA `f504baf6c91b291c38bdcc0b6590aaa68cc86554466c6ae15694b95de6b77669`；[接受页首差量](project-owner-update-http-spec-verification-evidence/accepted-header.diff)和[头部原记录](project-owner-update-http-spec-verification-evidence/accepted-header.json)仅改变第3行，接受页SHA为 `b7596871feab7ed147fa59d4a3eb9b331a5c49ceb6296df2c60a1cd551b53b96`，§1至末全部原字节保持。推荐及其inputs保留当时的只读建议和入口指纹，不能替代正式卡；准备笔记中Activity需等core构造后的错误顺序，已由正式卡与独审按Account Authority真实端口纠正。

[原静态检查](project-owner-update-http-spec-verification-evidence/independent-checks.json)保存81条实际只读命令及结果：44来源全部匹配作者冻结，42与固定产品Git一致，另2份是Owner-read提交后已接受的卡页首/验收归档；没有产品依赖漂移。9个文档链接/fragment、19路径范围和9个旧PG精确selector均有静态依据。新增4PG/3native及独立A/B仍是实施后的验收要求，未在本档运行；没有执行Go/Node、动态schema、native、PG或浏览器。

[原检查器首记录](project-owner-update-http-spec-verification-evidence/independent-check-first-result.json)保留新增文件no-index diff实际exit1而stdout/stderr空的情况；[原脚本](project-owner-update-http-spec-verification-evidence/independent-check-original.py)误要求exit0，仅在scratch修正状态判定后[修正脚本](project-owner-update-http-spec-verification-evidence/independent-check.py)的完整文档/来源检查通过。这是私有检查假设错误，不是卡缺陷或业务首红，归档者没有重跑脚本。作者freeze也记录首次Owner-read fragment错误在冻结前纠正；没有独立原首版文档副本可追加，不伪造该前像。

## 3. 保存与边界

[source-map.json](project-owner-update-http-spec-verification-evidence/source-map.json)逐项绑定13个原件，共111,899B；相同内容的整页rev1.diff仅保留SHA/大小，避免再复制原全文。固定产品及44来源沿原manifest/Git证据复用，不复制源码树、cache、binary或运行环境。原始文档内的相对引用按其原来源位置解释，原件字节不改。

归档已核全部来源与落盘字节、JSON、卡仅第3行变化/技术正文、报告链接、Markdown/UTF-8/LF/末换行和生成内容空白。原diff上下文空白为保留原件例外，不规范化；未执行Git、产品、Go或资源，卡和四入口未改。规格仍不交付生产初始化/完整生命周期或UI；系统统一会议initial/update含首轮标题、Project不override/复制初值及compaction/Execution Summary规则不变。production Resolution/Invocations、D24未绑定，ready503、D08–D28/E01未完、E01未开始及Object runtime join、OpenAI tools独立验证、SPA concurrent-publication三停止保持。
