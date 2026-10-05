# D26 认证验收证据入口

结论和限制见[主报告](../../d26-authentication-verification.md)。本目录保存原字节，来源绝对路径沿[provenance](provenance.json)映射；不能把原报告里的 `/tmp`/`/workspace` 路径当成今后的唯一入口。

| 证据 | 持久入口 |
| --- | --- |
| 作者最终 21 路径、精确差量、覆盖/复用 | [manifest](author/evidence/author-final/manifest.json)、[patch](author/evidence/author-final/candidate.patch)、[coverage](author/evidence/author-final/coverage.json)、[report](author/evidence/author-final/report.md) |
| 独立最终结果与首 CDP 红 | [report](independent/final-report.md)、[first failure](independent/first-real-failure.md)、[final index](independent/final-index.json)、[外部原入口](independent/external-locators.json) |
| 独立真实两轮原日志 | [首轮](independent/logs/independent-real-01.log)、[最终轮](independent/logs/independent-real-02.log)、[八阶段观察](independent/evidence/final-observations.json)、[资源交还](independent/resource-handoff-final.json) |
| 早期静审、pure、native DOM | [core 原审](independent-prior/core-review01/review.md)、[core 闭合](independent-prior/core-review02/review.md)、[UI 原审](independent-prior/ui-review01/review.md)、[UI05](independent-prior/ui05/ui05-review.md)、[pure](independent-prior/pure/report.md)、[native DOM](independent-prior/native-dom/report.md) |
| 输入与来源 | [作者源定位](source-locators.json)、[独立输入定位](independent-input-locators.json)、[原件路径映射](provenance.json) |
| 本次归档检查 | [checks](archive-checks.json)、[精确空白例外](whitespace-exceptions.json)、[SHA256SUMS](SHA256SUMS) |

固定运行基线为 `457b1979c9d6563740543b2011eedc06cce34c71`；只从 `9a710f272026b41ef69852bbeb41cb7670b500a8` 叠加最终 21 路径，不用接受提交整树替换。作者 23 份输入的 217 项源引用通过该提交及 32 个去重原源 blob 恢复。独立最终 567 项为 561 个固定 Git 源、3 个原 probe、3 个生成 dist 文件；原 dist 指纹已核，本次不重建/复制生成 bundle。按原锁和 Node/工具条件构建及重新授权真实窗口才可重跑，不把源核对当动态验收。

在仓库运行 `python3 docs/development/agent-team/evidence/d26-authentication-verification/verify_sources.py` 可只读核对上述 217 与 564 项可恢复源。该助手不执行 Go、npm、浏览器或 Docker，也不安装依赖；生成 dist 的原观察仍由原 manifest/raw 证明。

原作者/独立索引和日志中的历史路径、原失败、原格式原样保留。空白例外精确绑定原件 SHA，不覆盖新写报告；记录中的旧待验状态是原报告当时事实，当前结论以主报告为准。没有归档凭据、浏览器配置目录、node_modules、Go/浏览器缓存、二进制或完整输入树。已提交[规格/API 原档](../d26-authentication-spec/README.md)沿原入口复用。
